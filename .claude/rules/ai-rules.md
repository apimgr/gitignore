# AI Assistant Rules (PART 0, 1)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never edit AI.md; it is the read-only implementation specification.
- Never guess product behavior, paths, names, or implementation requirements.
- Never run Go builds, tests, or binaries on the host; use the specified containers and Makefile.
- Never claim completion without checking the result.
- Never add AI attribution, generated-by notices, or co-author trailers.
- Never put AI.md contradictions in project TODO.AI.md; record only AI.md issues in `~/Projects/github/apimgr/TODO.API.md`.

## CRITICAL - ALWAYS DO

- Read the applicable AI.md PART and IDEA.md requirements before implementation.
- Resolve dependencies before sequence order; read cross-referenced sections completely and return to the original section.
- Preserve existing behavior unless the specification explicitly changes it.
- Keep code, documentation, API descriptions, and translations synchronized.
- Work incrementally and verify changes in the prescribed container environment.
- Read each applicable PART completely, follow cross-references and return to the reading point, and surface/fix discovered issues.
- Keep project implementation tasks in TODO.AI.md and update it as work is completed.
- Finish complete implementations; leave no stubs, TODO comments, or deferred work.
- Treat PART-specific checklists and the FINAL checklist as required verification, not optional guidance.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| Which file controls implementation requirements? | AI.md; IDEA.md supplies product behavior. | PART 0, 1 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Project task | An implementation task tracked in project TODO files. |
| AI.md issue | A contradiction or defect in the authoritative specification. |

## KEY RULES

- AI.md is read-only and authoritative; IDEA.md defines product behavior.
- Use TODO.AI.md for project tasks; use `~/Projects/github/apimgr/TODO.API.md` only for AI.md defects.
- Do not treat existing code, audit reports, or task lists as authoritative over AI.md.
- Keep runtime, test, and build artifacts in the prescribed project-scoped temp location; do not alter global/host state to work around project issues.

For complete details, see AI.md PART 0, 1.
