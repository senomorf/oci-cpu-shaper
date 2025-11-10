# Changelog

## Unreleased

### Added
- Distroless Docker targets, Compose manifests, and runtime scripts for Komodo Mode A (§6).
- Documented bootstrap CLI flags, configuration layout, and diagnostics in §§5 and 9 references.
- GitHub Actions workflows covering `golangci-lint` and race-enabled `go test` runs on pull requests (§14).
- Automated release pipeline publishing multi-architecture images with Syft-generated SPDX SBOM artifacts (§14).
- Unit coverage for IMDS dummy metadata, controller mode wiring, and CLI bootstrap flows via dependency-injected smoke tests (§§5, 9, 11).

### Changed
- CLI argument parsing now validates supported controller modes and normalises flag input before wiring placeholder subsystems.
- Logger construction returns actionable errors for invalid levels while keeping structured output defaults consistent.
- Container build now targets the latest Go toolchain and documentation references the up-to-date requirements.
- CI and release automation now leverage GitHub Actions caching to speed linting, testing, and multi-architecture builds (§14).
- Release SBOM generation is pinned to the latest Anchore Syft GitHub Action for up-to-date SPDX output (§14).
