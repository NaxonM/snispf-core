# SNISPF Core Beginner Guide

This guide is for first-time users who want to run the core successfully with minimal guesswork.

## 1) What You Are Running

SNISPF Core is a local TCP forwarder that sits between your client app and your upstream endpoint.

Your client connects to SNISPF locally:

- `127.0.0.1:40443` (example)

SNISPF then forwards to your configured upstream endpoint:

- `CONNECT_IP:CONNECT_PORT`

## 2) Build

From the `core/` folder:

```powershell
go build -o snispf.exe ./cmd/snispf
```

## 3) Create a Starting Config

Generate a default config:

```powershell
.\snispf.exe --generate-config .\config.json
```

Then run config doctor:

```powershell
.\snispf.exe --config .\config.json --config-doctor
```

If doctor reports errors, fix those first.

## 4) Choose a Safe First Method

For first run, use `fragment`.

Why:

- Works without raw privileges.
- Easiest to validate before trying strict raw methods.

Set in `config.json`:

```json
"BYPASS_METHOD": "fragment"
```

## 5) Run the Core (Direct Mode)

```powershell
.\snispf.exe --config .\config.json
```

If startup is successful, you should see listener logs.

## 6) Point Your Client to Local Core

In your client app, set:

- Address: `127.0.0.1`
- Port: `40443` (or your `LISTEN_PORT`)

Keep your client's protocol-specific settings as required by your own stack.

## 7) Verify Connectivity

Checklist:

1. Core process is running.
2. Client is pointing to local listen host/port.
3. Config doctor has no errors.
4. Upstream endpoint is reachable on TCP 443.

## 8) Move to Advanced Methods

### fake_sni / combined

- Can run with fallback behavior when raw injection is unavailable.
- Good next step after `fragment` works.

### wrong_seq (strict)

Use this only when prerequisites are met:

- Raw injection support:
  - Linux with root/CAP_NET_RAW.
  - Windows with Administrator privileges and WinDivert available.
- Exactly one enabled endpoint.
- SNI <= 219 bytes.
- Generated fake ClientHello <= 1460 bytes.

Optional tuning:

- `WRONG_SEQ_CONFIRM_TIMEOUT_MS` (default `2000`)

## 9) Service API Mode (Optional)

If you want desktop/automation control, run service mode:

```powershell
.\snispf.exe --service --service-addr 127.0.0.1:8797
```

Useful API checks:

```powershell
Invoke-RestMethod http://127.0.0.1:8797/v1/status
Invoke-RestMethod http://127.0.0.1:8797/v1/health
Invoke-RestMethod http://127.0.0.1:8797/v1/validate
```

## 10) Multi-Listener Mode

You can run multiple local listeners in one process via `LISTENERS`.

Use this when you need separate local ports or different methods per route.

## 11) Common Mistakes

1. Running `wrong_seq` without Linux raw privileges.
2. Enabling multiple endpoints with strict `wrong_seq`.
3. Using unreachable upstream IP/SNI combinations.
4. Forgetting to point the client to local SNISPF listener.

## 12) Where to Read Next

- root `README.md` for full operational reference.
- `docs/api-contract.md` for exact service API request/response schema.
- `docs/internals.md` for deep internals and packet/state flow.