# SNISPF Core Inner Workings

This document explains how the Go core works internally, from process startup to packet-level bypass behavior.

## 1) High-level architecture

The core has two execution models:

- Direct core mode: one process that accepts local TCP connections and forwards to upstream endpoints with bypass strategies.
- Service API mode: a control process that exposes HTTP endpoints and starts/stops a child core worker process.

Main code areas:

- `cmd/snispf/main.go`: bootstrap, CLI flags, config loading, strategy selection, and runtime startup.
- `cmd/snispf/service_api.go`: localhost control API (`/v1/status`, `/v1/start`, `/v1/stop`, `/v1/health`, `/v1/validate`, `/v1/logs`).
- `internal/forwarder/server.go`: listener and per-connection forwarding loop.
- `internal/bypass/*`: strategy implementations.
- `internal/rawinjector/*`: raw packet monitor/injector implementation and platform stubs.
- `internal/tlsclienthello/*`: ClientHello parsing/building/fragmentation helpers.
- `internal/utils/*`: config schema, normalization, endpoint probing, and platform capability checks.

## 2) Startup and configuration pipeline

In direct mode, startup flow in `main.go` is:

1. Parse CLI flags and normalize aliases.
2. Load config JSON (or defaults if generating config).
3. Apply CLI overrides for listen/connect/sni/method/fragment options.
4. Validate port ranges.
5. Normalize config (`utils.NormalizeConfig`):
   - Fill defaults.
   - Apply inherited defaults to each `LISTENERS[]` entry when present.
   - Materialize endpoint list when missing.
   - Resolve hostnames to IPs.
   - Ensure `WRONG_SEQ_CONFIRM_TIMEOUT_MS` default (2000 ms).
6. Emit startup precedence warnings when top-level upstream fields conflict with `ENDPOINTS[0]`.
7. Filter to enabled/valid endpoints (`utils.EnabledEndpoints`).
8. Optional endpoint health probing (`utils.ProbeHealthyEndpoints`).
9. Build optional raw injector (single endpoint only, method-dependent, platform-dependent).
10. Build bypass strategy implementation.
11. Start forwarder server with cancellation context.

If `LISTENERS` is configured, the core starts one forwarder per listener inside the same process.

Key behavior note: `wrong_seq` is strict. Startup enforces:

- Exactly one enabled endpoint.
- Raw injector availability on Linux with needed privileges.

## 3) Forwarding data plane (per connection)

Connection lifecycle in `internal/forwarder/server.go`:

1. Accept inbound TCP client.
2. Read first payload (expected TLS ClientHello in common usage).
3. Parse incoming ClientHello for observability/logging.
4. Select upstream endpoint based on load-balancer base index and failover attempts.
5. Dial upstream TCP connection.
6. Apply bypass strategy to first payload.
7. If strategy fails:
   - `wrong_seq`: terminate connection (strict semantics).
   - other methods: fallback to writing first payload directly.
8. Start bidirectional relay (`io.Copy` in both directions).

### Why pre-connect raw registration was added

For raw-assisted modes, the forwarder reserves a local source port, registers that port with the injector, then dials upstream using that specific source port. This lets the raw injector see SYN/ACK handshake packets for the same tracked flow before confirmation wait begins.

## 4) Bypass strategy internals

### fragment

File: `internal/bypass/fragment.go`

- Pure stream-level strategy.
- Fragments first TLS payload (`tlsclienthello.FragmentClientHello`) based on configured split strategy.
- Writes fragments with configurable delay.
- No raw socket requirement.

### fake_sni

File: `internal/bypass/fake_sni.go`

Two tracks:

- Raw-injector active:
  - Wait for confirmation window.
  - Send real first payload regardless of confirmation outcome (soft fallback behavior).
- Non-raw path:
  - `ttl_trick`: send fake hello with low TTL, then send real payload.
  - Other fake methods map to safe fragmentation fallback to avoid stream corruption.

### combined

File: `internal/bypass/combined.go`

- Mixes fake prelude behavior and fragmentation.
- If raw injector exists: waits for confirmation but continues even on timeout.
- Else if TTL trick enabled: attempts fake hello with low TTL.
- Then always fragments and sends real payload.

### wrong_seq (strict)

File: `internal/bypass/wrong_seq.go`

- Implements strict behavior modeled after legacy strict flows.
- Requires raw injector.
- Waits for detailed confirmation status up to `WRONG_SEQ_CONFIRM_TIMEOUT_MS`.
- On non-confirmed statuses (`failed`, `timeout`, `not_registered`): returns failure and connection is not relayed.
- On success: writes first payload and proceeds to relay.

## 5) Raw injector internals (Linux)

File: `internal/rawinjector/rawinjector_linux.go`

### Core mechanism

