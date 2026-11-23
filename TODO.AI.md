# Current AI.md Alignment Tasks

## Critical (P0)

- [ ] Resolve the unused credential-shaped project variable in IDEA.md in accordance with AI.md and the explicit-confirmation rule for changing project variables.

## High (P1)

- [ ] Complete AI.md PART 30 localization for every rendered server page: move all human-readable UI text to translation keys, provide natural translations in every supported locale, preserve page behavior and no-JavaScript flows, and add conformance coverage.
- [ ] Complete AI.md PART 30 localization across non-web output, including API errors and validation, OpenAPI/Swagger, GraphQL, email, health/status, and both server and client CLI surfaces; verify key and placeholder parity in every supported locale.
- [ ] Bring the mobile navigation drawer into AI.md PART 16 compliance: body-level drawer and scrim, full-height safe-area layout, RTL direction, CSS-only open/close and scroll lock, keyboard enhancement, tokenized stacking, and automated viewport coverage.
- [ ] Bring README.md and every ReadTheDocs page into full alignment with the shipped product, including the complete configuration reference, routes and formats, security/reporting flows, discovery endpoints, integrations, CLI behavior, and operational guidance.
- [ ] Remove disallowed configuration/example artifacts and verify deployment documentation contains no stale `.env` setup; `.env.example` is removed in the working tree and remains unresolved until committed.

## Medium (P2)

- [ ] Complete a requirement-by-requirement verification of all remaining applicable AI.md PARTs and IDEA.md business behavior, including rendered page behavior, accessibility, responsive/RTL layouts, integrations, and CI/provider requirements; fix any additional confirmed project gaps.
