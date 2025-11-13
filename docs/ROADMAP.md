# Roadmap

## 3.1 Adaptive control loop
- Implement the fast one-second duty-cycle workers that react to host load and drop activity when contention is detected (§3.1).
- Wire the hourly P95 feedback loop with fallback mode and relaxed cadence handling for sustained healthy readings (§3.1).
- Export controller state via the `/metrics` endpoint so operators can compare local telemetry with OCI Monitoring (§3.2).

## 4.1 CPU control integration
- Validate cgroup v2 `cpu.weight` mappings across Docker, containerd, and Quadlet installs; document any runtime-specific quirks in [`04-cgroups-v2.md`](04-cgroups-v2.md) (§4).
- Provide configuration presets (e.g., Compose snippets) that keep the shaper responsive while sustaining ≥23% P95 CPU (§§4, 6).
- Add automated checks that surface misconfigured weights or ceilings before rollout, such as health endpoints exposing current controller limits (§4).

## 5.2 Adaptive controller wiring
- Wire the default CLI path to the adaptive controller using real OCI Monitoring clients, estimator sampling, and worker pools so `dry-run` and `enforce` execute the same slow-loop logic described in §§3.1 and 5.2 while `noop` remains a diagnostics bypass.
- Document the YAML configuration keys and environment overrides that surface controller targets, cadences, worker counts, and the HTTP bind address so operators can tune deployments without drifting from the baseline thresholds in §5.2.

## 7.1 Alarm operations
- Publish a reusable Terraform or CLI recipe mirroring the manual workflow in [`07-alarms.md`](07-alarms.md) so teams can provision alerts consistently (§7).
- Integrate alarm status with deployment pipelines to block releases when Always Free guardrails are not in place (§7).
- Capture a runbook entry mapping alarm payloads to tuning guidance in [`03-free-tier-reclaim.md`](03-free-tier-reclaim.md) (§7).

## 12.1 Documentation coverage
- Completed: Authored [`01-oci-policy.md`](01-oci-policy.md), [`03-free-tier-reclaim.md`](03-free-tier-reclaim.md), [`04-cgroups-v2.md`](04-cgroups-v2.md), and [`07-alarms.md`](07-alarms.md) to match the implementation plan (§12).
- Completed: Published the CLI deep dive in [`09-cli.md`](09-cli.md) and deployment walkthroughs in [`06-komodo-compose.md`](06-komodo-compose.md), covering configuration layering, Compose/Quadlet manifests, and smoke-test workflows now that the adaptive controller is wired end to end (§§5, 6, 9).
- Completed: Expanded contributor onboarding guidance in [`08-development.md`](08-development.md) with end-to-end QA workflows, coverage guardrails, and integration harness expectations so the ≥85% statement floor stays enforceable (§§8, 11).
- Pending: Track future updates to the adaptive controller and release automation, adding follow-up documentation as new telemetry or operator workflows emerge (§§5, 9, 14).

## 14.1 CI automation
- Run `golangci-lint` with the repository defaults on every pull request to catch regressions early (§14).
- Execute `go test ./... -race` for all targets so concurrency issues surface during review (§14).
- Follow the [§11 Coverage Workflow](08-development.md#11-coverage-workflow) to keep statement coverage at or above the CI ≥85% floor during feature work (§14).
- Block merges on CI completion to keep `main` green.
- Cache Go modules across CI jobs so linting and testing complete quickly while using the latest Go toolchain (§14).
- Build the container image with Docker Buildx on every pull request, reusing cached layers,
  and run a dry-run smoke test via the packaged CLI before merging (§14).
- Generate an SPDX SBOM for each pull-request build and gate merges on vulnerability scanning
  that fails when critical issues appear (§14).

## 14.2 Release pipeline
- Trigger releases from git tags prefixed with `v` to map cleanly to container tags (§14).
- Build multi-architecture images (`linux/amd64`, `linux/arm64`) via `docker buildx` and push them to GHCR for distribution (§4).
- Generate an SPDX SBOM with Syft for each release image and store it alongside build artifacts (§14).
- Reuse Buildx cache layers via GitHub Actions cache to accelerate repeat release builds (§14).
- Pin SBOM generation to the latest Anchore Syft GitHub Action outputting SPDX JSON artifacts (§14).

### Release checklist
1. Cut a `v*` tag from a green `main`.
2. Confirm the Release workflow pushes both versioned and `latest` images to GHCR.
3. Verify the generated SBOM artifact and attach it to any manual release notes.
4. Announce availability and update downstream deployment references if necessary.
5. Audit `docs/` (including `docs/CHANGELOG.md`) for drift introduced since the previous release and capture any required updates in the release notes.
