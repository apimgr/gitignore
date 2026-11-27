---
trigger: model_decision
description: "GitIgnore project requirements for docker rules; consult the referenced AI.md PARTs for full rules."
---

# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never run production or development Compose stacks as an AI test, mount runtime data into the checkout, or expose services on wildcard host interfaces.

## CRITICAL — ALWAYS DO

- Use the dedicated automated test stack, named and bounded containers, project-scoped temporary data, and the required startup chain.

## Key rules summary

- Dockerfiles and Compose files follow the project image, entrypoint, healthcheck, networking, mount and runtime-data requirements.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 26.
