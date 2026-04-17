# Example Configurations

Bundled sample profiles live in `configs/examples/` and are sanitized for publication.

All example values use placeholders:

- IPs in `203.0.113.x`
- SNI domains in `*.example.com`

Before deployment:

1. Replace placeholder IPs with reachable upstream endpoints.
2. Replace placeholder SNI values with valid hostnames.
3. Set `LOG_LEVEL` to your preferred verbosity (`error|warn|info|debug`).
4. Re-run config validation.

```bash
go run ./cmd/snispf --config ./config.json --config-doctor
```

## Profiles

- `fragment-baseline.json`: safest starter profile.
- `combined-aggressive.json`: fragment + fake_sni strategy with multiple endpoints.
- `failover-multi-endpoint.json`: failover behavior with endpoint probing.
- `wrong-seq-strict.json`: strict raw-confirmed mode (`wrong_seq`).
