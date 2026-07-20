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

## [x] Rename plural package dirs to singular (go-lint LAYOUT)
`src/paths/` -> `src/path/`, `src/templates/` -> `src/template/`,
`src/client/paths/` -> `src/client/path/`, package declarations and every
import site updated project-wide. The `src/path` and `src/client/path`
imports are aliased (`apppath`/`clipath`) at their call sites rather than
imported as bare `path` — several functions already use a local variable/
parameter named `path` (e.g. `service.fileExists(path string)`,
`config.Load`'s local `path :=`), and importing the package unaliased
would silently shadow it. No stdlib `path`/`html/template` import
collisions found in the files touched. Verified with a full Docker
build/vet/test pass (exit 0) and a go-lint re-run.
Read: ~/.claude/memory/go_conventions.md

## [x] Implement PART 7-22 requirements not yet verified
Implemented: security headers + fixed-window per-IP rate limiter
(src/server/middleware.go, IP resolved from r.RemoteAddr with port
stripped only — X-Forwarded-For/X-Real-IP deliberately not trusted,
see the trusted-proxy allowlist follow-up below), Prometheus
text-exposition /metrics
(src/server/metrics.go, request counters keyed by chi route pattern to
bound cardinality), unified API envelope + Cache-Control policy classes
(src/server/response.go), TLS wiring via ssl.Manager in Server.Start
(configureTLS + ServeTLS branch), OpenAPI 3.0 JSON generation + Swagger UI
+ GraphiQL + GraphQL SDL (src/server/openapi.go), embedded HTML frontend
(src/server/assets.go + assets/html/*, assets/static/*, previously stub
"TODO: Render HTML template" handlers), tar.gz template export, SQLite
pool bounds + PingContext reachability check (src/db/db.go), TODO stub
handlers replaced across src/server/handlers.go, web_handlers.go, and
src/templates/handlers.go (unified `ok` envelope, sendAPIResponseError).
Deferred (documented as new follow-ups below rather than implemented):
GraphQL execution engine, Email/SMTP, GeoIP mmdb, self-update command,
full CSP/Permissions-Policy config tree, SRI pinning for CDN assets,
/metrics bearer-token auth, and the PART 14 root/versioned route-naming
scheme.
Read: AI.md PART 7

## [ ] Restructure routes to match PART 14 "Route Naming Convention" /
"Root-Level Endpoints" exactly
Current implementation uses a flat `/api/v1/*` namespace (e.g.
`/api/v1/template/{name}`, `/api/v1/openapi.json`) and still serves the
spec's explicitly-removed root paths `/openapi`, `/openapi.json`, `/graphql`
(GET+POST). PART 14 requires: `/api/swagger` + `/api/{api_version}/server/
swagger` (OpenAPI JSON, no `.json` suffix on the path), `/api/graphql` +
`/api/{api_version}/server/graphql`, `/api/healthz` +
`/api/{api_version}/server/healthz`, `/server/docs/swagger` and
`/server/docs/graphql` UI pages at root, `/api/autodiscover`, and an
`/api/{api_version}/server/*` namespace for operator endpoints (info public,
mutating ones behind an operator token per PART 11). Left unfixed this
session — full blast radius (every route, the CLI client's API base paths,
docs/swagger page URLs, the gitignore.io compat layer's route table, and
tests) makes this unsafe as a drive-by change; do as its own isolated
commit with a full build/vet/test pass.
Read: AI.md PART 14 "Route Naming Convention", "Root-Level Endpoints"

## [ ] Implement or explicitly stub remaining PART 7-22 deferred items
GraphQL execution engine (handleGraphQL currently returns NOT_IMPLEMENTED),
Email/SMTP notifications, GeoIP mmdb lookups, self-update command, full
CSP/Permissions-Policy config tree (currently a fixed CSP string, not
operator-configurable), Subresource Integrity hashes for the Swagger UI/
GraphiQL CDN assets (jsdelivr script/style tags), and Bearer-token auth
gating on `/metrics` (currently unauthenticated, relies on the operator to
firewall it).
Read: AI.md PART 15, PART 17-22

## [ ] Add trusted-proxy CIDR allowlist for rate-limit client IP resolution
`clientIP()` (src/server/middleware.go) currently uses `r.RemoteAddr` only,
deliberately not trusting `X-Forwarded-For`/`X-Real-IP` since the project
has no operator-configured trusted-proxy list — honoring those headers
unconditionally would let any client spoof a fresh IP per request and
bypass the rate limiter. When deployed behind a reverse proxy, real client
IPs are needed for accurate limiting: add a `server.rate_limit.trusted_proxies`
config list of CIDRs, and only honor forwarded headers (rightmost entry)
when `r.RemoteAddr` matches one.
Read: AI.md PART 11

## [x] go-lint LAYOUT/EXIT findings from PART 7-22 batch
Fixed: src/server/assets.go used log.Fatalf on embed failures — switched to
fmt.Fprintf(os.Stderr,...) + os.Exit(exOSFile) (sysexits 72, matches
src/main.go's exOSFile) since a missing embedded asset means the build
itself is broken, not a recoverable runtime condition.
Not fixed, judged non-applicable: go-lint flagged src/server/openapi.go's
GraphiQL playground HTML for "React client-side rendering" violating
"server-side Go templates only". GraphiQL is a self-contained third-party
dev-tool page with no server-rendered equivalent — same category as the
already-unflagged Swagger UI bundle on the same page. The "no client-side
rendering" rule targets the app's own core content (search/list/template
pages, all server-rendered via src/server/assets/html/*), not an embedded
CDN developer tool. Left as-is.
A later go-lint re-run (after the singular-directory rename below) also
flagged the Swagger UI block itself (src/server/openapi.go lines
115-126, swagger-ui-dist CDN JS) under the same FORBIDDEN rule. Same
judgment applies and is reaffirmed here: Swagger UI is the same category
of self-contained CDN dev tool as GraphiQL, with no server-rendered
equivalent — left as-is for the same reason.
Read: ~/.claude/memory/go_conventions.md, ~/.claude/memory/ui_ux_conventions.md

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
