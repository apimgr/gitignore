---
trigger: model_decision
description: "GitIgnore project requirements for ci/cd rules; consult the referenced AI.md PARTs for full rules."
---

# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never call make in CI, use unpinned third-party actions, expose secrets to untrusted code, or install tools inline when the spec forbids it.

## CRITICAL — ALWAYS DO

- Run explicit commands with required environment values, least privilege, complete secret/dependency/image scans, and verify provider CI after pushes.

## Key rules summary

- Ship provider-equivalent workflows for the required GitHub, GitLab, Gitea, Forgejo and Jenkins targets; build and test the real project artifacts.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 27.
