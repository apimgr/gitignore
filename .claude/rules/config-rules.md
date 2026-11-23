# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never commit generated `server.yml` or runtime configuration.
- Never silently accept invalid values or log secret values; invalid known settings reset to documented defaults with the specified warning, while unknown keys are warned about and ignored.
- Never treat malformed YAML as a fatal startup error; retain last-good contents or defaults as AI.md defines.
- Never enable debug endpoints merely because development mode is active.

## CRITICAL - ALWAYS DO

- Merge `server.yml`, ordered `yaml.d/*.yml` overlays, and `secrets.yml`; preserve flag > environment > file > default precedence.
- Create configuration directories/files with the required restrictive permissions and never log secret values.
- Keep production as the default mode; gate debug endpoints on the explicit debug setting.
- Document restart requirements and reload only settings declared safe to reload.
- Keep node-local settings in `server.yml`; put other sections in their assigned `yaml.d/` area file and credentials only in `secrets.yml`.
- Apply defaults, `server.yml`, byte-wise ordered drop-ins, then secrets; maps merge recursively and lists replace as whole values.
- Ignore hidden, backup, unsupported, and nested drop-in entries; follow symlinks only when the regular-file target remains inside `yaml.d/`.
- Migrate legacy names before moving sections, create atomic backups, and never change/revalidate values during migration.
- Watch `server.yml`, `yaml.d/`, `secrets.yml`, and `assets/`; retain last-good data on parse/load failure and report restart-required settings through health output.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| What is configuration precedence? | Flags, environment, files, then defaults. | PART 5, 6, 12 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Drop-in | An ordered YAML overlay under `yaml.d/`. |
| Last-good config | The last valid configuration snapshot retained after a reload error. |

## KEY RULES

- Configuration is file-backed and deterministic; flags and environment overrides remain above file values.
- Invalid settings recover to defaults, malformed files preserve last-good state, and secrets remain protected.

For complete details, see AI.md PART 5, 6, 12.
