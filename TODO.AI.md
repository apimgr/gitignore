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

## [ ] Fix src/service/src/server gaps found by PART 23/24 audit
Audit complete; UID/GID range fixed directly. Remaining, in priority order:
- P1: privilege drop after bind (setuid/setgid to `gitignore` user once
  listener is bound, Unix build-tagged) — src/server or src/main.go
- P1: smart privilege escalation for `--service --install` (detect
  sudo/su/pkexec/doas, fall back to user service, else informative error)
  — src/main.go handleServiceCommand + src/service
- P2: service uninstall must prompt for confirmation and remove
  config/data/cache/log/backup dirs, PID file, and the system user/group
  — src/service/service.go uninstall* functions
- P2: add OpenRC and SysVinit init-system support to DetectServiceManager
  and the install/uninstall/start/stop/restart/reload switches
- P2: macOS launchd plist must not hardcode UserName/GroupName (start as
  root, drop privileges) or must create the dscl service account (200-399,
  IsHidden) it currently references but never creates; FreeBSD installBSDRC
  must create the service user via `pw useradd` before writing the rc
  script; Windows installWindows must create a Virtual Service Account
  instead of defaulting to LocalSystem
- P3: serviceStatus()/`--service --help` must show the spec's status block
  (installed/state/auto-start/PID) instead of a hard-coded line
- P3: `--maintenance mode <production|development>` must persist via
  config.Update, and `--maintenance setup` must actually reset server
  configuration, instead of both just printing
- P3: installSystemd must create the home/config dir before
  EnsureSystemUser() runs
- Path-security middleware: not found in src/server; flag for a
  separate check once handleStatic/handleFavicon move past stub status
Read: AI.md PART 23, PART 24

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
