# AGENTS

## Repository scope
- Architecture: follow `docs/initial-implementation-plan.md` (check §§5 & 15 before adding modules) and keep packages lean.
- Toolchain: Go 1.24.x, static linux/amd64+arm64 binaries (§4).
- QA: add/refresh tests with every logic change, then run `make test` and `make lint` until clean (§§11, 14); these helpers already seed `GOCACHE`/`GOLANGCI_LINT_CACHE` under `.cache` so avoid calling `go test` or `golangci-lint run` directly in constrained sandboxes.
- Workflows: prefer `make lint` instead of running `golangci-lint` manually so the helper sets `GOLANGCI_LINT_CACHE` in `.cache/golangci`; `make test` already sets `GOCACHE` to `.cache/go` so avoid invoking `go test`/`golangci-lint` directly from restricted sandboxes (the caches are ignored via `.gitignore`).
- Linting: rely on `make lint`, which honors the `.golangci.yml` `issues.fix: true` setting so fixable findings are auto-applied before results are reported; rerun after edits to verify no residual warnings remain.
- Coverage: keep statement coverage ≥85% via `make coverage MIN_COVERAGE=85`; extend suites when new paths appear.
- Docs: sync `docs/` (including `docs/CHANGELOG.md`) plus any impacted READMEs/config samples when behavior or config shifts (§12).
- Guidance hygiene: keep nested `AGENTS.md` files accurate; revise or prune stale rules as code moves (§8.4 of `docs/08-development.md`).
- Performance: avoid busy loops; respect duty-cycle budgets from §§3 & 10.
- Reviews: confirm instructions in every affected scope still apply and update them when they drift.

### Directory scopes
- `cmd/`, `pkg/`, `docs/` carry extra rules in local `AGENTS.md` files; nested guidance wins when editing.
