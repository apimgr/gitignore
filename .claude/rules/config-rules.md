# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never commit generated `server.yml` or runtime configuration.
- Never ignore unknown configuration keys or silently accept invalid values; warn and replace invalid values with documented defaults.
- Never enable debug endpoints merely because development mode is active.

## CRITICAL - ALWAYS DO

- Merge `server.yml`, ordered `yaml.d/*.yml` overlays, and `secrets.yml`; preserve flag > environment > file > default precedence.
- Create configuration directories/files with the required restrictive permissions and never log secret values.
- Keep production as the default mode; gate debug endpoints on the explicit debug setting.
- Document restart requirements and reload only settings declared safe to reload.

## KEY RULES

- Configuration sources merge deterministically, with command-line and environment overrides taking precedence.
- Invalid settings recover to safe defaults with a warning; secret values remain protected.

For complete details, see AI.md PART 5, 6, 12.
