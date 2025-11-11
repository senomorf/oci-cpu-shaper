# AGENTS

## Scope: `pkg/`
- Honor package seams from plan §§5.1 & 15; keep exported APIs tight with GoDoc comments.
- Follow §§3, 5, 9 for retries, fallbacks, and controllers; pair logic edits with focused unit tests (§11.1).
- Maintain ≥85% coverage via `make coverage`; expand suites before merging when new paths appear.
- Avoid busy-waiting—workers must respect the §10 duty-cycle budget.
