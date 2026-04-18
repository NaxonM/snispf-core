# Service API contract

Base URL is configured with `--service-addr` (default `127.0.0.1:8797`).

---

## Versioning

- Current version: `v1`
- Responses include `api_version` where applicable
- Within `v1`: new fields may be added without a breaking change
- Removal or rename of existing fields requires a new major version
- Clients must ignore unknown JSON fields

---

## Authentication

When SNISPF is started with `--service-token`, all endpoints require the token in one of:

- Header: `X-SNISPF-Token: <token>`
- Query parameter: `?token=<token>`

Unauthorized response:

```json
{ "error": "unauthorized" }
```

---

## Endpoints

### `GET /v1/status`

Returns the service and worker process state.

```json
{
  "api_version": "v1",
  "running": true,
  "pid": 12345,
  "started_at": "2026-04-14T14:00:00Z",
  "last_error": "",
  "log_path": "C:/Users/user/AppData/Roaming/snispf/logs/service.log",
  "config_path": "./config.json",
  "platform": "windows",
  "architecture": "amd64"
}
```

---

### `POST /v1/start`

Validates the current config and starts the core worker.

**Success:** Same schema as `/v1/status`.

**Errors:**

```json
{ "error": "core already running" }
```

```json
{ "error": "config has 1 issue(s); call /v1/validate" }
```

---

### `POST /v1/stop`

Stops the core worker if running.

**Response:** Same schema as `/v1/status`.

---

### `GET /v1/validate`

Runs config doctor checks and returns all issues and warnings.

```json
{
  "api_version": "v1",
  "issues": [],
  "warnings": [
    "raw injection unavailable; using fallback behavior"
  ]
}
```

`issues` block startup. `warnings` are informational.

---

### `GET /v1/health`

TCP-probes each enabled endpoint and returns `wrong_seq` outcome counters parsed from recent logs.

```json
{
  "api_version": "v1",
  "checked_at": "2026-04-14T14:01:00Z",
  "endpoints": [
    {
      "name": "primary",
      "ip": "104.18.38.202",
      "port": 443,
      "sni": "auth.vercel.com",
      "healthy": true,
      "latency_ms": 37,
      "error": ""
    }
  ],
  "wrong_seq": {
    "confirmed": 10,
    "timeout": 0,
    "failed": 1,
    "not_registered": 0,
    "first_write_fail": 0,
    "source_lines": 5000
  }
}
```

`wrong_seq` counter definitions:

| Counter | Meaning |
|---|---|
| `confirmed` | Bypass succeeded — upstream acknowledged the fake sequence |
| `timeout` | Confirmation window expired before upstream ACK |
| `failed` | Upstream sent RST |
| `not_registered` | Flow was not registered before dial |
| `first_write_fail` | First payload write failed after confirmation |
| `source_lines` | Number of log lines scanned to produce the counters |

---

### `GET /v1/logs`

Returns a filtered tail of the service log.

**Query parameters:**

| Parameter | Type | Default | Range |
|---|---|---|---|
| `limit` | integer | `200` | 1–2000 |
| `level` | string | `ALL` | `ALL`, `ERROR`, `WARN`, `INFO`, `DEBUG` |

Example: `/v1/logs?limit=300&level=WARN`

```json
{
  "api_version": "v1",
  "logs": "...",
  "returned_lines": 300,
  "limit": 300,
  "level": "WARN"
}
```

`level=ALL` is normalized to an empty filter string in the response `level` field.

---

## Method constraints

| Endpoint | Allowed methods |
|---|---|
| `/v1/status` | `GET` |
| `/v1/start` | `POST` |
| `/v1/stop` | `POST` |
| `/v1/validate` | `GET` |
| `/v1/health` | `GET` |
| `/v1/logs` | `GET` |

Wrong method response:

```json
{ "error": "method not allowed" }
```
