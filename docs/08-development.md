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

## Optional Git Hooks

To run formatting and linting automatically before pushing, opt in to the provided Git hook template:

```bash
git config core.hooksPath .githooks
```

The `.githooks/pre-push` script executes `make fmt` and `make lint`, aborting the push if formatting changes are required or linting fails. Remove or customize the hook as needed for your workflow.

## §8.4 Scoped AGENTS Policy

Create or update scoped `AGENTS.md` files whenever a directory needs guidance that differs from or expands on the repository root instructions. Keep each file tightly focused on actionable rules for that directory tree, and prefer linking to canonical docs (such as this development guide) instead of duplicating prose. When refactoring or adding new areas of the codebase, audit existing scopes, remove obsolete guidance, and consolidate overlapping notes so the instructions stay concise and discoverable.
