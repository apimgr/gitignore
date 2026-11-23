# Testing, Documentation, and I18N Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never run tests, project binaries, or host-affecting commands directly on the host.
- Never write test data or coverage files into the source tree or use a bare `/tmp` path.
- Never hardcode user-facing text when translation keys are required.
- Never treat unit tests as a substitute for required endpoint, browser, binary, and no-JavaScript behavior checks.

## CRITICAL - ALWAYS DO

- Keep unit, binary integration, content-negotiation, and browser E2E coverage aligned with IDEA.md and AI.md.
- Use `${TMPDIR:-/tmp}/{project_org}/{internal_name}-XXXXXX` for runtime/test output.
- Keep ReadTheDocs pages current with shipped browser, CLI, API, configuration, and integration behavior; build with MkDocs Material.
- Support at least the six required locales, with complete key parity for every embedded locale; keep RTL, fallback, and accessibility behavior aligned.
- Translate every human-readable string and keep locale registries, embedded files, and language lists synchronized.
- Keep every embedded locale shared by server and client; update each locale in the same change as English keys and validate key parity.
- Require CI conformance coverage for every registered/error page and browser E2E paths with and without JavaScript.
- Keep ReadTheDocs navigation and required pages complete, including public discovery/protocol endpoints and all operator/integrator-facing behavior.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| Where do Go tests run? | Through the prescribed container-backed `make test` workflow. | PART 28 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Page conformance | Automated coverage of each registered and error page’s required structure and behavior. |
| Locale parity | Every embedded locale has the same translation keys as English. |

## KEY RULES

- Tests and docs describe the actual shipped behavior and run in the prescribed isolated environment.
- Every human-readable string is translated; all supported locales have complete key parity.
- Do not claim a language in docs/help/selector unless it exists in the locale registry and embedded files.

For complete details, see AI.md PART 28, 29, 30.
