# Local Development

This guide covers the tooling expectations and command shortcuts for contributing to OCI CPU Shaper.

## Prerequisites

- Go (latest release).
- `make` for running the provided automation targets.
- [`golangci-lint`](https://golangci-lint.run/) for linting.

Install `golangci-lint` with the pinned module version so `go` manages the binary for you:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
```

Ensure `$GOBIN` (or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH` so the `golangci-lint` executable is discoverable. Re-run the command whenever the project bumps the pinned version.

## Command Reference

The repository includes a `Makefile` that wraps the most common development tasks:

| Command | Purpose |
|---------|---------|
| `make fmt` | Format all Go packages with `go fmt`. |
| `make lint` | Run `golangci-lint` with the configuration in `.golangci.yml`. |
| `make test` | Execute `go test -race ./...` across every package. |
| `make build` | Compile all packages to validate build readiness. |

Running the `test` target enables the Go race detector by default, helping detect data races early during development.

## Suggested Workflow

1. Update code and add or adjust tests.
2. Run `make fmt` to normalize formatting.
3. Execute `make lint` to catch style and static-analysis issues.
4. Run `make test` before opening a pull request.
5. Optionally execute `make build` to confirm the binary compiles successfully.

These steps help keep changes consistent and maintainable across the project.
