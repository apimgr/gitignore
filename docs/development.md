# Development Guide

## Prerequisites

- Git and Make
- Docker with access to the `casjaysdev/go:latest` image

The Makefile runs Go builds, tests, and static checks in Docker. Do not run Go build or test commands directly on the host; see the repository's [AI.md file](https://github.com/apimgr/gitignore/blob/main/AI.md) for the complete development rules.

## Layout

- `src/` — server and shared Go packages
- `src/client/` — companion CLI and terminal interface
- `src/server/` — HTTP routes, handlers, templates, and embedded assets
- `src/common/i18n/locales/` — embedded locale JSON files
- Locale message-key, placeholder, and plural validation lives in `src/common/i18n/*_test.go` and runs as part of the regular Go test suite.
- `docker/` — Dockerfiles and Compose definitions
- `docs/` — MkDocs and ReadTheDocs documentation
- `tests/` — Docker and optional integration/browser test runners

## Build

| Target | Behavior |
|---|---|
| `make build` | Build debug server and client binaries for the local platform and all eight supported targets. |
| `make test` | Run Go vet, staticcheck, govulncheck, shell checks, unit tests with coverage, then the basic binary checks in Docker. |
| `make release` | Build production binaries, source archive, and checksums locally; it does not tag, push, or publish. |
| `make docker` | Build the host-platform container image into the local image store; it does not push. |
| `make clean` | Remove `binaries/` and `releases/`. |
| `make help` | List the available targets. |

```sh
make build
make test
```

The Go toolchain runs in the Makefile's Docker image. The binaries are static (`CGO_ENABLED=0`) and contain their web assets, template data, and locale files.

## Test and security checks

```sh
make test
```

This target runs `go vet`, `staticcheck`, `govulncheck`, ShellCheck and shfmt for test scripts, the Go test suite with coverage enforcement, and then the basic binary checks through `tests/run_tests.sh`. Translation-key consistency is covered by the test suite. Optional integration and browser scripts are in `tests/`; consult their help or source before running them because some require Incus or a configured browser environment.

`./tests/run_tests.sh` runs the basic Docker binary-validation suite. Run `./tests/incus.sh` separately for the full systemd and OpenRC service matrix; it also checks restart persistence, overlay-provider behavior, and uninstall cleanup. If a temporary Incus guest cannot resolve its package mirror through the bridge, set `INCUS_DNS_SERVER` to an accessible IPv4 resolver; the override is scoped to that disposable guest. The browser suite is on-demand after major frontend changes and covers server-rendered, no-JavaScript, and full-JavaScript behavior:

```sh
./tests/e2e.sh
```

Each run removes its temporary Go caches by default. For repeat runs, set `PROJECT_E2E_CACHE_DIR` to a project-scoped temporary directory; the runner reuses its Go build and module caches there while keeping each run's database, logs, and browser artifacts separate:

```sh
cache_parent="${TMPDIR:-/tmp}/apimgr"
mkdir -p "$cache_parent"
cache_dir=$(mktemp -d "$cache_parent/gitignore-e2e-cache-XXXXXX")
PROJECT_E2E_CACHE_DIR="$cache_dir" ./tests/e2e.sh
```

## Release artifacts

```sh
make build
make release
```

`make release` builds the production platform matrix and prepares version, source archive, and checksum assets under `releases/`; it never tags, pushes, or publishes. CI handles stable, beta, and devel releases. `make docker` builds only the host platform and loads the image into the local image store; CI handles multi-platform publishing.

## Documentation

Update the relevant pages in `docs/` when browser routes, API behavior, configuration, CLI behavior, security, or integrations change. Update the translation guide and locale documentation when language support changes. Build commands are described in the root Makefile; there are no `make run`, `make run-dev`, or `make build-all` targets.

Validate the documentation with MkDocs and keep generated files in a project-scoped temporary directory:

```sh
temp_parent="${TMPDIR:-/tmp}/apimgr"
mkdir -p "$temp_parent"
temp_dir=$(mktemp -d "$temp_parent/gitignore-docs-XXXXXX")
trap 'rm -rf -- "$temp_dir"' EXIT
python -m mkdocs build --strict --site-dir "$temp_dir/site"
```

## Contributing

1. Work on `main` or use a temporary local branch and merge it into `main` before publishing.
2. Make the change and add or update focused tests where behavior changes.
3. Run `make test` in Docker.
4. Update the affected documentation pages.
5. Push through the repository's `gitcommit` workflow and check the resulting CI run.

Follow standard Go formatting and place comments above the code they describe. See the repository's [AI.md file](https://github.com/apimgr/gitignore/blob/main/AI.md) for the full coding requirements.
