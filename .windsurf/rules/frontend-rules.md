---
trigger: model_decision
description: "GitIgnore project requirements for frontend rules; consult the referenced AI.md PARTs for full rules."
---

# Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never add inline styles, executable inline scripts, inline event handlers, uncatalogued styling classes, or JavaScript-only core behavior.

## CRITICAL — ALWAYS DO

- Use semantic HTML, the shared CSS layers and tokens, responsive logical properties, accessible labels/focus states, and the required page/component conformance tests.

## Key rules summary

- Use the shared server-rendered page shell and catalogued components, styles, templates, translations, and accessibility behavior in AI.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 16.
