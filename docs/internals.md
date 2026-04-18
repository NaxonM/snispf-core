# Internals

Architecture and implementation reference for contributors and advanced debugging. This document assumes familiarity with Go and TCP/IP fundamentals.

For operational setup, see the root `README.md` and [`beginner-guide.md`](beginner-guide.md).

---

## Execution models

SNISPF has two top-level execution modes:

**Direct mode** — a single process accepts local TCP connections and forwards them to upstream endpoints with bypass strategies applied.

**Service API mode** — a controller process exposes HTTP endpoints and manages a child worker process that runs the direct-mode core. The two processes communicate only through process lifecycle signals and log files.

---

## Code map

| Path | Responsibility |
|---|---|
| `cmd/snispf/main.go` | Bootstrap, CLI parsing, config loading, strategy wiring, runtime startup |
| `cmd/snispf/service_api.go` | HTTP control API (`/v1/*`) and worker process lifecycle |
| `internal/forwarder/server.go` | TCP listener and per-connection forwarding loop |
| `internal/bypass/` | Strategy implementations (`fragment`, `fake_sni`, `combined`, `wrong_seq`) |
| `internal/rawinjector/` | Raw packet monitor and injector (Linux, Windows, and stub) |
| `internal/tlsclienthello/` | ClientHello parsing, building, and fragmentation |
| `internal/utils/` | Config schema, normalization, endpoint probing, capability checks |

---

## Startup pipeline (direct mode)

`main.go` startup in order:

1. Parse CLI flags and normalize aliases
2. Load config JSON (or generate defaults)
3. Apply CLI flag overrides (`--listen`, `--connect`, `--sni`, `--method`)
4. Validate port ranges
5. Normalize config (`utils.NormalizeConfig`):
   - Fill missing defaults
   - Apply top-level defaults to each `LISTENERS[]` entry
   - Materialize endpoint list when absent
   - Resolve hostnames to IPs
   - Ensure `WRONG_SEQ_CONFIRM_TIMEOUT_MS` default (2000 ms)
6. Log precedence warnings when `ENDPOINTS[0]` overrides top-level fields
7. Filter to enabled and valid endpoints (`utils.EnabledEndpoints`)
8. Optional endpoint health probing (`utils.ProbeHealthyEndpoints`)
9. Build raw injector (single endpoint, strategy-dependent, platform-dependent)
10. Build bypass strategy implementation
11. Start forwarder server with cancellation context

When `LISTENERS` is configured, steps 9–11 repeat once per listener inside the same process.

`wrong_seq` enforces two additional startup constraints before proceeding: exactly one enabled endpoint and a working raw injector.

---

## Per-connection forwarding loop

`internal/forwarder/server.go`:

1. Accept inbound TCP connection
2. Read first payload (expected to be a TLS ClientHello)
3. Parse incoming ClientHello for logging and observability
4. Select upstream endpoint (load-balancer index + failover offset)
5. Reserve a local source port, register it with the raw injector
6. Dial upstream using that specific source port
7. Apply bypass strategy to the first payload
8. On strategy failure:
   - `wrong_seq` — terminate connection (strict: no fallback)
   - all other strategies — write first payload directly as fallback
9. Start bidirectional relay (`io.Copy` in both directions)

**Why source port reservation?** For raw-assisted strategies, the forwarder must register the local source port with the injector *before* the TCP handshake occurs. This ensures the injector sees the SYN/ACK for the tracked flow before the confirmation window begins.

---

## Bypass strategy implementations

All strategies implement the `internal/bypass/Strategy` interface. The forwarder calls the strategy with the first payload and the established upstream connection.

### `fragment` (`internal/bypass/fragment.go`)

Pure stream-level strategy — no raw socket required.

- Fragments the TLS ClientHello using `tlsclienthello.FragmentClientHello` according to the configured split strategy
- Writes fragments with configurable inter-fragment delay
- Works unprivileged on all platforms

### `fake_sni` (`internal/bypass/fake_sni.go`)

Two code paths depending on raw injector availability:

**With raw injector:** Waits for confirmation window, then sends the real first payload regardless of outcome (soft fallback — never terminates on confirmation failure).

**Without raw injector:**
- `ttl_trick` method: sends a fake ClientHello with a low TTL, then sends the real payload
- All other fake methods: fall back to fragmentation to avoid stream corruption

### `combined` (`internal/bypass/combined.go`)

Combines fake prelude behavior with fragmentation:

1. If raw injector present: wait for confirmation (continues even on timeout)
2. Else if TTL trick enabled: attempt fake ClientHello with low TTL
3. Always: fragment and send real payload

### `wrong_seq` (`internal/bypass/wrong_seq.go`)

Strict mode — mirrors the behavior of legacy strict bypass flows.

