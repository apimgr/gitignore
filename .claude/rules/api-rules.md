# API Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never preserve project-owned obsolete routes as compatibility shims; external compatibility is defined by IDEA.md.
- Never expose sensitive operational details from health or diagnostics endpoints.
- Never weaken TLS or certificate validation.

## CRITICAL - ALWAYS DO

- Keep REST, OpenAPI/Swagger, and GraphQL routes and descriptions synchronized.
- Implement health and version behavior consistently across HTML, API, and text clients.
- Use proper status codes, response formats, and pagination/filtering where applicable.

For complete details, see AI.md PART 13, 14, 15.
