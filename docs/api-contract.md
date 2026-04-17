# SNISPF Core Service API Contract

Base URL is configured with `--service-addr` (default `127.0.0.1:8797`).

## Versioning and compatibility policy

- Current API version: `v1`.
- Responses include `api_version` where applicable.
- Within `v1`, additive fields may be introduced without breaking compatibility.
- Removal/rename of existing fields requires a new major API version.
- Clients should ignore unknown JSON fields.

## Auth

When started with `--service-token`, all endpoints require one of:

- Header: `X-SNISPF-Token: <token>`
- Query: `?token=<token>`

Unauthorized response:

```json
{ "error": "unauthorized" }
```

## Endpoints

### GET /v1/status

Returns service and core process status.

Example response:

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

### POST /v1/start

Validates current config and starts core worker.

Success response: same schema as `/v1/status`.

Error examples:

```json
{ "error": "core already running" }
```

```json
{ "error": "config has 1 issue(s); call /v1/validate" }
```

### POST /v1/stop

Stops core worker if running.

Success response: same schema as `/v1/status`.

### GET /v1/validate

Runs config doctor checks.

Example response:

```json
{
  "api_version": "v1",
  "issues": [],
  "warnings": [
    "raw injection unavailable; using fallback behavior"
  ]
}
```

### GET /v1/health

Runs endpoint probe checks on enabled endpoints.

Example response:

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

### GET /v1/logs

Query params:

- `limit` integer, default `200`, min `1`, max `2000`
- `level` one of `ALL`, `ERROR`, `WARN`, `INFO`, `DEBUG`

Example request:

`/v1/logs?limit=300&level=ALL`

Example response:

```json
{
  "api_version": "v1",
  "logs": "...",
  "returned_lines": 300,
  "limit": 300,
  "level": ""
}
```

Note: `level=ALL` is normalized to an empty filter string in the response.

## Method restrictions

- `GET`: `/v1/status`, `/v1/validate`, `/v1/health`, `/v1/logs`
- `POST`: `/v1/start`, `/v1/stop`

Invalid method returns:

```json
{ "error": "method not allowed" }
```