# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never copy or symlink build artifacts into arbitrary project or host paths.
- Never use a project directory for AI test runtime mounts or data.
- Never omit the required init process, health check, or safe runtime settings.

## CRITICAL - ALWAYS DO

- Follow the specified multi-stage build, static binary, Alpine runtime, tini entrypoint, OCI metadata, and rootfs overlay patterns.
- Keep development, test, and deployment Compose configurations distinct and use project-scoped networks and ports.

For complete details, see AI.md PART 26.
