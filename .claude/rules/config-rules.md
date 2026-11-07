# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never commit generated `server.yml` or runtime configuration.
- Never ignore unknown configuration keys or silently accept invalid values.
- Never enable debug endpoints merely because development mode is active.

## CRITICAL - ALWAYS DO

- Use `server.yml`, validated configuration, sane defaults, and the prescribed flag > environment > file > default precedence.
- Keep production as the default mode; gate debug endpoints on the explicit debug setting.
- Document restart requirements and reload only settings declared safe to reload.

For complete details, see AI.md PART 5, 6, 12.