- Opens AF_PACKET raw socket and binds to interface matching selected local IP.
- Tracks per-source-port state (`portState`) for monitored connection flows.
- Observes outbound/inbound TCP packets and manages a small state machine.
- Filters packet tracking by both configured upstream IP and upstream TCP port.

### State machine summary

For each registered local source port:

1. Capture outbound SYN and remember client ISN (`synSeq`, `synSeen`).
2. Capture outbound third-handshake ACK (`seq == synSeq+1`, ACK-only).
3. Build fake frame from observed template:
   - Append fake ClientHello payload.
   - Set PSH.
   - Set fake sequence to `synSeq + 1 - len(fake_payload)`.
   - Recompute IP/TCP checksums.
4. Inject frame through AF_PACKET.
5. Observe inbound ACK-only packets and mark confirmed when ACK equals `synSeq+1`.
6. Observe inbound RST and mark failed.

### Confirmation API

- `WaitForConfirmation`: boolean wrapper.
- `WaitForConfirmationDetailed`: status enum (`confirmed`, `failed`, `timeout`, `not_registered`).

## 5.1) Raw injector internals (Windows)

File: `internal/rawinjector/rawinjector_windows.go`

- Uses WinDivert sniff + send handles with staged fallback filter chains.
- Tracks per-source-port state similarly to Linux flow registration.
- Injects crafted packets via WinDivert send path and waits for confirmation statuses.
- Emits diagnostics to help identify WinDivert runtime/load errors.

Other non-Linux/non-Windows builds use `rawinjector_stub.go` with unavailable/no-op behavior.

## 6) Service mode internals

File: `cmd/snispf/service_api.go`

Service mode runs as a controller process and manages a worker child process.

Endpoints:

- `/v1/status`: worker running state, PID, start time, paths, platform.
- `/v1/start`: validates config and launches child worker (`--run-core --config ...`).
- `/v1/stop`: stops worker process.
- `/v1/health`: endpoint TCP probe plus `wrong_seq` counters parsed from log lines.
- `/v1/validate`: returns doctor issues/warnings.
- `/v1/logs`: returns filtered/tail logs.

### Parent lifecycle guard

When `--service-parent-pid` and optional start timestamp are passed, service mode periodically verifies parent identity to avoid PID reuse errors and self-shuts down when parent exits/changes.

## 7) Endpoint management and failover model

Config and endpoint handling in `internal/utils/endpoints.go`:

- Endpoints can be explicitly configured with per-endpoint SNI.
- `ENDPOINT_PROBE` can remove dead endpoints at startup.
- Load balancing modes:
  - `round_robin`
  - `random`
  - `failover`
- Default load balancing mode is `round_robin`.
- `AUTO_FAILOVER` is disabled by default and enables retry attempts on endpoint dial failures.
- Runtime dial failover uses base index + retry attempts.

Important interaction: strict `wrong_seq` currently expects one endpoint to preserve deterministic raw tracking semantics.

## 8) Observability model

Current observability channels:

- Standard logs for strategy and runtime decisions.
- `/v1/logs` for external clients.
- `/v1/health` for endpoint probes and `wrong_seq` outcome counters.

`wrong_seq` counters are log-derived in service mode because service and worker are separate processes; this avoids shared-memory coupling.

## 9) Platform behavior and privilege assumptions

- Fragmentation path works unprivileged on all supported platforms.
- Raw injection path requires Linux AF_PACKET raw socket capability.
- Capability hints are surfaced via `--info` and config doctor warnings.

## 10) Why this design

The core favors:

- A small data plane (`forwarder + strategy`) with isolated strategy implementations.
- Explicit strict mode (`wrong_seq`) versus soft-fallback modes (`fake_sni`, `combined`).
- A stable control-plane API in service mode for desktop and automation clients.
- Pragmatic observability through logs and API snapshots without heavyweight runtime dependencies.
- Multi-listener consolidation so one process can host multiple local entry points without extra supervisors.

## 11) Extending the core safely

To add a new bypass strategy:

1. Implement `internal/bypass/Strategy` interface.
2. Wire selection in `buildStrategy` in `cmd/snispf/main.go`.
3. Add config doctor validation and CLI help text.
4. Decide strict-vs-fallback behavior in forwarder failure branch.
5. Update docs and sample configs.

To extend raw behavior:

1. Keep state machine deterministic and flow-keyed by local source port.
2. Preserve clear success/failure statuses.
3. Avoid writing application payload before desired packet-level confirmation in strict modes.

## 12) Guardrails

Config doctor and startup checks enforce key packet-crafting constraints:

- SNI length must be <= 219 bytes.
- Generated fake ClientHello size must be <= 1460 bytes.

These limits reduce malformed packets and MTU-related issues in raw injection paths.