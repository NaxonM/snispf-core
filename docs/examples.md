# Example configurations

Bundled sample profiles live in `configs/examples/`. All IPs and SNI values are sanitized placeholders — replace them before use.

| Placeholder | Replace with |
|---|---|
| `203.0.113.x` | Your actual upstream IP |
| `*.example.com` | Your actual upstream hostname |

After editing any profile, validate it:

```bash
.\snispf.exe --config ./your-config.json --config-doctor
```

---

## Profiles

### `wrong-seq-strict.json` ★ recommended default

Strict raw-confirmed mode. The most effective bypass strategy when platform prerequisites are met.

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

**Requirements:**
- Exactly one enabled endpoint (`AUTO_FAILOVER: false`, `FAILOVER_RETRIES: 0` enforce this)
- Windows: **Administrator terminal** + `WinDivert.dll` + `WinDivert64.sys` alongside the binary
- Linux: `root` or `sudo setcap cap_net_raw+ep ./snispf`
- SNI ≤ 219 bytes; fake ClientHello ≤ 1460 bytes (both checked by `--config-doctor`)

**When to use:** Default for all platforms that meet the prerequisites above.

---

### `combined-aggressive.json`

Combines fake SNI prelude with fragmentation. Uses TTL trick and random load balancing across two endpoints. Falls back gracefully when raw injection is unavailable.

```json
{
  "LISTEN_HOST": "0.0.0.0",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "203.0.113.10",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "edge-a.example.com",
  "BYPASS_METHOD": "combined",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.05,
  "USE_TTL_TRICK": true,
  "FAKE_SNI_METHOD": "prefix_fake",
  "ENDPOINTS": [
    {
      "NAME": "primary",
      "IP": "203.0.113.10",
      "PORT": 443,
      "SNI": "edge-a.example.com",
      "ENABLED": true
    },
    {
      "NAME": "backup",
      "IP": "203.0.113.11",
      "PORT": 443,
      "SNI": "edge-b.example.com",
      "ENABLED": true
    }
  ],
  "LOAD_BALANCE": "random",
  "ENDPOINT_PROBE": true,
  "AUTO_FAILOVER": true,
  "FAILOVER_RETRIES": 4,
  "PROBE_TIMEOUT_MS": 2000
}
```

**When to use:** `wrong_seq` prerequisites can't be met, but aggressive bypass with multiple endpoints is still needed. Note `LISTEN_HOST: "0.0.0.0"` — this listens on all interfaces, not just localhost.

---

### `failover-multi-endpoint.json`

Fragment strategy with strict failover ordering and startup endpoint probing. Unreachable endpoints are removed before the first connection attempt.

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "203.0.113.10",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "edge-a.example.com",
  "BYPASS_METHOD": "fragment",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.1,
  "USE_TTL_TRICK": false,
  "FAKE_SNI_METHOD": "prefix_fake",
  "ENDPOINTS": [
    {
      "NAME": "edge-a",
      "IP": "203.0.113.10",
      "PORT": 443,
      "SNI": "edge-a.example.com",
      "ENABLED": true
    },
    {
      "NAME": "edge-b",
      "IP": "203.0.113.11",
      "PORT": 443,
      "SNI": "edge-b.example.com",
      "ENABLED": true
    },
    {
      "NAME": "edge-c-disabled",
      "IP": "203.0.113.12",
      "PORT": 443,
      "SNI": "edge-c.example.com",
      "ENABLED": false
    }
  ],
  "LOAD_BALANCE": "failover",
  "ENDPOINT_PROBE": true,
  "AUTO_FAILOVER": true,
  "FAILOVER_RETRIES": 3,
  "PROBE_TIMEOUT_MS": 2500
}
```

**When to use:** Production deployments where endpoint availability may vary and you want deterministic primary/fallback ordering. The disabled `edge-c` entry shows how to stage a third endpoint without activating it.

---

### `fragment-baseline.json`

Safest starting point. No raw injection, no TTL trick. Works unprivileged on every platform.

```json
{
  "LISTEN_HOST": "0.0.0.0",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "203.0.113.10",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "edge-a.example.com",
  "BYPASS_METHOD": "fragment",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.1,
  "USE_TTL_TRICK": false,
  "FAKE_SNI_METHOD": "prefix_fake",
  "ENDPOINTS": [
    {
      "NAME": "primary",
      "IP": "203.0.113.10",
      "PORT": 443,
      "SNI": "edge-a.example.com",
      "ENABLED": true
    }
  ],
  "LOAD_BALANCE": "round_robin",
  "ENDPOINT_PROBE": true,
  "AUTO_FAILOVER": true,
  "FAILOVER_RETRIES": 2,
  "PROBE_TIMEOUT_MS": 2500
}
```

**When to use:** Diagnosis, constrained environments, or any platform where raw injection is unavailable. Note `LISTEN_HOST: "0.0.0.0"` — listens on all interfaces.

---

## Multi-listener example

Not a standalone profile — add `LISTENERS` to any base config when you need multiple local entry points in a single process. Per-listener `BYPASS_METHOD` overrides the top-level value; listeners without one inherit the top-level default.

```json
{
  "BYPASS_METHOD": "wrong_seq",
  "LISTENERS": [
    {
      "NAME": "edge-a",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40443,
      "CONNECT_IP": "203.0.113.10",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "edge-a.example.com"
    },
    {
      "NAME": "edge-b",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40444,
      "CONNECT_IP": "203.0.113.11",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "edge-b.example.com",
      "BYPASS_METHOD": "fragment"
    }
  ]
}
```
