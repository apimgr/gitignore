# Binary Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never enable CGO or add unapproved binary suffixes. Never run built Go binaries on the host when the spec requires container execution.

## CRITICAL — ALWAYS DO

- Build through the specified Docker toolchain, preserve the exact artifact names and metadata, and keep server and client responsibilities separate.

## Key rules summary

- The server and client binary requirements, CLI behavior, artifact names, embedded build metadata, and supported targets come from AI.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 7, PART 8, PART 32.
