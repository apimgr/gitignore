# Service Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never perform host service lifecycle actions while developing or testing.
- Never run the application with unnecessary privileges or weaken privilege boundaries.

## CRITICAL - ALWAYS DO

- Keep privilege escalation explicit, least-privileged, and platform-specific.
- Provide service definitions and lifecycle behavior only for platforms and commands specified by AI.md.
- Bind privileged ports before dropping to the configured service user; keep service files aligned with the supported platform matrix.

## KEY RULES

- Service execution uses least privilege and the lifecycle behavior defined for supported platforms.
- Development and test work never alters host service state.

For complete details, see AI.md PART 23, 24.
