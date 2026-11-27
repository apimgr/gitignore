# Backend Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never ignore meaningful errors, expose secrets or sensitive report data, bypass security checks, or let optional overlay failures take down the main service.

## CRITICAL — ALWAYS DO

- Validate untrusted input, use bounded resources and safe database operations, redact sensitive logs, and make cleanup and failure paths explicit.

## Key rules summary

- Follow the specified error and cache behavior, database schema and lifecycle, security/logging controls, and Tor/I2P overlay behavior.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 9, PART 10, PART 11, PART 31.
