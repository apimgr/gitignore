# Binary and Client Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never enable CGO or rely on external runtime assets.
- Never omit a required platform or use a `-musl` binary suffix.
- Never run project binaries directly on the host.

## CRITICAL - ALWAYS DO

- Build static binaries from `./src` for linux, darwin, windows, and freebsd on amd64 and arm64.
- Embed assets and build metadata; follow the exact server and CLI binary naming rules.
- Build and distribute the required server and client binaries for all eight OS/architecture combinations; preserve static, reproducible release metadata.
- Keep server CLI and client CLI/TUI/GUI behavior, setup, language, and output requirements aligned with the spec.

## KEY RULES

- Go binaries are static, self-contained, and cross-compiled for the complete required platform matrix.
- Server and client naming, flags, embedded assets, and metadata follow their specified contracts.

For complete details, see AI.md PART 7, 8, 32.
