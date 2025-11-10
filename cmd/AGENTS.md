# AGENTS

## Scope: `cmd/`
- `cmd/shaper` should stay a minimal composition layer; keep business logic in `pkg/` per §§5.1, 15.
- Expose configuration via flags/env consistent with §5.2; document any new option in `docs/` per root guidance.
- Maintain ergonomic logging and exit codes; ensure CLI changes include smoke/unit coverage when feasible (§11.1).
- Keep statement coverage at or above the CI threshold (≥85%) and add targeted tests for new flags or workflows before landing.
