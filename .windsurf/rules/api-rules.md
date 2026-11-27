---
trigger: model_decision
description: "GitIgnore project requirements for api rules; consult the referenced AI.md PARTs for full rules."
---

# API Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never drift route methods, status codes, response shapes, auth scopes, or TLS requirements from the written contract.

## CRITICAL — ALWAYS DO

- Document and test every endpoint, preserve compatibility routes, and keep web, API, and machine-only surfaces within their specified access rules.

## Key rules summary

- Health, versioning, route contracts, API formats, OpenAPI, and TLS behavior are defined by AI.md and IDEA.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 13, PART 14, PART 15.
