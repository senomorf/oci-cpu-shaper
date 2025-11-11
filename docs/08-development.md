# Local Development

This guide covers the tooling expectations and command shortcuts for contributing to OCI CPU Shaper.

## Prerequisites

- Go 1.24.x (currently 1.24.10).
- `make` for running the provided automation targets.
- [`golangci-lint`](https://golangci-lint.run/) for linting.

Run `make tools` to install or upgrade the pinned `golangci-lint` release with `go install`, and to ensure the repository-standard `gofumpt` binary is available. The helper target keeps local tooling aligned with CI, which currently runs `golangci-lint` v2.6.1 and `gofumpt` v0.9.2. Ensure `$GOBIN` (or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH` so the installed binaries are discoverable. Developers using `mise`/`asdf` can achieve the same alignment by running `mise install`, because `.tool-versions` now pins Go 1.24.10 alongside the same `golangci-lint` and `gofumpt` versions referenced in §14.

## Command Reference

The repository includes a `Makefile` that wraps the most common development tasks:

| Command | Purpose |
|---------|---------|
| `make fmt` | Format all Go source files with `gofmt` followed by `gofumpt`. |
| `make tools` | Install pinned developer tooling (e.g., `golangci-lint` v2.6.1, `gofumpt` v0.9.2, Go 1.24.10 via `mise`/`asdf`). |
| `make lint` | Run `golangci-lint` with the configuration in `.golangci.yml` (auto-fixes formatting and lint findings where supported). This target also sets `GOLANGCI_LINT_CACHE` to `.cache/golangci`, so prefer `make lint` over calling `golangci-lint run` directly when working inside restricted sandboxes. |
| `make test` | Execute `go test -race ./...` across every package. |
| `make check` | Run linting and race-enabled tests in one step. |
| `make coverage` | Generate a race-enabled coverage profile for production packages, save `coverage.out`/`coverage.txt`, and print the total percentage (CI enforces ≥85%). |
| `make integration` | Ensure Docker is reachable, validate cgroup v2, and run the CPU weight responsiveness suite while teeing logs to `artifacts/integration.log` for post-run inspection (§§6, 11). |
| `make govulncheck` | Scan the module and all packages with `golang.org/x/vuln/cmd/govulncheck@v1.1.4`, failing on known Go vulnerabilities before changes ship (§14). |
| `make build` | Compile all packages to validate build readiness. |

## Local caches

The Makefile defines `GOCACHE_DIR` (`.cache/go`) and `GOLANGCI_LINT_CACHE_DIR` (`.cache/golangci`) relative to the repository root and injects them into the `make test` and `make lint` targets. These locally scoped caches keep Go build artifacts and linter facts inside the workspace so commands do not hit the runner’s global `~/.cache` tree, which may be read-only in restricted sandboxes. `.gitignore` excludes the `.cache/` directory, so the caches survive between runs without leaking into commits. You can run `go env -u GOCACHE` if you temporarily need to fall back to the default global cache.

### §14 Lint Auto-Fix Workflow

- `.golangci.yml` enables `issues.fix: true` and formatter integration (`gofmt`, `gofumpt`, `gci`, `golines`), so each `make lint` or `golangci-lint run` invocation rewrites files automatically when a supported diagnostic can be corrected.
- When running the linter manually, prefer `golangci-lint run --fix` to mirror CI and the Makefile helper; re-run until the command exits cleanly.
- Inspect `git status` after linting and stage the generated edits before committing so reviewers see both the intentional changes and any formatter updates together (§14).
- Auto-fixes often adjust imports and line wrapping; rerunning `make lint` after resolving merge conflicts keeps the workspace aligned with CI and avoids last-minute surprises.

Running the `test` target enables the Go race detector by default, helping detect data races early during development. Use `make coverage` before pushing to confirm your changes keep statement coverage at or above the CI threshold; the command writes `coverage.out`, mirrors the console summary into `coverage.txt`, and reports the aggregate percentage across production code by skipping developer tooling packages such as `cmd/agentscheck`. CI currently requires at least 85 percent statement coverage, and the latest filtered run reports 86.6 percent. Override `COVERAGE_EXCLUDES` when invoking the target if you introduce additional non-production packages that should be omitted from the calculation. The `test` job in `.github/workflows/ci.yml` runs the same make target with the `MIN_COVERAGE` guard and publishes both coverage files as build artifacts so reviewers can audit the report without re-running the suite locally.

## Suggested Workflow

1. Update code and add or adjust tests.
2. Run `make fmt` to normalize formatting with `gofmt` and `gofumpt`.
3. Execute `make check` to run linting and race-enabled tests together (or `make lint` / `make test` individually); because linting applies auto-fixes, review `git status` afterward and stage the generated edits.
4. Re-run `go test ./... -race` and `make lint` (or `make check`) until they pass—features and fixes must never ship while any test or lint job is failing (§11).
5. Run `make govulncheck` to confirm the dependency graph and local packages are free of published vulnerabilities before opening a pull request (§14).
6. Optionally execute `make build` to confirm the binary compiles successfully.

The lint configuration enables checks such as `staticcheck`, `ineffassign`, `gofumpt`, and `goimports`, ensuring both correctness and import formatting stay consistent with CI expectations. These steps help keep changes consistent and maintainable across the project.

## Container Smoke and SBOM Checks

Every pull request also exercises the container delivery path. The CI workflow builds the image with `docker buildx build` using the `deploy/Dockerfile`, reuses GitHub Actions cache-backed layers for faster rebuilds, and tags the result locally as `oci-cpu-shaper:test`. A dry-run smoke test executes the packaged binary:

```bash
docker run --rm oci-cpu-shaper:test --mode dry-run --log-level debug --shutdown-after=4s
```

After the smoke test completes, CI emits an SPDX SBOM (`sbom.spdx.json`) with Anchore's Syft action and scans the image with Anchore's Grype-based scanner, failing the job if any critical vulnerabilities are detected. Developers replicating the pipeline locally should install Docker Buildx, run the command sequence above, and review the generated reports before opening a pull request when container-affecting changes are made. The container image now ships `/etc/oci-cpu-shaper/config.yaml` with `oci.offline: true`, letting the packaged binary wire the adaptive controller using the static metrics client and fallback instance ID described in §9.2 so smoke tests succeed without IMDS or Monitoring credentials; unset `OCI_OFFLINE` or override the config when targeting real tenancy environments.

Rootful experiments using `deploy/compose/mode-b.rootful.yaml` or the matching Quadlet unit should run on hosts where Docker/Podman can grant UID 0 and `SYS_NICE`. The Compose manifest defaults to `network_mode: host`; switch `SHAPER_NETWORK_MODE` to an isolated network when testing on shared lab hardware, and avoid running it under rootless Docker because cgroup weight, `cpus`, and capability settings will be ignored (§6.2).

## §11.2 CPU Weight Integration Suite

End-to-end responsiveness tests live under `tests/integration/` and run with the `integration` build tag. They build the rootful container image, compile a static CPU hog helper, and launch the image alongside an `alpine` competitor constrained to the same CPU. The harness measures each container's `cpu.weight` and `cpu.stat` usage to assert the heavier workload receives at least five times the CPU time, ensuring the runtime honours the responsiveness guarantees described in §§5, 9, and 11.

Run the suite on a Linux host with Docker or Podman configured for cgroup v2 (verify with `docker info --format '{{.CgroupVersion}}'` or by checking `/sys/fs/cgroup/cgroup.controllers` for the `cpu` entry). Because the harness builds and runs containers locally, execute it from the repository root with elevated privileges when necessary. The `make integration` helper mirrors the CI workflow: it refuses to run unless Docker is reachable, enforces cgroup v2, and tees verbose output to `artifacts/integration.log`, removing the log directory on success while preserving it after failures for debugging (§§6, 11). Developers who need finer control can still invoke `go test -tags=integration -v ./tests/integration/...`, but the Makefile target should be preferred so local runs collect the same diagnostics as CI. When iterating locally, rerun the suite after modifying container entrypoints, CPU-tuning flags, or workload scripts to preserve the CI-required ≥85% coverage baseline while keeping responsiveness guardrails intact.

## §11 Coverage Workflow

Follow this loop to keep the repository above the CI-required 85 percent statement coverage floor (§14):

1. Write or update unit, integration, or smoke tests alongside code changes so new logic is executable under `go test` (§§5, 9, 11).
2. Run `make coverage` to generate `coverage.out` and `coverage.txt`, review the reported percentage, and inspect uncovered packages with `go tool cover -func coverage.out` when the value trends downward.
3. Patch gaps immediately—prefer focused tests that exercise failure paths and concurrency edges instead of broad golden snapshots.
4. Capture any notable coverage shifts (both increases and decreases) in `docs/CHANGELOG.md` so reviewers can audit the impact alongside functional notes (§12).

Committers should only merge when coverage meets or exceeds the automated threshold and the new tests clearly document the intended behaviour. When a change legitimately lowers coverage (for example, introducing defensive code that is hard to trigger), document the rationale in the changelog and open a follow-up issue to backfill tests.

## §11.1 Integration Test Expectations

Integration tests complement unit coverage by validating interactions between packages and external systems:

- Prefer table-driven tests using the public APIs wired through `cmd/shaper` so CLI flows remain measurable (§5.2).
- Use the existing dummy IMDS server and controller harnesses to exercise multi-component workflows; extend them instead of building bespoke fixtures (§§5, 9).
- Gate new features on end-to-end assertions that demonstrate the behaviour across controller states, rate limiting, and failure handling. When integration coverage is impractical, describe the manual verification steps in the pull request and track automation debt in an issue.
- Keep integration suites fast—tests should reuse shared setup helpers and run within CI timeouts while still contributing to the overall coverage budget.

Document meaningful integration suites and their expected coverage deltas in the changelog so downstream operators understand the verification story.

## Optional Git Hooks

To run formatting and linting automatically before pushing, opt in to the provided Git hook template:

```bash
git config core.hooksPath .githooks
```

The `.githooks/pre-push` script executes `make fmt` and `make lint`, aborting the push if formatting changes are required or linting fails. Remove or customize the hook as needed for your workflow.

## §8.4 Scoped AGENTS Policy

Create or update scoped `AGENTS.md` files whenever a directory needs guidance that differs from or expands on the repository root instructions. Keep each file tightly focused on actionable rules for that directory tree, and prefer linking to canonical docs (such as this development guide) instead of duplicating prose. When refactoring or adding new areas of the codebase, audit existing scopes, remove obsolete guidance, and consolidate overlapping notes so the instructions stay concise and discoverable. Run `make agents` before submitting changes to confirm every Go package directory inherits the appropriate guidance and that scope headers match the directory layout.

## §8.5 Directory Change Checklist

Any change that creates, renames, or deletes a directory with Go code **must** review AGENTS coverage:

1. Identify which existing `AGENTS.md` file governs the directory tree.
2. Update or add scoped instructions that reflect the new layout and remove references to paths that no longer exist.
3. Re-run `make agents` to ensure the repository-wide policy check passes and capture any stale references that need pruning.

For broader refactors, scan neighbouring scopes as well—moving files often requires adjusting multiple `AGENTS.md` files so contributors never encounter contradictory guidance.

## §8.6 Token-Optimised AGENTS Templates

Keep `AGENTS.md` content short and directive so nested scopes are cheap to load. Start from these templates when seeding new subtrees:

- **Go package subtree**

  ```markdown
  # AGENTS

  ## Scope: `path/to/pkg/`
  - One line per enforced rule; reference §§ from the development plan instead of repeating rationale.
  - Note any testing or tooling expectations unique to the subtree.
  ```

- **Documentation subtree**

  ```markdown
  # AGENTS

  ## Scope: `docs/new-area/`
  - Tie headings back to the numbered sections in §8.
  - Link to canonical references instead of duplicating configuration snippets.
  ```

- **Aggregating scopes**

  ```markdown
  # AGENTS

  ## Scope: `services/`
  - List the child directories that have their own `AGENTS.md` files and the key behaviour that differs from the parent scope.
  - Remind contributors which shared policies from the root still apply.
  ```

Adapt the bullet points to the smallest set of actionable, testable rules; long narrative guidance should live in the numbered docs and be linked from the AGENTS files instead of copied verbatim.
