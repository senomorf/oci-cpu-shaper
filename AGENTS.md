# AGENTS

## Repository scope
- Follow `docs/initial-implementation-plan.md` for architecture; see §§5, 15 before adding modules.
- Build with Go 1.23+ and ship static binaries for linux/amd64,arm64 (§4).
- Keep docs current: update relevant files in `docs/` plus `docs/CHANGELOG.md` when behavior/config changes (§12).
- Run `go test ./... -race` and `golangci-lint run` before submitting PRs (§14); add/adjust tests per §11 when touching logic.
- Prefer small, composable packages and avoid long-running busy loops (see §§3, 5, 10).

### Directory scopes
- `cmd/`, `pkg/`, `docs/` have additional instructions in their own `AGENTS.md` files; obey nested guidance when editing.
