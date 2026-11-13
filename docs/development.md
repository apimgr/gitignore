# Development Guide

## Prerequisites

- Git and Make
- Docker with access to the `casjaysdev/go:latest` image

The Makefile runs Go builds, tests, and static checks in Docker. Do not run Go build or test commands directly on the host; see the repository's [AI.md](../AI.md) for the complete development rules.

## Layout

- `src/` — server and shared Go packages
- `src/client/` — companion CLI and terminal interface
- `src/server/` — HTTP routes, handlers, templates, and embedded assets
- `src/common/i18n/locales/` — embedded locale JSON files
- `cmd/i18n-validate/` — translation consistency validator
- `docker/` — Dockerfiles and Compose definitions
- `docs/` — MkDocs and ReadTheDocs documentation
- `tests/` — Docker and optional integration/browser test runners

## Build

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

## Release artifacts

```sh
make build
make release
```

`make release` builds the configured platform matrix and writes version, source archive, and checksum artifacts under `releases/`. `make docker` builds and pushes the configured multi-architecture image, so use it only when publishing is intended.

## Documentation

Update the relevant pages in `docs/` when browser routes, API behavior, configuration, CLI behavior, security, or integrations change. Update the translation guide and locale documentation when language support changes. Build commands are described in the root Makefile; there are no `make run`, `make run-dev`, or `make build-all` targets.

## Contributing

1. Create a feature branch.
2. Make the change and add or update focused tests where behavior changes.
3. Run `make test` in Docker.
4. Update the affected documentation pages.
5. Submit a pull request with a concise description of behavior and verification.

Follow standard Go formatting and place comments above the code they describe. See [AI.md](../AI.md) for the repository's full coding requirements.
