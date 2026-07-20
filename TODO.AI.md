# TODO.AI.md

## [x] Add "Third-Party Licenses" section to LICENSE.md
Read: AI.md PART 2

## [x] Swap CORS dependency from go-chi/cors to github.com/rs/cors
Read: AI.md PART 3

## [x] Fix src/config ParseBool to match spec vocabulary and signature
Add full truthy/falsy vocabulary, correct `(bool, error)` signature, and
`IsTruthy`/`IsFalsy`/`MustParseBool`/`IsDebug()` helpers.
Read: AI.md PART 5

## [x] Add independent debug-flag tracking to src/mode
Mode (production/development) and debug (--debug/DEBUG) must be tracked as
two independent axes producing four operational states; currently
`ShouldShowDebugEndpoints()` just aliases `IsDevelopment()`.
Read: AI.md PART 6

## [x] Create tests/ scaffolding (run_tests.sh, docker.sh, incus.sh)
Read: AI.md PART 28

## [x] Create .github/ governance and CI/CD files
Workflows (release.yml, beta.yml, daily.yml, docker.yml), CODEOWNERS,
SECURITY.md, ISSUE_TEMPLATE/*, PULL_REQUEST_TEMPLATE.md, renovate.json.
CONTRIBUTING.md and CODE_OF_CONDUCT.md still missing per PART 1 public-repo
requirement — not covered by this item's original scope.
Read: AI.md PART 27

## [x] Create src/client/ (CLI client, required for all projects)
Implemented CLI (stdlib flag dispatch) + bubbletea/lipgloss TUI (default
interactive mode) + GUI stub behind `//go:build gui`. Cobra/viper migration
not done (PART 32's own Required Libraries table doesn't list them; only
the illustrative go.mod example does) — stdlib dispatch left in place.
Read: AI.md PART 32

## [x] Fix src/service/src/server gaps found by PART 23/24 audit
All P1-P3 items implemented: privilege drop after bind (src/server/
privilege_unix.go, privilege_windows.go); smart privilege escalation for
`--service --install` (service.DetectEscalation/ExecElevated/InstallUser);
uninstall confirmation + full cleanup (removeAllData/removeSystemUser);
OpenRC + SysVinit support in DetectServiceManager and all lifecycle
switches; macOS dscl service account creation, FreeBSD `pw useradd`,
Windows Virtual Service Account; spec status block in serviceStatus();
`--maintenance mode`/`setup` now persist via config.Update; installSystemd
creates dirs before EnsureSystemUser().
Path-security middleware still not found in src/server — flag for a
separate check once handleStatic/handleFavicon move past stub status.
Read: AI.md PART 23, PART 24

## [ ] Rename plural package dirs to singular (go-lint LAYOUT)
`src/paths/` -> `src/path/`, `src/templates/` -> `src/template/`,
`src/client/paths/` -> `src/client/path/`. Pre-existing convention
violation across the whole tree, not introduced by recent batches —
requires updating every import site project-wide; do as its own isolated
commit with a full build/vet/test pass, not a drive-by rename.
Read: ~/.claude/memory/go_conventions.md

## [ ] Implement PART 7-22 requirements not yet verified
Binary requirements, server CLI, error handling/caching, database,
security/logging, server configuration, health/versioning, API structure,
SSL/TLS, web frontend, email/notifications, scheduler, GeoIP, metrics,
backup/restore, update command — full implementation depth not audited
during bootstrap; run an explicit audit to verify compliance.
Read: AI.md PART 7

## [x] Fix Makefile CasjaysDev convention violations
VERSION hardcoded instead of read from release.txt; PROJECTNAME/PROJECTORG
hardcoded instead of inferred from `git remote get-url origin`; build/test/run
targets invoke `go` directly on host instead of inside Docker; build entry
point is `.` instead of `./src`; missing required `dev` target; LDFLAGS
missing `-trimpath`.
Read: AI.md PART 3, ~/.claude/memory/go_conventions.md

## [x] Fix docker/Dockerfile CasjaysDev convention violations
Uses `golang:alpine` instead of `casjaysdev/go:latest`; `go build` missing
inline `-buildvcs=false`; sets `main.CommitID` ldflag but main.go declares
`main.Commit`.
Read: AI.md PART 3, ~/.claude/memory/dockerfile_conventions.md

## [x] Add missing CLI flags to src/main.go
Missing `-v` short flag for `--version`; missing `--debug` flag; missing
`--color` flag (values `auto`/`yes`/`no`, default `auto`).
Read: AI.md PART 5, ~/.claude/memory/go_conventions.md

## [x] Implement gitignore.io route/API compatibility layer
Implemented in `src/server/compat_handlers.go`, registered in
`src/server/server.go` as unversioned `/api/list` and `/api/{list}` routes
alongside `/api/v1/*`, reusing `templates.Manager.Get`/`ListAll` — no
separate dataset. Exact route/status/body contract per IDEA.md "External
API Compatibility".
