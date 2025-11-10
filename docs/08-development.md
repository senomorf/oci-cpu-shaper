# Local Development

This guide covers the tooling expectations and command shortcuts for contributing to OCI CPU Shaper.

## Prerequisites

- Go (latest release).
- `make` for running the provided automation targets.
- [`golangci-lint`](https://golangci-lint.run/) for linting.

Run `make tools` to install or upgrade the pinned `golangci-lint` release with `go install`, and to ensure the repository-standard `gofumpt` binary is available. The helper target keeps local tooling aligned with CI, which currently runs `golangci-lint` v1.64.8 and `gofumpt` v0.6.0. Ensure `$GOBIN` (or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH` so the installed binaries are discoverable.

## Command Reference

The repository includes a `Makefile` that wraps the most common development tasks:

| Command | Purpose |
|---------|---------|
| `make fmt` | Format all Go source files with `gofmt` followed by `gofumpt`. |
| `make tools` | Install pinned developer tooling (e.g., `golangci-lint` v1.64.8, `gofumpt` v0.6.0). |
| `make lint` | Run `golangci-lint` with the configuration in `.golangci.yml`. |
| `make test` | Execute `go test -race ./...` across every package. |
| `make check` | Run linting and race-enabled tests in one step. |
| `make build` | Compile all packages to validate build readiness. |

Running the `test` target enables the Go race detector by default, helping detect data races early during development.

## Suggested Workflow

1. Update code and add or adjust tests.
2. Run `make fmt` to normalize formatting with `gofmt` and `gofumpt`.
3. Execute `make check` to run linting and race-enabled tests together (or `make lint` / `make test` individually).
4. Optionally execute `make build` to confirm the binary compiles successfully.

The lint configuration enables checks such as `staticcheck`, `ineffassign`, `gofumpt`, and `goimports`, ensuring both correctness and import formatting stay consistent with CI expectations. These steps help keep changes consistent and maintainable across the project.

## Container Smoke and SBOM Checks

Every pull request also exercises the container delivery path. The CI workflow builds the image with `docker buildx build` using the `deploy/Dockerfile`, reuses GitHub Actions cache-backed layers for faster rebuilds, and tags the result locally as `oci-cpu-shaper:test`. A dry-run smoke test executes the packaged binary:

```bash
docker run --rm oci-cpu-shaper:test --mode dry-run
```

After the smoke test completes, CI emits an SPDX SBOM (`sbom.spdx.json`) with Anchore's Syft action and scans the image with Anchore's Grype-based scanner, failing the job if any critical vulnerabilities are detected. Developers replicating the pipeline locally should install Docker Buildx, run the command sequence above, and review the generated reports before opening a pull request when container-affecting changes are made.

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
