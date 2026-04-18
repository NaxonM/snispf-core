# SNISPF Core (Go)

Terminal-first DPI bypass core, designed to run headless as a stable runtime process.

This core follows [patterniha's SNI-Spoofing](https://github.com/patterniha/SNI-Spoofing) DPI bypass technique. All credit for the original idea and method goes to [@patterniha](https://github.com/patterniha).

Persian guide: `README_fa.md`

## What This Core Does

SNISPF runs as a local TCP forwarder between your client and upstream endpoint:

1. Your client connects locally to SNISPF (`LISTEN_HOST:LISTEN_PORT`).
2. SNISPF connects to your upstream endpoint (`CONNECT_IP:CONNECT_PORT`).
3. SNISPF applies a bypass strategy (`fragment`, `fake_sni`, `combined`, or strict `wrong_seq`).

This design keeps your client config simple and moves bypass behavior into one controllable core process.

## Before You Start

Use this section as a quick decision table for permissions and prerequisites.

| Platform | Basic methods (`fragment`, `fake_sni`, `combined`) | Strict `wrong_seq` |
|---|---|---|
| Linux | Works unprivileged | Requires raw packet capability (`root` or `CAP_NET_RAW`) |
| Windows | Works normally | Requires Administrator + `WinDivert.dll` + `WinDivert64.sys` |
| OpenWrt | Works normally | Requires `CAP_NET_RAW`/root and AF_PACKET support |

Run this to inspect runtime capability flags:

```powershell
.\snispf.exe --info
```

If raw injection is unavailable, `--info` can print `raw_injection_diagnostic=...` with the reason.

## Quickstart (4 Steps)

### Step 1) Build

```powershell
go build -o snispf.exe ./cmd/snispf
```

### Step 2) Generate and Validate Config

```powershell
.\snispf.exe --generate-config .\config.json
.\snispf.exe --config .\config.json --config-doctor
```

### Step 3) Configure Minimal Safe Profile

Start from the safest baseline (`fragment`):

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "188.114.98.0",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "auth.vercel.com",
  "BYPASS_METHOD": "fragment"
}
```

Field mapping:

| Field | Meaning |
|---|---|
| `LISTEN_HOST:LISTEN_PORT` | Local address your client should connect to |
| `LOG_LEVEL` | Runtime verbosity: `error`, `warn`, `info`, `debug` |
| `CONNECT_IP:CONNECT_PORT` | Upstream destination SNISPF dials |
| `FAKE_SNI` | SNI used by fake/combined logic and endpoint defaults |
| `BYPASS_METHOD` | Strategy (`fragment`, `fake_sni`, `combined`, `wrong_seq`) |

### Step 4) Run and Point Client

```powershell
.\snispf.exe --config .\config.json
```

Set your client to:

- Address: `127.0.0.1`
- Port: `40443` (or your configured `LISTEN_PORT`)

Keep the rest of your client protocol settings unchanged.

## Choosing a Bypass Method

Use this order unless you have a specific reason not to:

1. `fragment` (best first run)
2. `fake_sni` or `combined` (next step after baseline stability)
3. `wrong_seq` only when strict prerequisites are met

`wrong_seq` guardrails and requirements:

1. Exactly one enabled endpoint.
2. Raw injection available on current platform.
3. SNI length <= `219` bytes.
4. Generated fake ClientHello size <= `1460` bytes.
5. Optional timeout tuning: `WRONG_SEQ_CONFIRM_TIMEOUT_MS` (default `2000`).
6. For multi-WAN/multi-WLAN route changes, `wrong_seq` may need restart to rebind raw injector.

Multi-WAN practical note:

- `wrong_seq` is strict mode and is best with a single stable upstream path.
- For automatic per-connection route adaptation across changing WAN paths, prefer `fragment`/`combined`.

## Run Modes

### Direct Mode (Simplest)

```powershell
.\snispf.exe --config .\config.json
```

Optional one-off overrides:

```powershell
.\snispf.exe --config .\config.json --listen 0.0.0.0:40443 --connect 188.114.98.0:443 --sni auth.vercel.com --method combined
```

### Service API Mode (Desktop/Automation)

```powershell
.\snispf.exe --service --service-addr 127.0.0.1:8797
```

With auth token:

```powershell
.\snispf.exe --service --service-addr 127.0.0.1:8797 --service-token your-token
```

Use service mode when another process (UI, launcher, script) should control start/stop/health.

## Service API Quick Reference

Base URL: `http://127.0.0.1:8797` (or your `--service-addr`)

- `GET /v1/status`
- `POST /v1/start`
- `POST /v1/stop`
- `GET /v1/health`
- `GET /v1/validate`
- `GET /v1/logs?limit=300&level=ALL`

If token is enabled, send header `X-SNISPF-Token: <token>`.

Recommended troubleshooting order:

1. `/v1/status`
2. `/v1/validate`
3. `/v1/health`
4. `/v1/logs`

