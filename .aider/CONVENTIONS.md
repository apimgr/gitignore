# Project Conventions (Auto-generated from AI.md)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

AI.md is read-only and authoritative for implementation. IDEA.md is authoritative for product behavior. Read the referenced PARTs before changing related files.

## AI Assistant Rules (PART 0, 1)

**Key rules:** AI.md and any non-empty SPEC.md are the implementation authority; IDEA.md defines product behavior. Keep AI.md read-only. Preserve project and user changes. Complete requested work and verify it; never claim an unrun check.

**Never:** Never edit AI.md or invent behavior missing from IDEA.md. Never treat existing code or docs as more authoritative than the specification.

**Always:** Read the relevant AI.md and IDEA.md sections before each task. Keep progress and task tracking in the prescribed TODO/PLAN files, and use the required commit and verification workflow.

For complete details, see `AI.md` PART 0, PART 1.

## Project Structure Rules (PART 2, 3, 4)

**Key rules:** Keep the prescribed repository structure, license and attribution, and portable OS-specific runtime paths. Generate runtime configuration and data at the locations specified by AI.md.

**Never:** Never commit runtime data, credentials, generated build output, or machine-specific paths. Never omit required notices or third-party attributions.

**Always:** Keep source, tests, docs, and runtime data in their specified locations. Resolve OS paths at runtime and use the stable internal project identifiers.

For complete details, see `AI.md` PART 2, PART 3, PART 4.

## Configuration Rules (PART 5, 6, 12)

**Key rules:** Configuration defaults, modes, validation, precedence, and server settings are specified by AI.md. Settings are file-based and operator-editable.

**Never:** Never add an undocumented setting, runtime admin UI, or checked-in runtime config. Never silently accept malformed configuration.

**Always:** Validate config at load boundaries, document supported settings, apply the defined precedence, and preserve safe defaults and mode-specific behavior.

For complete details, see `AI.md` PART 5, PART 6, PART 12.

## Binary Rules (PART 7, 8, 32)

**Key rules:** The server and client binary requirements, CLI behavior, artifact names, embedded build metadata, and supported targets come from AI.md.

**Never:** Never enable CGO or add unapproved binary suffixes. Never run built Go binaries on the host when the spec requires container execution.

**Always:** Build through the specified Docker toolchain, preserve the exact artifact names and metadata, and keep server and client responsibilities separate.

For complete details, see `AI.md` PART 7, PART 8, PART 32.

## Backend Rules (PART 9, 10, 11, 31)

**Key rules:** Follow the specified error and cache behavior, database schema and lifecycle, security/logging controls, and Tor/I2P overlay behavior.

**Never:** Never ignore meaningful errors, expose secrets or sensitive report data, bypass security checks, or let optional overlay failures take down the main service.

**Always:** Validate untrusted input, use bounded resources and safe database operations, redact sensitive logs, and make cleanup and failure paths explicit.

For complete details, see `AI.md` PART 9, PART 10, PART 11, PART 31.

## API Rules (PART 13, 14, 15)

**Key rules:** Health, versioning, route contracts, API formats, OpenAPI, and TLS behavior are defined by AI.md and IDEA.md.

**Never:** Never drift route methods, status codes, response shapes, auth scopes, or TLS requirements from the written contract.

**Always:** Document and test every endpoint, preserve compatibility routes, and keep web, API, and machine-only surfaces within their specified access rules.

For complete details, see `AI.md` PART 13, PART 14, PART 15.

## Frontend Rules (PART 16)

**Key rules:** Use the shared server-rendered page shell and catalogued components, styles, templates, translations, and accessibility behavior in AI.md.

**Never:** Never add inline styles, executable inline scripts, inline event handlers, uncatalogued styling classes, or JavaScript-only core behavior.

**Always:** Use semantic HTML, the shared CSS layers and tokens, responsive logical properties, accessible labels/focus states, and the required page/component conformance tests.

For complete details, see `AI.md` PART 16.

## Feature Rules (PART 17-22)

**Key rules:** Email/notifications, scheduler, GeoIP, metrics, backup/restore, and update behavior must match their dedicated AI.md contracts.

**Never:** Never make optional integrations mandatory at startup or expose protected operational data publicly.

**Always:** Honor configured enablement, validate inputs and retention limits, report feature health, and preserve the specified failure and recovery behavior.

For complete details, see `AI.md` PART 17-22.

## Service Rules (PART 23, 24)

**Key rules:** Privilege escalation, service management, packaging, and supported operating systems follow the exact AI.md lifecycle and platform contracts.

**Never:** Never escalate privileges unnecessarily or run service install/management actions on the development host during tests.

**Always:** Keep service operations explicit and reversible, use supported service managers, and test platform-specific behavior in the required isolated environment.

For complete details, see `AI.md` PART 23, PART 24.

## Makefile Rules (PART 25)

**Key rules:** The Makefile is for local development only and has the exact targets, containerized build/test behavior, output paths, and gates specified by AI.md.

**Never:** Never add targets beyond the prescribed set or use Makefile targets from CI.

**Always:** Use the project Docker toolchain with scoped caches, resource limits and timeouts; keep test/build artifacts in their allowed locations.

For complete details, see `AI.md` PART 25.

## Docker Rules (PART 26)

**Key rules:** Dockerfiles and Compose files follow the project image, entrypoint, healthcheck, networking, mount and runtime-data requirements.

**Never:** Never run production or development Compose stacks as an AI test, mount runtime data into the checkout, or expose services on wildcard host interfaces.

**Always:** Use the dedicated automated test stack, named and bounded containers, project-scoped temporary data, and the required startup chain.

For complete details, see `AI.md` PART 26.

## CI/CD Rules (PART 27)

**Key rules:** Ship provider-equivalent workflows for the required GitHub, GitLab, Gitea, Forgejo and Jenkins targets; build and test the real project artifacts.

**Never:** Never call make in CI, use unpinned third-party actions, expose secrets to untrusted code, or install tools inline when the spec forbids it.

**Always:** Run explicit commands with required environment values, least privilege, complete secret/dependency/image scans, and verify provider CI after pushes.

For complete details, see `AI.md` PART 27.

## Testing Rules (PART 28, 29, 30)

**Key rules:** Run tests in the specified isolation; maintain ReadTheDocs content and the complete locale/accessibility requirements in AI.md.

**Never:** Never use the source checkout for runtime/test data, run host-affecting tests on the host, or claim verification without actual output.

**Always:** Run the prescribed test, lint, browser/layout, docs and i18n checks; document current shipped behavior and keep translation keys/placeholders synchronized.

For complete details, see `AI.md` PART 28, PART 29, PART 30.
