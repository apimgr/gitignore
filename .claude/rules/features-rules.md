# Feature Rules (PART 17, 18, 19, 20, 21, 22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never use an external scheduler or route an inbound caller-controlled destination through Tor.
- Never expose internal metrics publicly or store unbounded usage data.
- Never overwrite data during backup, restore, or update operations without the specified safety checks.

## CRITICAL - ALWAYS DO

- Keep email/notification content translated and SMTP behavior consistent with the documented auto-detection rules.
- Use the built-in scheduler, bounded GeoIP behavior, internal metrics, encrypted/compliant backups, and verified update flow as specified.
- Treat scheduler, GeoIP, metrics, backup/restore, and update support as required features; enforce bounds and integrity checks on their data and operations.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| What runs periodic maintenance? | The application’s built-in scheduler. | PART 18 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| GeoIP | Approximate IP geolocation used as a risk signal, not identity proof. |
| I2P provider | The app-managed i2pd process or configured external SAM bridge. |

## KEY RULES

- Required operational features must be bounded, secure, and fully integrated with configuration and documentation.
- Tor hidden service starts when a Tor binary is found; I2P is opt-in on bare metal/VM and defaults on inside a container. Both use isolated app-managed paths.
- Backups must verify integrity and apply specified encryption/retention rules; updates verify checksums/signatures before atomic replacement.
- GeoIP and blocklists fail safely, respect allowlists and configured reload behavior, and never replace other security controls.

For complete details, see AI.md PART 17, 18, 19, 20, 21, 22.
