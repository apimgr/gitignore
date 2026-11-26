# Contributing

## Development setup

Clone the repository and use the Makefile with Docker. Go builds, tests, and
vulnerability checks run in the `casjaysdev/go:latest` container; do not run Go
build or test commands directly on the host.

```sh
make dev
make test
make build
```

`make build` writes ignored artifacts under `binaries/`. For documentation,
follow the project-scoped temporary output instructions in
[`docs/development.md`](../docs/development.md). That guide also covers the
container, integration, browser, and documentation workflows.

## Changes and pull requests

Open pull requests against `main`. Keep changes focused, add or update tests
when behavior changes, and update the affected README, ReadTheDocs, API, or
configuration documentation. Include test evidence, documentation/config
impact, breaking-change notes, and security/privacy impact in the pull request.

Before submitting, run `make test` and the relevant binary, browser, or
container checks described in `docs/development.md`. Do not introduce
placeholder, stub, or unfinished TODO behavior.

## Security reports

Do not report vulnerabilities in a public issue or pull request. Use
[GitHub private vulnerability reporting](https://github.com/apimgr/gitignore/security/advisories/new).
For a vulnerability in a deployed instance, follow the instructions at
`/server/security` or `/.well-known/security.txt`; the deployed report form is
available at `/server/contact?security_id=...`.
