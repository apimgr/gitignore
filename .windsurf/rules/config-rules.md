---
trigger: model_decision
description: "GitIgnore project requirements for configuration rules; consult the referenced AI.md PARTs for full rules."
---

# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never add an undocumented setting, runtime admin UI, or checked-in runtime config. Never silently accept malformed configuration.

## CRITICAL — ALWAYS DO

- Validate config at load boundaries, document supported settings, apply the defined precedence, and preserve safe defaults and mode-specific behavior.

## Key rules summary

- Configuration defaults, modes, validation, precedence, and server settings are specified by AI.md. Settings are file-based and operator-editable.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 5, PART 6, PART 12.
