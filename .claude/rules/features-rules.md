# Feature Rules (PART 17, 18, 19, 20, 21, 22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use an external scheduler or enable optional network integrations without their documented opt-in.
- Never expose internal metrics publicly or store unbounded usage data.
- Never overwrite data during backup, restore, or update operations without the specified safety checks.

## CRITICAL - ALWAYS DO

- Keep email/notification content translated and SMTP behavior consistent with the documented auto-detection rules.
- Use the built-in scheduler, bounded GeoIP behavior, internal metrics, encrypted/compliant backups, and verified update flow as specified.
- Treat scheduler, GeoIP, metrics, backup/restore, and update support as required features; enforce bounds and integrity checks on their data and operations.

## KEY RULES

- Required operational features must be bounded, secure, and fully integrated with configuration and documentation.
- Network overlays are opt-in; backups and updates must verify integrity before changing data.

For complete details, see AI.md PART 17, 18, 19, 20, 21, 22.
