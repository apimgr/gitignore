# Testing, Documentation, and I18N Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never run tests, project binaries, or host-affecting commands directly on the host.
- Never write test data or coverage files into the source tree or use a bare `/tmp` path.
- Never hardcode user-facing text when translation keys are required.

## CRITICAL - ALWAYS DO

- Keep unit, binary integration, content-negotiation, and browser E2E coverage aligned with IDEA.md and AI.md.
- Use `${TMPDIR:-/tmp}/{project_org}/{internal_name}-XXXXXX` for runtime/test output.
- Build the ReadTheDocs documentation and keep all six locales, accessibility behavior, and fallback rules synchronized.

For complete details, see AI.md PART 28, 29, 30.
