# Roadmap

Planned direction for SNISPF Core. This captures practical lessons from runtime-first architectures applied to SNISPF's specific scope — not a feature wishlist.

---

## Guiding principles

- Keep core and UI strictly separate
- Treat the core as a stable runtime platform, not just a proxy binary
- Prefer compatibility and observability over rapid breaking changes
- Add complexity only when it improves resilience, performance, or operations

---

## Phases

### Phase 1 — Stability and compatibility hardening

- Introduce explicit config schema versioning (`CONFIG_VERSION` field)
- Add config migration path with clear deprecation warnings
- Keep backward compatibility for all existing flags and config fields
- Freeze v1 API field semantics and publish compatibility test fixtures

### Phase 2 — Runtime operations maturity

- Improve graceful shutdown behavior before force-kill fallback
- Add metrics endpoint (uptime, active sessions, failover counters, error counters)
- Add deterministic exit codes for automation and service managers
- Add structured log mode optimized for desktop and client parsing

### Phase 3 — Policy and routing evolution

- Add richer endpoint selection policy primitives (latency-aware, weighted, sticky)
- Add domain/IP policy matching for route selection
- Add fallback chains with explicit per-decision failover reasons
- Add dry-run mode to preview route and policy outcomes

### Phase 4 — Release and supply-chain maturity

- Sign release manifests (cosign or minisign)
- Add reproducibility checks in CI for release artifacts
- Add SBOM generation and publication per release
- Add changelog gates to prevent undocumented breaking changes

### Phase 5 — Ecosystem readiness

- Maintain a stable service API contract for external clients
- Add API capability discovery endpoint for client compatibility checks
- Publish official client-integration SDK snippets (PowerShell, C#, Go)
- Add integration test matrix for desktop-client and core protocol lifecycle

---

## Non-goals (near term)

- Becoming a general-purpose full protocol platform
- Adding transport protocols before core policy and runtime maturity is complete
- Reintroducing embedded UI logic into the core binary

---

## Success criteria

- Core upgrades without breaking existing configs or clients
- Service API remains stable and versioned
- Release artifacts are verifiable and signed
- Runtime behavior is measurable and diagnosable in production
