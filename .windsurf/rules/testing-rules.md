---
trigger: model_decision
description: "GitIgnore project requirements for testing rules; consult the referenced AI.md PARTs for full rules."
---

# Testing Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never use the source checkout for runtime/test data, run host-affecting tests on the host, or claim verification without actual output.

## CRITICAL — ALWAYS DO

- Run the prescribed test, lint, browser/layout, docs and i18n checks; document current shipped behavior and keep translation keys/placeholders synchronized.

## Key rules summary

- Run tests in the specified isolation; maintain ReadTheDocs content and the complete locale/accessibility requirements in AI.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 28, PART 29, PART 30.
