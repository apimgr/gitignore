# Development Guide

## Prerequisites

- Go (latest stable; never pin a specific version)
- Make
- Docker (all builds and tests run inside the `casjaysdev/go:latest` toolchain
  image; nothing is built on the host)

## Layout

- `src/` - server source (package `main`)
- `src/client/` - companion CLI client
- `src/server/` - HTTP server, routes, and handlers
- `src/config/`, `src/db/`, `src/path/` - configuration, storage, and paths
- `docs/` - this documentation set (ReadTheDocs / MkDocs)

## Build

```bash
git clone https://github.com/apimgr/gitignore
cd gitignore
make build
```

Binaries are written to `binaries/`. Builds are static (`CGO_ENABLED=0`).

## Run Locally

```bash
make run          # build and run on :8080
make run-dev      # run from source in dev mode
```

## Testing

```bash
make test
```

`make test` validates translation files, runs `go vet ./...`, and executes the
test suite inside Docker.

## Cross-Platform Builds

```bash
make build-all    # all supported OS/arch pairs
make release      # build-all plus packaged release artifacts
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes and add tests for new behavior
4. Run `make test`
5. Update the relevant `docs/` pages
6. Submit a pull request

## Code Style

- Follow standard Go formatting (`gofmt`)
- Keep comments above the code they describe, never inline
- Add a test that fails before and passes after any behavior change
