# Changelog

## Unreleased

### Added
_Note coverage-impacting additions: mention new test suites or tooling that shift the CI ≥85% statement coverage budget (§11)._
- CLI emulation suite under `tests/e2e/` gated by the `e2e` build tag, complete with reusable IMDS/Monitoring mocks and `make e2e` helper so offline/online controller flows, metrics output, and structured state-transition logs stay verifiable in CI and locally (§§5, 9, 11).
- Rootful worker pools compiled with `-tags rootful` now request Linux
  `SCHED_IDLE` scheduling for each worker and emit a `worker failed to enter
  sched_idle` warning when the kernel rejects the downgrade (for example,
  missing `CAP_SYS_NICE`/`SYS_NICE`). Dependency-injected unit tests stub
  `unix.SchedSetScheduler` to cover success and EPERM denial paths, preserving
  the §11.1 coverage contract while documenting the new behaviour in §§6 and 9.
- Regression suite `TestControllerCpuUtilisationAcrossOCPUs` covering 1–4 OCPU CpuUtilization streams and the relaxed-interval clamp so the adaptive controller keeps the Always Free reclaim guardrails documented in §§3.1 and 5.2. Tests maintain the ≥85% statement floor by exercising the prolonged high-utilisation path in `pkg/adapt/controller.go` (§11).
- Deterministic 24-hour-equivalent worker-pool load harness (`go test -tags=load ./pkg/shape -run TestPoolLoad24hEquivalent`) that logs CPU/RSS telemetry to `artifacts/load/pool-24h.log` and enforces the §10 budgets alongside nightly/manual CI coverage via `.github/workflows/load.yml` (§§10, 11.4).
- Always Free Terraform stack under `deploy/terraform/self-hosted-runner/` that provisions a hardened GitHub Actions runner with instance-principal access scoped to test compartments, including cloud-init hardening and IAM automation (§§5, 8, 15).
- Scheduled `self-hosted` workflow exercising IMDS lookups, live `QueryP95CPU` calls via `hack/tools/p95query`, and Docker cgroup v2 behaviour on the OCI runner (§§4, 6, 11, 15).
- Runner maintenance and secrets rotation guidance in §15 of `docs/08-development.md`, covering patch cadence, token refresh, and repository variables linked to the new workflow (§§8, 12, 15).
- Dependabot automation covering Go modules, GitHub Actions, and container Dockerfiles with weekly/monthly cadences to keep CI and release dependencies current (§§11, 14).
- Documented the §8.7 issue triage workflow so contributors can acknowledge, classify, and reproduce reports consistently across tooling and coverage expectations (§§8, 11, 12, 15).
- Adaptive controller wiring from `cmd/shaper` to the OCI Monitoring client, estimator sampler, and worker pool, plus layered YAML + environment configuration for controller targets, cadences, worker counts, and HTTP binding (§§3.1, 5.2). Tests cover configuration decoding, environment overrides, and controller factory success/error paths to preserve the ≥85% coverage floor (§11).
- Fast-loop suppression mode that adds a `suppressed` controller state, host-load hysteresis, and configuration knobs (`controller.suppressThreshold`/`controller.suppressResume`, `SHAPER_SUPPRESS_THRESHOLD`/`SHAPER_SUPPRESS_RESUME`) so the estimator can drop the worker pool to zero until the host cools (§§3.1, 5.2). Unit tests now cover suppression entry/exit and estimator error recording while docs in §§4 and 9 describe the new telemetry and structured `controllerState` logging.
- Instance-principal Monitoring client (`pkg/oci`) exposing `QueryP95CPU` with pagination, missing-data fallbacks, and HTTP-backed mocks that keep coverage above the ≥85% floor. Documented in §5 alongside troubleshooting guidance for tenancy policy and metric gaps.
- HTTP-backed IMDSv2 client with retried metadata lookups, shape-config decoding, and an overridable endpoint (`OCI_CPU_SHAPER_IMDS_ENDPOINT`), documented in §2 and backed by `httptest` unit coverage (§§2, 5, 11).
- Repository-wide AGENTS policy check with `make agents` and CI coverage to enforce scoped instructions (§8.4).
- Token-optimised AGENTS templates and directory-change checklist to keep scoped guidance current (§8.6).
- Distroless Docker targets, Compose manifests, and runtime scripts for Komodo Mode A (§6).
- Rootful Mode B Compose manifest, Quadlet unit, and documentation covering host capability requirements (§6).
- Documented bootstrap CLI flags, configuration layout, and diagnostics in §§5 and 9 references.
- Time-bounded shutdown support via the `--shutdown-after` flag so smoke tests and diagnostics can exercise the adaptive controller without leaving background processes behind; docs cover the workflow alongside the offline configuration shipped in the image (§§5, 9).
- GitHub Actions workflows covering `golangci-lint` and race-enabled `go test` runs on pull requests (§14).
- Automated release pipeline publishing multi-architecture images with Syft-generated SPDX SBOM artifacts (§14).
- Unit coverage for IMDS dummy metadata, controller mode wiring, and CLI bootstrap flows via dependency-injected smoke tests (§§5, 9, 11).
- Race-enabled `make coverage` target and CI enforcement requiring at least 85% statement coverage before merging (§14).
- Go vulnerability scanning via `make govulncheck` and a dedicated CI job that restores module/build caches, failing pull requests when published advisories affect the dependency graph (§14).
- CPU weight responsiveness integration suite with CI coverage on `ubuntu-latest` (cgroup v2) that exercises the container build alongside a competing workload and publishes verbose logs (§§6, 11).
- Local `make integration` helper replicating the CI cgroup v2 guard, Docker availability checks, and log capture so contributors can rerun the CPU weight suite with artifact parity (§§6, 11).
- Documentation refresh covering OCI IAM policy setup (§1), Always Free reclaim guardrails (§3), cgroup v2 tuning guidance (§4), and alarm workflows (§7), aligning `docs/` with the implementation plan’s required artifacts (§12).

