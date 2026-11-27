# Project memory

`AI.md` is the read-only implementation source of truth. `IDEA.md` defines product behavior. Read the relevant sections before editing and follow `rules/`.

## Critical rules

- Never edit `AI.md`; implement its requirements and log only contradictions inside it in the parent `TODO.API.md`.
- Run Go builds and tests through the project Docker toolchain with `CGO_ENABLED=0`; do not run project binaries on the host.
- Keep runtime and test data in an organization and project scoped OS temporary directory, outside the source tree.
- Configuration is file based. This project has no user accounts, registration, or admin web panel.
- Keep browser pages, API behavior, and ReadTheDocs documentation aligned with the current specification and shipped routes.
