# Binary and Client Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never enable CGO or make core operation depend on unembedded assets; configured branding assets follow the explicit PART 16 file/URL/data rules.
- Never omit a required platform or use a `-musl` binary suffix.
- Never run project binaries directly on the host.

## CRITICAL - ALWAYS DO

- Build static binaries from `./src` for linux, darwin, windows, and freebsd on amd64 and arm64.
- Embed assets and build metadata; follow the exact server and CLI binary naming rules.
- Build and distribute the required server and client binaries for all eight OS/architecture combinations; preserve static, reproducible release metadata.
- Keep server CLI and client CLI/TUI/GUI behavior, setup, language, and output requirements aligned with the spec.
- Use the actual executable basename for displayed command names and fixed project identity for user-agent and on-disk paths.
- Keep the required client available in every project; synchronize translations across server and client binaries.
- Verify downloads with the published SHA-256 and replace binaries atomically; never update from an unverified response.

## KEY RULES

- Go binaries are static, self-contained, and cross-compiled for the complete required platform matrix.
- Server and client naming, flags, embedded assets, and metadata follow their specified contracts.
- CLI token/config files use user-only permissions; auto-update follows documented interactive and non-interactive rules.

For complete details, see AI.md PART 7, 8, 32.
