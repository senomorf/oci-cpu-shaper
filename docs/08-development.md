# Local Development

This guide covers the tooling expectations and command shortcuts for contributing to OCI CPU Shaper.

## Prerequisites

- Go (latest release).
- `make` for running the provided automation targets.
- [`golangci-lint`](https://golangci-lint.run/) for linting.

Run `make tools` to install or upgrade the pinned `golangci-lint` release with `go install`. The helper target keeps local tooling aligned with CI, which currently runs v1.64.8. Ensure `$GOBIN` (or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH` so the installed binaries are discoverable.

## Command Reference

The repository includes a `Makefile` that wraps the most common development tasks:

| Command | Purpose |
|---------|---------|
| `make fmt` | Format all Go packages with `go fmt`. |
| `make tools` | Install pinned developer tooling (e.g., `golangci-lint` v1.64.8). |
| `make lint` | Run `golangci-lint` with the configuration in `.golangci.yml`. |
| `make test` | Execute `go test -race ./...` across every package. |
| `make check` | Run linting and race-enabled tests in one step. |
| `make build` | Compile all packages to validate build readiness. |

Running the `test` target enables the Go race detector by default, helping detect data races early during development.

## Suggested Workflow

1. Update code and add or adjust tests.
2. Run `make fmt` to normalize formatting.
3. Execute `make check` to run linting and race-enabled tests together (or `make lint` / `make test` individually).
4. Optionally execute `make build` to confirm the binary compiles successfully.

These steps help keep changes consistent and maintainable across the project.

## §8.4 Scoped AGENTS Policy

Create or update scoped `AGENTS.md` files whenever a directory needs guidance that differs from or expands on the repository root instructions. Keep each file tightly focused on actionable rules for that directory tree, and prefer linking to canonical docs (such as this development guide) instead of duplicating prose. When refactoring or adding new areas of the codebase, audit existing scopes, remove obsolete guidance, and consolidate overlapping notes so the instructions stay concise and discoverable.
