# Changelog

## Unreleased

### Added
- Distroless Docker targets, Compose manifests, and runtime scripts for Komodo Mode A (§6).
- Documented bootstrap CLI flags, configuration layout, and diagnostics in §§5 and 9 references.

### Changed
- CLI argument parsing now validates supported controller modes and normalises flag input before wiring placeholder subsystems.
- Logger construction returns actionable errors for invalid levels while keeping structured output defaults consistent.