### Changed
_Record coverage reductions or mitigations so reviewers can audit the CI ≥85% threshold impact (§11)._
- CLI now starts the metrics HTTP server using `http.bind`/`HTTP_ADDR`, shuts it down with the run context, and ships container/Compose updates (`EXPOSE 9108`, `${SHAPER_METRICS_BIND}`) so `/metrics` is reachable when enabled; docs describe the exporter and monitoring workflow alignment (§§6, 9, 11).
- CLI metadata resolution now populates `oci.compartmentId`/`OCI_COMPARTMENT_ID` alongside the new `oci.region`/`OCI_REGION` overrides using IMDS when online, threads the resolved region into the Monitoring client, and logs both identifiers for observability. Fresh unit coverage in §11 exercises the success, fallback, and error paths so the ≥85% statement floor holds.
- IMDS client now injects the required IMDSv2 authorisation header and exposes canonical-region plus compartment OCID lookups, with unit tests and docs refreshed to keep §2 aligned with the metadata surface.
- CLI `--mode` handling now starts the adaptive controller in `dry-run`/`enforce`, keeps `noop` as a diagnostics bypass, and logs configuration failures surfaced by the new YAML/environment loader. Updated docs in §§5 and 9 describe the operating modes and tunable configuration.
- Raised the CI statement coverage floor to 85% and filtered `make coverage` to exclude developer tooling packages (for example, `cmd/agentscheck`), bringing the latest production-only run to 86.6% while keeping the threshold focused on shipped code paths (§11).
- CLI argument parsing now validates supported controller modes and normalises flag input before wiring placeholder subsystems.
- §11 development workflow now mandates shipping changes only after `go test ./... -race` and `golangci-lint run` succeed, reinforcing the all-tests-pass requirement alongside the existing ≥85% coverage guardrail.
- CLI runtime configuration accepts an `oci.instanceId`/`OCI_INSTANCE_ID` override so dry-run and enforce modes can bootstrap when IMDS access is unavailable (e.g., CI smoke tests), with docs refreshed in §§2 and 9.
- CLI runtime configuration now recognises `oci.offline`/`OCI_OFFLINE`, substituting a static metrics client and fallback instance ID so dry-run and enforce bootstrap without IMDS or Monitoring access. Container docs in §§8 and 9 cover the new smoke-test defaults.
- Logger construction returns actionable errors for invalid levels while keeping structured output defaults consistent.
- Container build now targets the latest Go toolchain and documentation references the up-to-date requirements.
- Raised the module `go` directive, `.tool-versions`, and container build ARG to Go 1.24.10 so CI, local workflows, and release images all consume the latest patched toolchain required by `govulncheck` (§14).
- CI and release automation now leverage GitHub Actions caching to speed linting, testing, and multi-architecture builds, including restoring the runner `~/.cache/go-build` directory alongside module downloads (§14).
- Container smoke testing now runs the packaged binary with `--log-level debug --shutdown-after=4s`, verifies the offline metadata log and graceful shutdown message, and uses a tighter offline configuration so CI fails quickly when wiring regresses (§§8, 9, 11).
- Release SBOM generation is pinned to the latest Anchore Syft GitHub Action for up-to-date SPDX output (§14).
- Local lint tooling is standardised on `golangci-lint` v2.6.1 with pinned installation in CI and the developer Makefile helper, keeping contributor environments aligned (§14).
- `make lint`/`make test` now create repository-local caches (`.cache/golangci` and `.cache/go`) and set `GOLANGCI_LINT_CACHE`/`GOCACHE` accordingly so the tools never write to protected runner directories; prefer using the Makefile helpers instead of invoking the linters or `go test` manually to keep sandbox runs stable (§14).
- `.tool-versions` now pins `golangci-lint` v2.6.1 and `gofumpt` v0.9.2 so `mise`/`asdf` environments surface the same linting behaviour developers see in CI (§14).
- `golangci-lint` now runs with depguard allow-listing for module imports and `issues.fix: true`, letting formatters auto-apply fixes while docs instruct contributors to stage the generated edits (§14).
- Overview and Monitoring documentation now link to the IAM, reclaim, cgroup, and alarm guides so operators can navigate the consolidated Always Free playbook (§§0, 5).
- Updated third-party Go modules (flock, gobreaker, testify, golang.org/x/{crypto,net,sys}) to their latest releases so the controller wiring, samplers, and tests stay aligned with upstream fixes (§§11, 14).
- Reconfirmed all Go module requirements and GitHub Actions pins are on the latest stable releases, updating workflow actions to their freshest tags to keep CI and release automation current (§§11, 14).