- Requires raw injector (startup enforces this)
- Waits for detailed confirmation status up to `WRONG_SEQ_CONFIRM_TIMEOUT_MS`
- Returns failure and aborts the connection for any non-confirmed outcome: `failed`, `timeout`, `not_registered`
- On `confirmed`: writes first payload and hands off to relay

---

## Raw injector — Linux (`internal/rawinjector/rawinjector_linux.go`)

### Mechanism

Opens an `AF_PACKET` raw socket bound to the network interface matching the configured local IP. Tracks per-source-port connection state in a `portState` map. Filters packet tracking by both upstream IP and upstream TCP port.

### Per-connection state machine

For each registered local source port:

| Step | Event | Action |
|---|---|---|
| 1 | Outbound SYN | Record client ISN as `synSeq`, set `synSeen` |
| 2 | Outbound ACK (`seq == synSeq+1`, ACK-only) | Capture packet as injection template |
| 3 | — | Build fake frame: append fake ClientHello payload, set PSH flag, set `seq = synSeq + 1 - len(fake_payload)`, recompute IP/TCP checksums |
| 4 | — | Inject frame via `AF_PACKET` |
| 5 | Inbound ACK-only (`ACK == synSeq+1`) | Mark state `confirmed` |
| 6 | Inbound RST | Mark state `failed` |

### Confirmation API

- `WaitForConfirmation(port)` — returns bool
- `WaitForConfirmationDetailed(port)` — returns status enum: `confirmed`, `failed`, `timeout`, `not_registered`

---

## Raw injector — Windows (`internal/rawinjector/rawinjector_windows.go`)

Uses WinDivert sniff and send handles with staged fallback filter chains. Per-source-port state tracking mirrors the Linux implementation. Injects crafted packets via the WinDivert send path. Emits diagnostics to help identify WinDivert load or runtime errors.

Non-Linux/non-Windows platforms use `rawinjector_stub.go`, which returns `unavailable` for all operations.

---

## Service API internals (`cmd/snispf/service_api.go`)

The service controller manages a child worker process launched as:

```
snispf --run-core --config <path>
```

### Endpoints

| Endpoint | Implementation |
|---|---|
| `/v1/status` | Returns worker process state, PID, start time, platform, and config/log paths |
| `/v1/start` | Runs config validation, then spawns child worker process |
| `/v1/stop` | Sends stop signal to worker; force-kills after grace period |
| `/v1/health` | TCP probes each enabled endpoint; parses `wrong_seq` outcome counters from worker log lines |
| `/v1/validate` | Runs config doctor; returns issues and warnings |
| `/v1/logs` | Reads and filters tail of service log file |

### Parent lifecycle guard

When started with `--service-parent-pid` (and optionally a start timestamp), the service polls the named PID and shuts itself down when the parent exits or the PID is reused by a different process. This prevents orphaned service processes when a launcher crashes.

### Why `wrong_seq` counters are log-derived

The service controller and the core worker are separate processes with no shared memory. Parsing counter values from worker log lines avoids an IPC channel and keeps the two processes decoupled.

---

## Endpoint management (`internal/utils/endpoints.go`)

- Endpoints can be defined explicitly with per-endpoint SNI overrides
- `ENDPOINT_PROBE` removes unreachable endpoints at startup
- Load balancing modes: `round_robin` (default), `random`, `failover`
- `AUTO_FAILOVER` (disabled by default) enables retry on dial failure
- Runtime failover uses base index + retry attempt offset

`wrong_seq` requires exactly one endpoint to preserve deterministic raw tracking — the injector state machine is keyed by local source port, which is only meaningful against a single known upstream IP:port pair.

---

## Observability

| Channel | What it surfaces |
|---|---|
| Structured logs | Strategy decisions, connection outcomes, raw injector events |
| `/v1/logs` | Log tail with level filtering for external clients |
| `/v1/health` | Per-endpoint TCP probe latency + `wrong_seq` outcome counters |

---

## Extending the core

### Adding a bypass strategy

1. Implement `internal/bypass/Strategy`
2. Wire the strategy name in `buildStrategy` in `cmd/snispf/main.go`
3. Add config doctor validation for any new required fields
4. Decide strict vs. soft-fallback behavior for the forwarder failure branch
5. Update `docs/examples.md` with a sample config profile

### Extending raw behavior

- Keep the state machine deterministic and keyed by local source port
- Preserve clear, named status values — avoid boolean returns where an enum is more informative
- Never write application payload before the desired packet-level confirmation in strict modes

---

## Config validation constraints

Config doctor and startup enforce these packet-crafting limits:

| Constraint | Limit | Reason |
|---|---|---|
| SNI length | ≤ 219 bytes | Avoids oversized fake ClientHello fields |
| Fake ClientHello size | ≤ 1460 bytes | Stays within standard Ethernet MTU |

Violating either constraint produces a config doctor error that blocks startup.
