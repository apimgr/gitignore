# TODO.AI.md

## [ ] Add "Third-Party Licenses" section to LICENSE.md
Read: AI.md PART 2

## [ ] Swap CORS dependency from go-chi/cors to github.com/rs/cors
Read: AI.md PART 3

## [ ] Fix src/config ParseBool to match spec vocabulary and signature
Add full truthy/falsy vocabulary, correct `(bool, error)` signature, and
`IsTruthy`/`IsFalsy`/`MustParseBool`/`IsDebug()` helpers.
Read: AI.md PART 5

## [ ] Add independent debug-flag tracking to src/mode
Mode (production/development) and debug (--debug/DEBUG) must be tracked as
two independent axes producing four operational states; currently
`ShouldShowDebugEndpoints()` just aliases `IsDevelopment()`.
Read: AI.md PART 6

## [ ] Create tests/ scaffolding (run_tests.sh, docker.sh, incus.sh)
Read: AI.md PART 28

## [ ] Create .github/ governance and CI/CD files
Workflows (release.yml, beta.yml, daily.yml, docker.yml), CODEOWNERS,
SECURITY.md, ISSUE_TEMPLATE/*, PULL_REQUEST_TEMPLATE.md, renovate.json.
Read: AI.md PART 27

## [ ] Create src/client/ (CLI client, required for all projects)
Read: AI.md PART 32

## [ ] Verify src/server/, src/service/ implementation depth against spec
Privilege escalation, maintenance mode, port selection, path-security
middleware were read in full at PART 23/24 but not verified against
current implementation.
Read: AI.md PART 23

## [ ] Implement PART 7-22 requirements not yet verified
Binary requirements, server CLI, error handling/caching, database,
security/logging, server configuration, health/versioning, API structure,
SSL/TLS, web frontend, email/notifications, scheduler, GeoIP, metrics,
backup/restore, update command — full implementation depth not audited
during bootstrap; run an explicit audit to verify compliance.
Read: AI.md PART 7

## [ ] Fix Makefile CasjaysDev convention violations
VERSION hardcoded instead of read from release.txt; PROJECTNAME/PROJECTORG
hardcoded instead of inferred from `git remote get-url origin`; build/test/run
targets invoke `go` directly on host instead of inside Docker; build entry
point is `.` instead of `./src`; missing required `dev` target; LDFLAGS
missing `-trimpath`.
Read: AI.md PART 3, ~/.claude/memory/go_conventions.md

## [ ] Fix docker/Dockerfile CasjaysDev convention violations
Uses `golang:alpine` instead of `casjaysdev/go:latest`; `go build` missing
inline `-buildvcs=false`; sets `main.CommitID` ldflag but main.go declares
`main.Commit`.
Read: AI.md PART 3, ~/.claude/memory/dockerfile_conventions.md

## [ ] Add missing CLI flags to src/main.go
Missing `-v` short flag for `--version`; missing `--debug` flag; missing
`--color` flag (values `auto`/`yes`/`no`, default `auto`).
Read: AI.md PART 5, ~/.claude/memory/go_conventions.md
