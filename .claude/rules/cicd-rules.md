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
- Pin third-party actions to full commit SHAs, use the required Go container rather than setup-go, and verify provider CI after pushes.
- Keep CI independent of Makefile targets and publish the required checksums, SBOM, and provenance for releases.

## KEY RULES

- Workflows use pinned actions and the approved container, with all required platform builds and quality gates.
- Release artifacts include integrity metadata; pushed changes are checked against provider CI results.

For complete details, see AI.md PART 27.
