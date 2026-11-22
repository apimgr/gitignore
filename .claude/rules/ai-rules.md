# AI Assistant Rules (PART 0, 1)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never edit AI.md; it is the read-only implementation specification.
- Never guess product behavior, paths, names, or implementation requirements.
- Never run Go builds, tests, or binaries on the host; use the specified containers and Makefile.
- Never claim completion without checking the result.
- Never add AI attribution, generated-by notices, or co-author trailers.
- Never put AI.md contradictions in project TODO.AI.md; record only AI.md issues in `~/Projects/github/apimgr/TODO.API.md`.

## CRITICAL - ALWAYS DO

- Read the applicable AI.md PART and IDEA.md requirements before implementation.
- Preserve existing behavior unless the specification explicitly changes it.
- Keep code, documentation, API descriptions, and translations synchronized.
- Work incrementally and verify changes in the prescribed container environment.
- Read each applicable PART completely, follow cross-references and return to the reading point, and surface/fix discovered issues.
- Keep project implementation tasks in TODO.AI.md and update it as work is completed.
- Finish complete implementations; leave no stubs, TODO comments, or deferred work.

## KEY RULES

- AI.md is read-only and authoritative; IDEA.md defines product behavior.
- Use TODO.AI.md for project tasks; use `~/Projects/github/apimgr/TODO.API.md` only for AI.md defects.

For complete details, see AI.md PART 0, 1.
