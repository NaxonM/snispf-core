# SNISPF Core Future Goals

This roadmap captures the practical lessons we can borrow from runtime-first architectures without turning SNISPF into a feature-clone.

## Guiding principles

- Keep core and UI strictly separate.
- Treat the core as a stable runtime platform, not just a proxy executable.
- Prefer compatibility and observability over rapid breaking changes.
- Add complexity only when it improves resilience, performance, or operations.

## What we learned from large runtime projects

- Runtime-first design scales better than UI-first design.
- Configuration compatibility and migration discipline are major long-term advantages.
- Strong routing and policy control unlock real-world flexibility.
- Low-overhead execution matters for small devices and continuous operation.
- A stable control API enables healthy client ecosystem growth.

## Roadmap

### Phase 1: Stability and compatibility hardening

- Introduce explicit config schema versioning (for example `CONFIG_VERSION`).
- Add config migration path with clear deprecation warnings.
- Keep backward compatibility for existing flags and config fields.
- Freeze v1 API field semantics and publish compatibility test fixtures.

### Phase 2: Runtime operations maturity

- Improve graceful shutdown behavior before force-kill fallback.
- Add metrics endpoint (uptime, active sessions, failover counters, error counters).
- Add deterministic exit codes for automation and service managers.
- Add structured log mode optimized for desktop/client parsing.

### Phase 3: Policy and routing evolution

- Add richer endpoint selection policy primitives (latency-aware, weighted, sticky).
- Add domain/IP policy matching for route selection.
- Add fallback chains with explicit failover decision reasons.
- Add dry-run mode to preview route and policy outcomes.

### Phase 4: Release and supply-chain maturity

- Add signed release manifest (for example with cosign/minisign).
- Add reproducibility checks in CI for release artifacts.
- Add SBOM generation and publication for each release.
- Add changelog gates to prevent undocumented breaking changes.

### Phase 5: Ecosystem readiness

- Maintain a stable service API contract for external clients.
- Add API capability discovery endpoint for client compatibility checks.
- Publish official client-integration SDK snippets (PowerShell, C#, Go).
- Add integration test matrix for desktop-client and core protocol lifecycle.

## Non-goals (near term)

- Becoming a general-purpose full protocol platform.
- Adding many transport protocols before core policy and runtime maturity is complete.
- Reintroducing embedded UI logic into the core binary.

## Success criteria

- Core can be upgraded without breaking existing configs and clients.
- Service API remains stable and versioned.
- Release artifacts are verifiable and signed.
- Runtime behavior is measurable and diagnosable in production.
