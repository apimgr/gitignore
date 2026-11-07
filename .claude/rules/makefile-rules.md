# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use Makefile targets in CI/CD.
- Never run Go tools or binaries on the host.
- Never write coverage or temporary test artifacts into the project tree.

## CRITICAL - ALWAYS DO

- Keep local targets limited to the six specified purposes: dev, local, build, test, release, and docker.
- Run Go commands in `casjaysdev/go:latest`, with CGO disabled and project-scoped Go caches.
- Put test runtime and coverage output in the required project-scoped OS temp directory.

For complete details, see AI.md PART 25.
