# Changelog

All notable changes to this project are documented in this file.

## [v0.1.3] - 2026-04-18

### Added
- Config precedence warnings at startup when ENDPOINTS[0] conflicts with top-level CONNECT_IP, CONNECT_PORT, or FAKE_SNI.

### Changed
- Improved strict wrong_seq reliability with clearer connection-drop warnings and stronger failover attempt handling.
- Enhanced Linux raw injector route awareness and send-path stability after interface/route changes.
- Improved OpenWrt operational recovery with route-aware source-IP dialing and faster watchdog restart behavior.
- OpenWrt installer flow now supports delayed post-install restart and guided watchdog installation.

### Fixed
- wrong_seq runtime diagnostics now surface more failure modes in logs.
- OpenWrt script examples now consistently use ash invocation style.
- CLI --info behavior is now clearly documented as config-independent.

### Documentation
- Synced docs with runtime defaults and behavior:
  - ENDPOINTS precedence model and warnings.
  - round_robin default load-balancing.
  - AUTO_FAILOVER default behavior.
  - OpenWrt watchdog guidance and strict wrong_seq multi-WAN notes.

## [v0.1.2] - 2026-04-17

See Git history and GitHub release notes for details.
