# Service Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never perform host service lifecycle actions while developing or testing.
- Never run the application with unnecessary privileges or weaken privilege boundaries.
- Never use the mutable project name where a service account or unit requires the stable internal name.

## CRITICAL - ALWAYS DO

- Keep privilege escalation explicit, least-privileged, and platform-specific.
- Provide service definitions and lifecycle behavior only for platforms and commands specified by AI.md.
- Bind privileged ports before dropping to the configured service user; keep service files aligned with the supported platform matrix.
- Create required home/config/data/log directories and permissions in the documented order, then drop privileges before application work.
- Support documented init/service platforms and lifecycle commands without performing host lifecycle actions during validation.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| May development checks change host services? | No; service lifecycle checks run in the prescribed isolated environment. | PART 23, 24, 28 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Privilege drop | Starting privileged only for required setup, then running as the configured service user. |

## KEY RULES

- Service execution uses least privilege and the lifecycle behavior defined for supported platforms.
- Development and test work never alters host service state.
- Privileged-port binding and privilege drop follow the startup sequence; service identity stays stable across ordinary project renames.

For complete details, see AI.md PART 23, 24.
