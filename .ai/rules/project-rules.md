# Project Structure Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL — NEVER DO

- Never commit runtime data, credentials, generated build output, or machine-specific paths. Never omit required notices or third-party attributions.

## CRITICAL — ALWAYS DO

- Keep source, tests, docs, and runtime data in their specified locations. Resolve OS paths at runtime and use the stable internal project identifiers.

## Key rules summary

- Keep the prescribed repository structure, license and attribution, and portable OS-specific runtime paths. Generate runtime configuration and data at the locations specified by AI.md.
- Treat `AI.md` as the complete contract; use these notes only as a quick index.
- Check the named PARTs before changing related code, configuration, tests, or documentation.

For complete details, see `AI.md` PART 2, PART 3, PART 4.
