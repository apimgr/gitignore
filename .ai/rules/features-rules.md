# Feature Rules (PART 17-22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never make optional integrations mandatory at startup or expose protected operational data publicly.

## CRITICAL — ALWAYS DO

- Honor configured enablement, validate inputs and retention limits, report feature health, and preserve the specified failure and recovery behavior.

## Key rules summary

- Email/notifications, scheduler, GeoIP, metrics, backup/restore, and update behavior must match their dedicated AI.md contracts.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 17-22.
