# Backend Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never interpolate untrusted input into SQL, filesystem paths, shell commands, or executable content.
- Never expose credentials, sensitive paths, or private data in logs or public responses.
- Never let Tor/I2P setup escape the application-managed configuration and data paths.

## CRITICAL - ALWAYS DO

- Use stable structured errors, safe cache headers, and version-aware cache invalidation.
- Use idempotent database schema creation/updates and prepared statements.
- Apply security headers, configurable abuse protection, structured audit/security logs, and safe log rotation.

For complete details, see AI.md PART 9, 10, 11, 31.
