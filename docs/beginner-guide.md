# Beginner guide

This guide gets you from zero to a working SNISPF connection. Follow the steps in order — don't skip ahead.

---

## What SNISPF does

SNISPF sits between your client app and your upstream endpoint:

```
Your client  →  127.0.0.1:40443  →  SNISPF  →  188.114.98.0:443
```

Your client connects locally to SNISPF. SNISPF connects to your upstream and applies a bypass strategy before forwarding traffic. Your client never needs to know about the bypass logic.

---

## Step 1 — Build

From the repo root:

```bash
go build -o snispf.exe ./cmd/snispf
```

---

## Step 2 — Create a config

Generate a default config file:

```bash
.\snispf.exe --generate-config .\config.json
```

Then validate it:

```bash
.\snispf.exe --config .\config.json --config-doctor
```

Fix every error the doctor reports before moving on. Warnings are informational — errors will prevent startup.

---

## Step 3 — Edit the config

Open `config.json` and fill in your upstream details. The recommended default strategy is `wrong_seq`:

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "your-upstream-ip",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "your-upstream-hostname",
  "BYPASS_METHOD": "wrong_seq",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.05,
  "USE_TTL_TRICK": false,
  "FAKE_SNI_METHOD": "raw_inject",
  "WRONG_SEQ_CONFIRM_TIMEOUT_MS": 2000,
  "ENDPOINTS": [
    {
      "NAME": "primary",
      "IP": "your-upstream-ip",
      "PORT": 443,
      "SNI": "your-upstream-hostname",
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

The only fields you need to change are `CONNECT_IP`, `FAKE_SNI`, and the matching values inside `ENDPOINTS[0]`. Everything else can stay as shown.

| Field | What to put here |
|---|---|
| `CONNECT_IP` | IP address of your upstream endpoint |
| `CONNECT_PORT` | Port of your upstream endpoint (usually 443) |
| `FAKE_SNI` | Hostname associated with the upstream endpoint |
| `ENDPOINTS[0].IP` / `.SNI` | Must match `CONNECT_IP` / `FAKE_SNI` |
| `LISTEN_PORT` | Any unused local port (40443 is a safe default) |

> **Config precedence note:** When `ENDPOINTS` is defined, runtime dial values come from there rather than the top-level `CONNECT_IP`/`CONNECT_PORT`/`FAKE_SNI` fields. The top-level fields exist for backward compatibility. A warning is logged at startup when `ENDPOINTS[0]` overrides them.

**Can't use `wrong_seq`?** If your platform doesn't support raw packet injection, use `fragment` instead — change `BYPASS_METHOD` to `"fragment"` and remove the `WRONG_SEQ_CONFIRM_TIMEOUT_MS` field. See [Moving to other bypass strategies](#moving-to-other-bypass-strategies) for the full picture.

---

## Step 4 — Open a privileged terminal and run

`wrong_seq` requires raw packet injection, which needs elevated privileges.

**Windows — run as Administrator:**
1. Right-click your terminal (PowerShell or CMD) and choose **Run as administrator**
2. Make sure `WinDivert.dll` and `WinDivert64.sys` are in the same directory as `snispf.exe` — these ship with the Windows release bundle
3. Run:

```powershell
.\snispf.exe --config .\config.json
```

**Linux — grant capability or run as root:**

```bash
# One-time capability grant (preferred over running as root)
sudo setcap cap_net_raw+ep ./snispf

# Then run normally
./snispf --config ./config.json
```

**Not sure if raw injection is available on your system?**

```bash
.\snispf.exe --info
```

If raw injection is unavailable, the output includes a `raw_injection_diagnostic` field explaining why. In that case, switch to `fragment` (see [Moving to other bypass strategies](#moving-to-other-bypass-strategies)).

You should see listener logs indicating SNISPF is running and waiting for connections.

---

## Step 5 — Point your client

In your client application, set:

- **Address:** `127.0.0.1`
- **Port:** `40443` (or whatever you set as `LISTEN_PORT`)

Leave all other client settings unchanged. SNISPF is transparent — your client behaves as if it's connecting directly to the upstream.

---

## Step 6 — Verify it's working

Go through this checklist:

- [ ] SNISPF process is running with no error logs
- [ ] Client is pointing to the local listener address and port
- [ ] Config doctor reported no errors
- [ ] Upstream endpoint is reachable on TCP port 443

If traffic isn't flowing, jump to the [Troubleshooting](#troubleshooting) section below.

---

## Moving to other bypass strategies

`wrong_seq` is the recommended default. Only move to a different strategy if it doesn't work on your platform.

### `combined` and `fake_sni`

Both fall back gracefully when raw injection is unavailable and produce more aggressive bypass than `fragment` alone. Use these if `wrong_seq` prerequisites can't be met.

```json
"BYPASS_METHOD": "combined"
```

### `fragment`

The simplest strategy — splits the TLS ClientHello without any raw injection. Works unprivileged everywhere. Use this for diagnosis or when no other strategy is viable.

```json
"BYPASS_METHOD": "fragment"
```

### `wrong_seq` prerequisites recap

- **Exactly one enabled endpoint** in your config
- **Privileged terminal:**
  - Windows: Administrator + `WinDivert.dll` + `WinDivert64.sys` alongside the binary
  - Linux: `root` or `CAP_NET_RAW` granted via `setcap`
  - OpenWrt: `root` or `CAP_NET_RAW` + AF_PACKET support
- **SNI hostname ≤ 219 characters**
- **Generated fake ClientHello ≤ 1460 bytes** (validated by `--config-doctor`)

---

## Using service API mode (optional)

If you want another process — a desktop app, launcher, or script — to control SNISPF, run it in service mode instead:

```bash
.\snispf.exe --service --service-addr 127.0.0.1:8797
```

With an auth token:

```bash
.\snispf.exe --service --service-addr 127.0.0.1:8797 --service-token your-token
```

Useful status checks (PowerShell):

```powershell
Invoke-RestMethod http://127.0.0.1:8797/v1/status
Invoke-RestMethod http://127.0.0.1:8797/v1/health
Invoke-RestMethod http://127.0.0.1:8797/v1/validate
```

Full API documentation: [`api-contract.md`](api-contract.md)

---

## Running multiple listeners

You can run more than one local listener in a single SNISPF process using the `LISTENERS` array. Use this when you need different local ports, different upstream endpoints, or different bypass strategies running simultaneously.

See the root `README.md` for a full multi-listener example.

---

## Troubleshooting

### SNISPF starts but traffic doesn't flow

1. Confirm your client is connecting to `127.0.0.1:LISTEN_PORT`, not directly to the upstream.
2. Check startup logs for connection errors to the upstream IP.
3. Run `/v1/health` (service mode) or look for probe errors in startup logs.

### Config doctor reports errors

Fix all errors before running. Common causes:
- `CONNECT_IP` is a hostname instead of an IP address — resolve it first
- `CONNECT_PORT` is out of range
- `wrong_seq` selected but multiple endpoints configured

### `wrong_seq` connections are failing

Check `/v1/logs` or service logs for these outcome codes:

| Code | Meaning |
|---|---|
| `confirmed` | Bypass succeeded |
| `timeout` | Raw confirmation window expired — try increasing `WRONG_SEQ_CONFIRM_TIMEOUT_MS` |
| `failed` | Upstream sent RST — endpoint may be blocking the technique |
| `not_registered` | Flow wasn't registered before dial — may indicate a race condition |
| `first_write_fail` | First payload write failed after confirmation |

### Raw injection isn't working on Linux

```bash
# Check current capability
.\snispf.exe --info

# Grant capability without running as root
sudo setcap cap_net_raw+ep ./snispf
```

---

## Common mistakes

- Running `wrong_seq` without the required platform privileges
- Configuring multiple endpoints while using `wrong_seq`
- Setting `CONNECT_IP` to a hostname (must be a resolved IP)
- Forgetting to point the client to the local SNISPF address after config changes

---

## What to read next

- **Root `README.md`** — full operational reference including multi-listener, OpenWrt, and release builds
- **[`api-contract.md`](api-contract.md)** — exact service API request/response schema
- **[`internals.md`](internals.md)** — architecture and code internals (for contributors and advanced debugging)
