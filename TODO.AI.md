# AI.md Alignment Tasks

## High (P1) — Required project foundations

- [x] Add the AI.md-required Claude rule summaries, project memory index, and root instruction loaders without changing AI.md.
- [ ] Align Makefile, Docker, and release/CI workflows with the applicable Go, Docker, CI, and temp-directory requirements in AI.md.
- [ ] Complete the required automated binary and browser E2E test coverage, with all runtime data in project-scoped OS temp directories.
- [ ] Audit application, API, frontend, configuration, security, scheduler, service, i18n, client, and documentation behavior against AI.md and IDEA.md; fix verified gaps and keep paired documentation synchronized.

## Verification

- [ ] Run containerized project tests, lint, platform builds, Docker validation, and documentation build; record any checks blocked by the execution environment.

The containerized `make test` gate was attempted through the required test/lint
wrapper with a 90-second bound on 2026-10-31. Docker created the Go image
container but left it in `Created` state without starting it; the command timed
out before emitting test output. The container created by that attempt was
removed. Retry the verification gate when Docker can start containers.
- gitignore: cmd/i18n-validate is an entry point outside src/ (layout); CI go build lacks -ldflags strip/build-info flags
