# AGENTS

## Scope: `pkg/`
- Respect package boundaries outlined in §§5.1, 15; keep exported APIs small and document them with GoDoc comments.
- Implement retries, fallbacks, and controller logic exactly as described in §§3, 5, 9; add targeted unit tests per §11.1.
- Avoid busy-waiting: duty-cycle workers should honor the performance budget in §10.
