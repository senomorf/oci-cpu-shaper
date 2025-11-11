# Changelog

## Unreleased

### Added
_Note coverage-impacting additions: mention new test suites or tooling that shift the CI ≥85% statement coverage budget (§11)._ 
- Adaptive controller wiring from `cmd/shaper` to the OCI Monitoring client, estimator sampler, and worker pool, plus layered YAML + environment configuration for controller targets, cadences, worker counts, and HTTP binding (§§3.1, 5.2). Tests cover configuration decoding, environment overrides, and controller factory success/error paths to preserve the ≥85% coverage floor (§11).
- Instance-principal Monitoring client (`pkg/oci`) exposing `QueryP95CPU` with pagination, missing-data fallbacks, and HTTP-backed mocks that keep coverage above the ≥85% floor. Documented in §5 alongside troubleshooting guidance for tenancy policy and metric gaps.
- HTTP-backed IMDSv2 client with retried metadata lookups, shape-config decoding, and an overridable endpoint (`OCI_CPU_SHAPER_IMDS_ENDPOINT`), documented in §2 and backed by `httptest` unit coverage (§§2, 5, 11).
- Repository-wide AGENTS policy check with `make agents` and CI coverage to enforce scoped instructions (§8.4).
- Token-optimised AGENTS templates and directory-change checklist to keep scoped guidance current (§8.6).
- Distroless Docker targets, Compose manifests, and runtime scripts for Komodo Mode A (§6).
- Documented bootstrap CLI flags, configuration layout, and diagnostics in §§5 and 9 references.
- GitHub Actions workflows covering `golangci-lint` and race-enabled `go test` runs on pull requests (§14).
- Automated release pipeline publishing multi-architecture images with Syft-generated SPDX SBOM artifacts (§14).
- Unit coverage for IMDS dummy metadata, controller mode wiring, and CLI bootstrap flows via dependency-injected smoke tests (§§5, 9, 11).
- Race-enabled `make coverage` target and CI enforcement requiring at least 85% statement coverage before merging (§14).
- CPU weight responsiveness integration suite with CI coverage on `ubuntu-latest` (cgroup v2) that exercises the container build alongside a competing workload and publishes verbose logs (§§6, 11).

### Changed
_Record coverage reductions or mitigations so reviewers can audit the CI ≥85% threshold impact (§11)._ 
- CLI `--mode` handling now starts the adaptive controller in `dry-run`/`enforce`, keeps `noop` as a diagnostics bypass, and logs configuration failures surfaced by the new YAML/environment loader. Updated docs in §§5 and 9 describe the operating modes and tunable configuration.
- Raised the CI statement coverage floor to 85% and filtered `make coverage` to exclude developer tooling packages (for example, `cmd/agentscheck`), bringing the latest production-only run to 86.6% while keeping the threshold focused on shipped code paths (§11).
- CLI argument parsing now validates supported controller modes and normalises flag input before wiring placeholder subsystems.
- Logger construction returns actionable errors for invalid levels while keeping structured output defaults consistent.
- Container build now targets the latest Go toolchain and documentation references the up-to-date requirements.
- CI and release automation now leverage GitHub Actions caching to speed linting, testing, and multi-architecture builds (§14).
- Release SBOM generation is pinned to the latest Anchore Syft GitHub Action for up-to-date SPDX output (§14).
- Local lint tooling is standardised on `golangci-lint` v2.6.1 with pinned installation in CI and the developer Makefile helper, keeping contributor environments aligned (§14).
- `.tool-versions` now pins `golangci-lint` v2.6.1 and `gofumpt` v0.9.2 so `mise`/`asdf` environments surface the same linting behaviour developers see in CI (§14).
- `golangci-lint` now runs with depguard allow-listing for module imports and `issues.fix: true`, letting formatters auto-apply fixes while docs instruct contributors to stage the generated edits (§14).
- Updated third-party Go modules (flock, gobreaker, testify, golang.org/x/{crypto,net,sys}) to their latest releases so the controller wiring, samplers, and tests stay aligned with upstream fixes (§§11, 14).
- Reconfirmed all Go module requirements and GitHub Actions pins are on the latest stable releases, updating workflow actions to their freshest tags to keep CI and release automation current (§§11, 14).
