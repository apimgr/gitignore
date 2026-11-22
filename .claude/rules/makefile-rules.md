# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use Makefile targets in CI/CD.
- Never run Go tools or binaries on the host.
- Never write coverage or temporary test artifacts into the project tree.

## CRITICAL - ALWAYS DO

- Keep local targets limited to the six specified purposes: dev, local, build, test, release, and docker.
- `clean` is the only permitted additional core target; Makefile targets are for local development only.
- Run Go commands in `casjaysdev/go:latest`, with CGO disabled and project-scoped Go caches.
- Put test runtime and coverage output in the required project-scoped OS temp directory.

## KEY RULES

- Local development uses only the prescribed Make targets and containerized Go toolchain.
- Test data and caches use project-scoped paths outside the source tree.

For complete details, see AI.md PART 25.
