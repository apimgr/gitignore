# Project Structure Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never commit runtime config, data, logs, cache, binaries, or test output.
- Never change frozen `internal_name` or `internal_org` without an explicit migration.
- Never put product behavior or business requirements in loader files.

## CRITICAL - ALWAYS DO

- Keep Go source under `src/`, assets embedded, and runtime paths derived from project variables and OS conventions.
- Keep IDEA.md in its required layout as the source of product behavior; AI.md remains the implementation source of truth.
- Keep all documentation, server pages, API descriptions, translations, and shipped behavior synchronized.
- Keep licenses and required third-party attributions in LICENSE.md.

## KEY RULES

- Follow the prescribed project layout and OS-specific runtime paths; keep generated/runtime artifacts out of source control.
- IDEA.md describes the product and AI.md specifies implementation; loader files stay concise.

For complete details, see AI.md PART 2, 3, 4.
