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
| `make dev` | Build server and client into a project-scoped temporary directory without release metadata. |
| `make local` | Build versioned server and client binaries for the local platform. |
| `make build` | Build local binaries and the eight supported server and client platform artifacts. |
| `make test` | Run vet, vulnerability scanning, unit tests, and the coverage gate in Docker. |
| `make release` | Build and publish the stable `release.txt` version as a GitHub release. It replaces an existing release and tag with the same version. |
| `make beta` | Create and push a timestamped beta tag from a clean `main` branch; CI publishes the beta. |
| `make docker` | Build and push the multi-architecture container image. |
| `make clean` | Remove `binaries/` and `releases/`. |

```sh
make dev    # Build server and client into a project-scoped temporary directory
make local  # Build binaries for the host OS/architecture into binaries/
make build  # Build server and client for the configured platform matrix
```

The Go toolchain runs in the Makefile's Docker image. The binaries are static (`CGO_ENABLED=0`) and contain their web assets, template data, and locale files.

## Test and security checks

```sh
make test
```

This target runs `go vet`, `govulncheck`, the Go test suite, and the coverage threshold inside the Go container. Translation-key consistency is covered by the test suite. Optional container/integration scripts are in `tests/`; consult their help or source before running them because some require Incus or a configured browser environment.

`./tests/run_tests.sh` runs the basic Docker binary-validation suite. Run `./tests/incus.sh` separately for the full systemd and OpenRC service matrix; it also checks restart persistence, overlay-provider behavior, and uninstall cleanup. The browser suite is on-demand after major frontend changes and covers server-rendered, no-JavaScript, and full-JavaScript behavior:

```sh
./tests/e2e.sh
```

## Release artifacts

```sh
make build
make release
```

`make release` requires a stable semantic version in `release.txt`. It builds the platform matrix, prepares version, source archive, and checksum assets, then creates the matching `v{version}` GitHub release as latest. If that release or tag already exists, the target deletes it before recreating it. Beta and devel releases are published by CI from their tags and commits. `make docker` builds and pushes the configured multi-architecture image.

## Documentation

Update the relevant pages in `docs/` when browser routes, API behavior, configuration, CLI behavior, security, or integrations change. Update the translation guide and locale documentation when language support changes. Build commands are described in the root Makefile; there are no `make run`, `make run-dev`, or `make build-all` targets.

Validate the documentation with `python -m mkdocs build --strict`. When running locally, pass `--site-dir` to a project-scoped temporary directory so generated site files do not enter the repository.

## Contributing

1. Work on `main` or use a temporary local branch and merge it into `main` before publishing.
2. Make the change and add or update focused tests where behavior changes.
3. Run `make test` in Docker.
4. Update the affected documentation pages.
5. Push through the repository's `gitcommit` workflow and check the resulting CI run.

Follow standard Go formatting and place comments above the code they describe. See the repository's [AI.md file](https://github.com/apimgr/gitignore/blob/main/AI.md) for the full coding requirements.
