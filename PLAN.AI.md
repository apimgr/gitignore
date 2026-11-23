# AI.md Alignment Plan

## Objective

Bring the current repository into complete alignment with AI.md and IDEA.md. AI.md remains read-only and authoritative. Record only AI.md contradictions in the parent `TODO.API.md`, after checking for an existing duplicate. Track project implementation work only in `TODO.AI.md`.

## Work sequence

1. Complete the remaining read-only requirement inventory from AI.md PART 0–32, IDEA.md, project rules, current code, all server pages, README, docs, and existing tests. Preserve current user changes.
2. Align the owner-token cookie identifier with AI.md's lowercase suffix rule. A successful validation of the prior cookie transparently writes the compliant name and expires the prior name; AI.md does not specify this persistent-cookie migration, so report that guidance gap to the coordinating session.
3. Complete browser-page localization across every rendered page and all supported locales. Translate visible text, accessible labels, titles, metadata, legal content, and no-JavaScript messages; preserve behavior and add deterministic locale-conformance coverage.
4. Verify and complete localization of other human-readable output required by PART 30, including API errors, health/status, OpenAPI/Swagger, GraphQL, email, and server/client CLI output.
5. Bring README and all ReadTheDocs content into current alignment. Provide a per-setting configuration reference with defaults, accepted values, validation, and canonical file locations; document routes, formats, security/reporting, discovery, integrations, CLI, and operations.
6. Fix any additional confirmed project gaps from the full requirement inventory. Keep AI.md defects out of project TODOs and avoid duplicate parent TODO.API entries.
7. Run the required local gates, docs build, browser E2E/layout matrix, and provider workflow checks. Review the complete diff and resolve any failure before committing.
8. Commit the implementation with TODO.AI.md items still present. After that commit succeeds, remove completed items according to AI.md's TODO completion sequence; use the exact required completion message when deleting the final item. Push through `gitcommit` and verify CI after each commit.

## Constraints and risks

- Read the exact AI.md section and existing target file before every write; never edit AI.md.
- Preserve no-JavaScript form behavior, route contracts, accessibility, locale key/placeholder parity, and supported-language registry parity.
- Do not expose or store credentials in documentation. Follow AI.md over project-owned documents and update IDEA.md when its variables or behavior conflict.
- Keep test/build/runtime artifacts in project-scoped OS temporary storage and remove artifacts created for this work when finished.
- The comprehensive browser matrix and configuration inventory may reveal additional gaps; add those to TODO.AI.md before expanding implementation scope.
