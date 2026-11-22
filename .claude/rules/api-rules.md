# API Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never preserve project-owned obsolete routes as compatibility shims; external compatibility is defined by IDEA.md.
- Never expose sensitive operational details from health or diagnostics endpoints.
- Never weaken TLS or certificate validation.
- Never add an unversioned alias unless AI.md explicitly defines it, and never duplicate endpoint logic between aliases.

## CRITICAL - ALWAYS DO

- Keep REST, OpenAPI/Swagger, and GraphQL routes and descriptions synchronized.
- Use `/api/{api_version}/...` as the canonical API namespace; preserve only explicitly specified unversioned aliases and route them to shared handlers.
- Implement health and version behavior consistently across HTML, API, and text clients.
- Use proper status codes, response formats, and pagination/filtering where applicable.
- Preserve content negotiation and the documented error envelope on every route.
- Keep route tables, OpenAPI operations, GraphQL schema/resolvers, and server-rendered pages synchronized with implemented handlers.

## KEY RULES

- Canonical routes are versioned; aliases exist only where AI.md explicitly specifies them.
- Keep API contracts and equivalent HTML/text behavior synchronized.
- Public health output follows exact field-visibility rules; detailed diagnostics stay operator-only.

For complete details, see AI.md PART 13, 14, 15.
