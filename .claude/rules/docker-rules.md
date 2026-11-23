# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

> Revalidated against AI.md on 2026-11-23.

## CRITICAL - NEVER DO

- Never copy or symlink build artifacts into arbitrary project or host paths.
- Never use a project directory for AI test runtime mounts or data.
- Never omit the required init process, health check, or safe runtime settings.
- Never copy or symlink host build artifacts into images as an alternate install path.

## CRITICAL - ALWAYS DO

- Follow the specified multi-stage build, static binary, Alpine runtime, tini entrypoint, OCI metadata, and rootfs overlay patterns.
- Keep the Dockerfile at `docker/Dockerfile`; use the specified Go build image and avoid an extra Go `Dockerfile.build` unless the spec explicitly requires one.
- Keep development, test, and deployment Compose configurations distinct and use project-scoped networks and ports.
- Keep runtime logs on stdout/stderr, one process per container, and state under specified `/config` and `/data` mounts.
- The Go project uses `casjaysdev/go:latest` directly; do not add a Go toolchain image or `ensure-build-image` workflow without an explicit project-specific need.
- Respect container-specific I2P default behavior while allowing explicit configuration to override it.

## KEY DECISIONS (pre-answered)

| Question | Answer | Spec Reference |
|---|---|---|
| Which Dockerfile builds the application image? | `docker/Dockerfile` using the specified multi-stage build. | PART 26 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Runtime image | The minimal image that runs the static application binary. |
| Overlay | A project rootfs file copied into its specified image path. |

## KEY RULES

- Ship a minimal static-binary runtime with init, health checks, metadata, and required filesystem overlays.
- Keep test/development runtime data outside the source tree and Compose environments separate.
- Keep Docker port bindings, volume paths, health checks, and image labels aligned with PART 26 and actual runtime configuration.

For complete details, see AI.md PART 26.
