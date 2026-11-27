# Project instructions

Read `AI.md` for implementation rules and `IDEA.md` for product behavior before changing project files. `AI.md` is read-only. Use `.claude/rules/` for concise topic guidance; the full specification always wins.

## Critical rules

- Run Go builds and tests in the project Docker toolchain with `CGO_ENABLED=0`; do not run project binaries on the host.
- Keep runtime and test data in an organization and project scoped OS temporary directory outside the source tree.
- Configuration is file based; this project has no user accounts, registration, or admin web panel.
- Keep code, server pages, and ReadTheDocs documentation aligned with AI.md and shipped behavior.
