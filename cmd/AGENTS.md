# AGENTS

## Scope: `cmd/`
- `cmd/shaper` stays a thin wiring layer; keep logic in `pkg/` (plan §§5.1, 15).
- Flags/env must mirror §5.2; document new options in `docs/` as part of each change.
- Preserve friendly logging/exit codes and pair CLI tweaks with smoke/unit coverage (§11.1).
- Hold coverage ≥95% (`make coverage`) and add tests for every new flag path before merging.
