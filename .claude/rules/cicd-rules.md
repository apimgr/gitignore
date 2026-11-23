# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never run workflow logic through Makefile targets.
- Never use unpinned third-party actions, expose secrets in logs, or allow unreviewed broad triggers.
- Never claim release integrity without producing the specified checksums, SBOM, and provenance.
- Never use Makefile targets as CI build steps or add inline tool installation to workflow jobs.

## CRITICAL - ALWAYS DO

- Build and test in the approved Go container with CGO disabled.
- Cover all eight server platforms and all eight CLI platforms when the client exists.
- Gate releases on tests, lint, vulnerability and secret scans, and publish the required metadata.
- Pin third-party actions to full commit SHAs, use the required Go container rather than setup-go, and verify provider CI after pushes.
- Keep CI independent of Makefile targets and publish the required checksums, SBOM, and provenance for releases.
- Keep equivalent required pipelines for GitHub, GitLab, Gitea, Forgejo, and Jenkins; use each provider's native CLI for status checks.
- Run secret scanning, vulnerability scanning, workflow policy checks, tests, lint, and the complete server/client platform matrix as blocking gates.
- For Go, use `casjaysdev/go:latest` directly; do not add a build-toolchain image or preflight unless a project-specific need is explicit.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| May CI use Makefile targets? | No; CI runs provider-native workflow steps directly. | PART 25, 27 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Provider workflow | Native CI configuration for a supported git provider. |
| Release provenance | Required build metadata and attestation for release artifacts. |

## KEY RULES

- Workflows use pinned actions and the approved container, with all required platform builds and quality gates.
- Release artifacts include integrity metadata; pushed changes are checked against provider CI results.
- Release workflows verify tag ownership and publish checksums, SBOM, and build provenance/attestation as required by AI.md.

For complete details, see AI.md PART 27.
