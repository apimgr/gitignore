# Testing, Documentation, and I18N Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never run tests, project binaries, or host-affecting commands directly on the host.
- Never write test data or coverage files into the source tree or use a bare `/tmp` path.
- Never hardcode user-facing text when translation keys are required.

## CRITICAL - ALWAYS DO

- Keep unit, binary integration, content-negotiation, and browser E2E coverage aligned with IDEA.md and AI.md.
- Use `${TMPDIR:-/tmp}/{project_org}/{internal_name}-XXXXXX` for runtime/test output.
- Keep ReadTheDocs pages current with shipped browser, CLI, API, configuration, and integration behavior; build with MkDocs Material.
- Support at least the six required locales, with complete key parity for every embedded locale; keep RTL, fallback, and accessibility behavior aligned.
- Translate every human-readable string and keep locale registries, embedded files, and language lists synchronized.

## KEY RULES

- Tests and docs describe the actual shipped behavior and run in the prescribed isolated environment.
- Every human-readable string is translated; all supported locales have complete key parity.

For complete details, see AI.md PART 28, 29, 30.
