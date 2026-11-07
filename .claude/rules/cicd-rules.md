# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never run workflow logic through Makefile targets.
- Never use unpinned third-party actions, expose secrets in logs, or allow unreviewed broad triggers.
- Never claim release integrity without producing the specified checksums, SBOM, and provenance.

## CRITICAL - ALWAYS DO

- Build and test in the approved Go container with CGO disabled.
- Cover all eight server platforms and all eight CLI platforms when the client exists.
- Gate releases on tests, lint, vulnerability and secret scans, and publish the required metadata.

For complete details, see AI.md PART 27.
