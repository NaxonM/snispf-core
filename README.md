# SNISPF Core

Terminal-first DPI bypass core for Go. Runs headless as a stable local TCP forwarder between your client and an upstream endpoint.

Implements [@patterniha's SNI-Spoofing](https://github.com/patterniha/SNI-Spoofing) technique. All credit for the original method goes to [@patterniha](https://github.com/patterniha).

Persian guide: `README_fa.md`

---

## How it works

```
Your client  →  SNISPF (127.0.0.1:LISTEN_PORT)  →  Upstream endpoint (CONNECT_IP:CONNECT_PORT)
```

SNISPF intercepts the outbound TLS ClientHello and applies a bypass strategy before forwarding. Your client config stays unchanged — all bypass logic lives in SNISPF.

---

## Quickstart

### 1. Build

```bash
go build -o snispf.exe ./cmd/snispf
```

### 2. Generate and validate config

```bash
.\snispf.exe --generate-config .\config.json
.\snispf.exe --config .\config.json --config-doctor
```

Fix any errors the doctor reports before continuing.

### 3. Run

> **`wrong_seq` requires a privileged terminal.** On Windows, run as Administrator. On Linux, run as root or grant `CAP_NET_RAW` first. See [Bypass strategies](#bypass-strategies) for details.

```bash
.\snispf.exe --config .\config.json
```

### 4. Point your client

```
Address: 127.0.0.1
Port:    40443  (or your configured LISTEN_PORT)
```

Keep all other client protocol settings unchanged.

---

## Recommended config

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "203.0.113.10",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "edge-a.example.com",
  "BYPASS_METHOD": "wrong_seq",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.05,
  "USE_TTL_TRICK": false,
  "FAKE_SNI_METHOD": "raw_inject",
  "WRONG_SEQ_CONFIRM_TIMEOUT_MS": 2000,
  "ENDPOINTS": [
    {
      "NAME": "strict-primary",
      "IP": "203.0.113.10",
      "PORT": 443,
      "SNI": "edge-a.example.com",
      "ENABLED": true
    }
  ],
  "LOAD_BALANCE": "failover",
  "ENDPOINT_PROBE": true,
  "AUTO_FAILOVER": false,
  "FAILOVER_RETRIES": 0,
  "PROBE_TIMEOUT_MS": 2500
}
```

| Field | Description |
|---|---|
| `LISTEN_HOST:LISTEN_PORT` | Local address your client connects to |
| `CONNECT_IP:CONNECT_PORT` | Upstream destination SNISPF dials |
| `FAKE_SNI` | SNI used by `fake_sni` and `combined` strategies |
| `BYPASS_METHOD` | Strategy: `fragment`, `fake_sni`, `combined`, or `wrong_seq` |
| `FRAGMENT_STRATEGY` | How to split the ClientHello (e.g. `sni_split`) |
| `FRAGMENT_DELAY` | Inter-fragment delay in seconds |
| `USE_TTL_TRICK` | Send fake ClientHello with low TTL before the real one |
| `FAKE_SNI_METHOD` | Fake SNI method: `raw_inject`, `prefix_fake`, etc. |
| `WRONG_SEQ_CONFIRM_TIMEOUT_MS` | Confirmation window for `wrong_seq` mode (default 2000) |
| `LOAD_BALANCE` | Endpoint selection: `round_robin`, `random`, `failover` |
| `ENDPOINT_PROBE` | Remove unreachable endpoints at startup |
| `AUTO_FAILOVER` | Retry on dial failure |
| `FAILOVER_RETRIES` | Number of failover attempts |
| `PROBE_TIMEOUT_MS` | Endpoint probe timeout in milliseconds |
| `LOG_LEVEL` | Verbosity: `error`, `warn`, `info`, `debug` |

**Config precedence:** If `ENDPOINTS` is defined, runtime dial values come from there. Top-level `CONNECT_IP`, `CONNECT_PORT`, and `FAKE_SNI` remain as backward-compatible defaults. A startup warning is logged when `ENDPOINTS[0]` overrides top-level values.

---

## Bypass strategies

`wrong_seq` is the recommended default. It produces the most effective bypass when platform prerequisites are met. Fall back to simpler strategies only if they are not.

| Strategy | Privilege required | Recommended when |
|---|---|---|
| **`wrong_seq`** | Yes — see table below | Default choice when prerequisites are met |
| `combined` | No (degrades gracefully) | Raw injection unavailable but aggressive bypass needed |
| `fake_sni` | No (degrades gracefully) | Lighter alternative to `combined` |
| `fragment` | No | Diagnosis, constrained environments, or as a last resort |

### Platform and privilege requirements

| Platform | `fragment`, `fake_sni`, `combined` | `wrong_seq` |
|---|---|---|
| Linux | Unprivileged | **Privileged terminal** — `root` or `CAP_NET_RAW` |
| Windows | Normal | **Administrator terminal** + `WinDivert.dll` + `WinDivert64.sys` in the same directory as the binary |
| OpenWrt | Normal | **Privileged** — `CAP_NET_RAW` or root + AF_PACKET support |

> **Windows:** The Windows release bundle includes `WinDivert.dll` and `WinDivert64.sys`. Place them in the same directory as `snispf.exe` and run from an Administrator terminal. Without both files, `wrong_seq` will fail to initialize and `--info` will report the reason.

> **Linux:** Either run as root, or grant the capability once: `sudo setcap cap_net_raw+ep ./snispf`

`wrong_seq` additional constraints:
- Exactly one enabled endpoint
- SNI length ≤ 219 bytes
- Generated fake ClientHello ≤ 1460 bytes (both validated by `--config-doctor`)

Run `.\snispf.exe --info` to inspect runtime capability flags. This flag is config-independent.

---

## Run modes

### Direct mode

```bash
.\snispf.exe --config .\config.json
```

One-off flag overrides (do not persist to config):

```bash
.\snispf.exe --config .\config.json --listen 0.0.0.0:40443 --connect 188.114.98.0:443 --sni auth.vercel.com --method combined
```

### Service API mode

Exposes an HTTP control API for desktop apps, launchers, and automation.

```bash
.\snispf.exe --service --service-addr 127.0.0.1:8797
.\snispf.exe --service --service-addr 127.0.0.1:8797 --service-token your-token
```

API base URL: `http://127.0.0.1:8797`

| Endpoint | Method | Description |
|---|---|---|
| `/v1/status` | GET | Worker state, PID, start time |
| `/v1/start` | POST | Validate config and start worker |
| `/v1/stop` | POST | Stop worker |
| `/v1/health` | GET | Endpoint TCP probe + `wrong_seq` counters |
| `/v1/validate` | GET | Config doctor results |
| `/v1/logs` | GET | Log tail (`?limit=300&level=ALL`) |

When a token is configured, send `X-SNISPF-Token: <token>` with every request.

Recommended troubleshooting order: `/v1/status` → `/v1/validate` → `/v1/health` → `/v1/logs`

Full request/response schema: [`docs/api-contract.md`](docs/api-contract.md)

---

## Multi-listener mode

Run multiple local listeners in one process:

```json
{
  "BYPASS_METHOD": "wrong_seq",
  "LISTENERS": [
    {
      "NAME": "edge-a",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40443,
      "CONNECT_IP": "104.19.229.21",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "hcaptcha.com"
    },
    {
      "NAME": "edge-b",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40444,
      "CONNECT_IP": "104.19.229.22",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "hcaptcha.com",
      "BYPASS_METHOD": "fragment"
    }
  ]
}
```

Each listener runs independently. When `LISTENERS` is present, per-listener values override any top-level defaults.

---

## Linux service deployment

Linux bundles include a systemd template and installer:

```bash
sudo bash ./install_linux_service.sh install --binary ./snispf_linux_amd64 --config ./config.json
sudo bash ./install_linux_service.sh status
sudo bash ./install_linux_service.sh restart
sudo bash ./install_linux_service.sh logs --lines 120
```

The default unit sets `Restart=always` and `LimitNOFILE=65535`.

Core recovery behavior (all platforms):

- With multiple enabled `ENDPOINTS` and load balancing configured, the core now chains fallback attempts across endpoints when dial or strategy confirmation fails.
- If critical failures repeat in a short window (for example repeated `upstream_unreachable` or confirmation failures), the core can trigger an internal runtime rebuild as a final recovery step.

---

## OpenWrt deployment

Build and copy an architecture bundle to the router:

```bash
powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1
scp ./release/openwrt/snispf_openwrt_x86_64_bundle.tar.gz root@192.168.1.1:/tmp/
```

Install on the router:

```sh
ssh root@192.168.1.1
cd /tmp && tar -xzf snispf_openwrt_x86_64_bundle.tar.gz && cd snispf_openwrt_bundle
ash ./openwrt_snispf.sh install --binary ./snispf --config ./config.json
```

The bundle includes the binary, `openwrt_snispf.sh`, and a generated default config.

**Watchdog:** Installed interactively by default (every 1 minute). Restarts on down process, missing listen port, or degraded raw-injector log patterns. Force-install or tune:

```sh
ash ./openwrt_snispf.sh watchdog-install
ash ./openwrt_snispf.sh install --binary ./snispf --config ./config.json --watchdog auto --post-restart-delay 20
```

Useful operations:

```sh
ash ./openwrt_snispf.sh status
ash ./openwrt_snispf.sh logs --follow
ash ./openwrt_snispf.sh monitor --watch 30 --interval 2
ash ./openwrt_snispf.sh doctor
```

For `wrong_seq` on OpenWrt:

```sh
setcap cap_net_raw+ep /path/to/snispf
```

> **Note:** If logs show `socket: too many open files`, reinstall with the latest `openwrt_snispf.sh` to get the procd `nofile` limit fix.

---

## Build and release

### Local

```bash
go build -o snispf.exe ./cmd/snispf
```

### Cross-build scripts

| Target | Command |
|---|---|
| Windows amd64 | `powershell -ExecutionPolicy Bypass -File .\scripts\build_windows_amd64.ps1` |
| Linux amd64 (PowerShell) | `powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1` |
| Linux amd64 (bash) | `bash ./scripts/build_linux_amd64.sh` |
| Full release matrix | `powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1` |
| OpenWrt matrix (PowerShell) | `powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1` |
| OpenWrt matrix (bash) | `bash ./scripts/build_openwrt_matrix.sh` |

Verification:

```bash
powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1
bash ./scripts/verify_release.sh
```

### Release outputs

- Core binaries: `release/snispf_windows_amd64.exe`, `release/snispf_linux_amd64`, `release/snispf_linux_arm64`
- Bundles: `release/snispf_*_bundle.{zip,tar.gz}`
- OpenWrt: `release/openwrt/` — per-arch binaries, per-arch bundles, `openwrt_snispf.sh`, `openwrt_default_config.json`
- Metadata: `release/checksums.txt`, `release/release_manifest.json`

OpenWrt matrix architectures: `armv7`, `armv6`, `mipsle_softfloat`, `mips_softfloat`, `arm64`, `x86_64`

### GitHub Actions

Workflow: `.github/workflows/release.yml`

- `workflow_dispatch` — draft/test builds
- Push tag (e.g. `v1.2.3`) — full release with checksums and manifest

---

## CLI reference

```
--config <path>           Load config file
--generate-config <path>  Write default config to path
--config-doctor           Validate config and exit
--info                    Show platform capability flags (no config required)
--listen <host:port>      Override listen address
--connect <ip:port>       Override upstream address
--sni <hostname>          Override SNI
--method <strategy>       Override bypass strategy
--service                 Start in service API mode
--service-addr <host:port>
--service-token <token>
--build-info / --version
```

Backward-compatible subcommand aliases: `snispf run`, `snispf service`, `snispf doctor`, `snispf build-info`

---

## Verification checklist

```bash
go test ./...
go vet ./...
go build -o snispf.exe ./cmd/snispf
powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\integration_service_lifecycle.ps1
```

---

## Troubleshooting

1. Run `--config-doctor` and fix all reported errors.
2. Confirm your client is pointing to the local SNISPF listener address and port.
3. Confirm upstream reachability via `/v1/health` or startup logs.
4. For `wrong_seq`: verify platform privilege and single-endpoint constraint.
5. Check `/v1/logs` for `timeout`, `failed`, and `not_registered` outcomes.

---

## Documentation

| Doc | Audience |
|---|---|
| [`docs/beginner-guide.md`](docs/beginner-guide.md) | First-time setup and troubleshooting |
| [`docs/api-contract.md`](docs/api-contract.md) | Service API schema and compatibility |
| [`docs/internals.md`](docs/internals.md) | Architecture, code paths, contributing |
| [`docs/examples.md`](docs/examples.md) | Annotated config profiles |
| [`docs/roadmap.md`](docs/roadmap.md) | Planned direction and non-goals |
