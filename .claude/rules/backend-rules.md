# Backend Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never interpolate untrusted input into SQL, filesystem paths, shell commands, or executable content.
- Never expose credentials, sensitive paths, or private data in logs or public responses.
- Never let Tor/I2P setup escape the application-managed configuration and data paths.
- Never route an inbound visitor-supplied destination through the application's outbound Tor client.

## CRITICAL - ALWAYS DO

- Use stable structured errors, safe cache headers, and version-aware cache invalidation.
- Use idempotent database schema creation/updates and prepared statements.
- Apply security headers, configurable abuse protection, structured audit/security logs, and safe log rotation.
- Treat internet-facing endpoints as hostile-input boundaries; minimize public health/diagnostic data and return generic public errors.
- Keep security reports encrypted and access-controlled; Tor hidden service starts when the Tor binary is found, while I2P is off on bare metal/VM and defaults on in containers.
- Keep database migrations idempotent and compatible with existing records; use the configured local or remote database backend and never store operator configuration in the database.
- Keep logs in the required plain-text formats, mask secrets, and do not emit terminal colors or decorative characters into logs.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| Where does runtime state live? | In configured data, config, and log paths, never the source tree. | PART 2, 9-11 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Inbound Tor | The app-managed hidden service that exposes the server. |
| Outbound Tor | App-controlled Tor client traffic to app-determined destinations. |

## KEY RULES

- Validate external input and fail safely without exposing private operational detail.
- Protect persistent data, secrets, logs, and security-report workflows.
- Keep Tor inbound and outbound paths separate; outbound requests may target only app-determined destinations.

For complete details, see AI.md PART 9, 10, 11, 31.
