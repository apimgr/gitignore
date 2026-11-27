# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never add targets beyond the prescribed set or use Makefile targets from CI.

## CRITICAL — ALWAYS DO

- Use the project Docker toolchain with scoped caches, resource limits and timeouts; keep test/build artifacts in their allowed locations.

## Key rules summary

- The Makefile is for local development only and has the exact targets, containerized build/test behavior, output paths, and gates specified by AI.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 25.
