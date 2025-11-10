# Changelog

## Unreleased

### Added
_Note coverage-impacting additions: mention new test suites or tooling that shift the CI ≥30% statement coverage budget (§11)._ 
- Repository-wide AGENTS policy check with `make agents` and CI coverage to enforce scoped instructions (§8.4).
- Token-optimised AGENTS templates and directory-change checklist to keep scoped guidance current (§8.6).
- Distroless Docker targets, Compose manifests, and runtime scripts for Komodo Mode A (§6).
- Documented bootstrap CLI flags, configuration layout, and diagnostics in §§5 and 9 references.
- GitHub Actions workflows covering `golangci-lint` and race-enabled `go test` runs on pull requests (§14).
- Automated release pipeline publishing multi-architecture images with Syft-generated SPDX SBOM artifacts (§14).
- Unit coverage for IMDS dummy metadata, controller mode wiring, and CLI bootstrap flows via dependency-injected smoke tests (§§5, 9, 11).
- Race-enabled `make coverage` target and CI enforcement requiring at least 30% statement coverage before merging (§14).

### Changed
_Record coverage reductions or mitigations so reviewers can audit the CI ≥30% threshold impact (§11)._ 
- CLI argument parsing now validates supported controller modes and normalises flag input before wiring placeholder subsystems.
- Logger construction returns actionable errors for invalid levels while keeping structured output defaults consistent.
- Container build now targets the latest Go toolchain and documentation references the up-to-date requirements.
- CI and release automation now leverage GitHub Actions caching to speed linting, testing, and multi-architecture builds (§14).
- Release SBOM generation is pinned to the latest Anchore Syft GitHub Action for up-to-date SPDX output (§14).
- Local lint tooling is standardised on `golangci-lint` v2.6.1 with pinned installation in CI and the developer Makefile helper, keeping contributor environments aligned (§14).
- `.tool-versions` now pins `golangci-lint` v2.6.1 and `gofumpt` v0.9.2 so `mise`/`asdf` environments surface the same linting behaviour developers see in CI (§14).
