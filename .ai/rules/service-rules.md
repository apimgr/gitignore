# Service Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never escalate privileges unnecessarily or run service install/management actions on the development host during tests.

## CRITICAL — ALWAYS DO

- Keep service operations explicit and reversible, use supported service managers, and test platform-specific behavior in the required isolated environment.

## Key rules summary

- Privilege escalation, service management, packaging, and supported operating systems follow the exact AI.md lifecycle and platform contracts.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 23, PART 24.
