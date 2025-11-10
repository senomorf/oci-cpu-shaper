# AGENTS

## Scope: `pkg/`
- Respect package boundaries outlined in §§5.1, 15; keep exported APIs small and document them with GoDoc comments.
- Implement retries, fallbacks, and controller logic exactly as described in §§3, 5, 9; add targeted unit tests per §11.1.
- Preserve or raise coverage relative to the CI floor (≥85%, measured with `make coverage`) by exercising new code paths with focused tests before merging.
- Avoid busy-waiting: duty-cycle workers should honor the performance budget in §10.