`/v1/health` includes `wrong_seq` counters from logs:

- `confirmed`
- `timeout`
- `failed`
- `not_registered`
- `first_write_fail`

Full request/response contract: `docs/api-contract.md`

## OpenWrt Deployment (Practical Flow)

Build OpenWrt artifacts:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1
```

Copy to router:

```bash
scp ./release/openwrt/snispf_openwrt_armv7 root@192.168.1.1:/tmp/
scp ./config.json root@192.168.1.1:/tmp/snispf_config.json
scp ./release/openwrt/openwrt_snispf.sh root@192.168.1.1:/tmp/
```

Install and run on router:

```sh
ssh root@192.168.1.1
chmod +x /tmp/openwrt_snispf.sh
ash /tmp/openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --config /tmp/snispf_config.json
```

Installer behavior (default):

- Schedules one delayed restart after install/start (default `20s`).
- Asks to install watchdog in interactive shell (`--watchdog ask`).
- In non-interactive mode, `ask` behaves like auto install.

Watchdog defaults and tuning:

- Default schedule is every `1` minute.
- It restarts on down process, missing listen port, and degraded raw-injector patterns in logs.

Force watchdog install or tune delayed restart:

```sh
ash /tmp/openwrt_snispf.sh watchdog-install
ash /tmp/openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --config /tmp/snispf_config.json --watchdog auto --post-restart-delay 20
```

Useful operations:

```sh
ash /tmp/openwrt_snispf.sh status
ash /tmp/openwrt_snispf.sh logs --follow
ash /tmp/openwrt_snispf.sh monitor --watch 30 --interval 2
/tmp/openwrt_snispf.sh doctor
```

For strict `wrong_seq` on OpenWrt, use root or grant capability:

```sh
setcap cap_net_raw+ep /path/to/snispf_openwrt_armv7
```

## Build and Release Scripts

Local build:

```powershell
go build -o snispf.exe ./cmd/snispf
```

Cross-build scripts:

- Windows amd64: `powershell -ExecutionPolicy Bypass -File .\scripts\build_windows_amd64.ps1`
- Linux amd64 (PowerShell): `powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1`
- Linux amd64 (bash): `bash ./scripts/build_linux_amd64.sh`
- Full release matrix: `powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1`
- OpenWrt matrix (PowerShell): `powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1`
- OpenWrt matrix (bash): `bash ./scripts/build_openwrt_matrix.sh`

Verification scripts:

- `powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1`
- `bash ./scripts/verify_release.sh`

Release outputs:

- Core binaries: `release/snispf_windows_amd64.exe`, `release/snispf_linux_amd64`, `release/snispf_linux_arm64`
- Bundled archives: `release/snispf_windows_amd64_bundle.zip`, `release/snispf_linux_amd64_bundle.tar.gz`, `release/snispf_linux_arm64_bundle.tar.gz`
- Metadata: `release/checksums.txt`, `release/release_manifest.json`
- OpenWrt: `release/openwrt/` (includes binaries + `openwrt_snispf.sh`), `release/openwrt/checksums.txt`, `release/openwrt/release_manifest.json`

## GitHub Actions Release

Workflow: `.github/workflows/release.yml`

1. Trigger manually with `workflow_dispatch` for draft/test release builds.
2. Push tag (for example `v1.2.3`) to build and publish assets.
3. Workflow publishes both core and OpenWrt artifacts with checksums/manifest.

## CLI Snapshot

Common flags:

- `--config`, `--generate-config`, `--config-doctor`, `--info`
- `--listen`, `--connect`, `--sni`, `--method`
- `--service`, `--service-addr`, `--service-token`
- `--build-info`, `--version`

Backward-compatible aliases:

- `snispf run ...` -> direct core mode
- `snispf service ...` -> service mode
- `snispf doctor ...` -> config doctor
- `snispf build-info` -> build metadata

## Multi-Listener Example

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

When `LISTENERS` is present, each listener runs independently in the same process.

## Verification Checklist

```powershell
go test ./...
go vet ./...
go build -o snispf.exe ./cmd/snispf
powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1
```

Windows service lifecycle integration:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\integration_service_lifecycle.ps1
```

## Docs Map

- `docs/README.md`: documentation index and reading order.
- `docs/beginner-guide.md`: first-time setup and troubleshooting path.
- `docs/api-contract.md`: full service API contract.
- `docs/internals.md`: detailed architecture and data path.
- `docs/examples.md`: sanitized example profiles.
- `docs/roadmap.md`: planned future direction.

## Troubleshooting Checklist

1. Run config doctor and fix reported errors.
2. Confirm client points to local SNISPF listener.
3. Confirm upstream reachability (`/v1/health` or startup logs).
4. For `wrong_seq`, verify platform privilege and single endpoint.
5. Inspect `/v1/logs` for `timeout`, `failed`, and `not_registered` outcomes.
