# GitIgnore API

A Go application that serves a curated collection of `.gitignore` templates through a versioned REST API, GraphQL, a server rendered web interface, and a companion CLI. Templates are embedded in the application binary and the public API requires no account or API key.

## Features

- Browse, search, preview, and combine templates in a responsive web interface.
- Retrieve templates, lists, categories, and statistics through REST and GraphQL.
- Use the unversioned routes compatible with the gitignore.io API.
- Install the Go CLI for shell pipelines and use generated shell completions.
- Run the application as a self contained binary or container.

## Run with Docker

```sh
docker compose -f docker/docker-compose.yml up -d
```

The web interface listens on the port configured in the compose file. Configuration is supplied through `server.yml`; see [configuration documentation](docs/configuration.md).

## Build and run

Build the server and CLI with the project Makefile:

```sh
make build
```

The release binaries are written to `binaries/`. Run the server with:

```sh
./binaries/gitignore --help
./binaries/gitignore
```

Use the CLI to compose templates into a `.gitignore` file:

```sh
./binaries/gitignore-cli Go Node > .gitignore
```

## API

The versioned API is rooted at `/api/v1` by default. Examples:

```sh
# List available templates
curl http://localhost:64080/api/v1/list

# Retrieve a template
curl http://localhost:64080/api/v1/templates/Go

# Search templates
curl 'http://localhost:64080/api/v1/search?q=python'

# Combine templates
curl 'http://localhost:64080/api/v1/combine?templates=Go,Python,VSCode'

# View the OpenAPI document
curl http://localhost:64080/api/v1/server/swagger
```

GraphQL is available at `/graphql` and `/api/graphql` (with versioned aliases); its interactive browser page is `/server/docs/graphql`. The API is public and read only. Compatibility routes for existing gitignore.io clients are also served at `/api/list` and `/api/{template-names}`.

## Configuration and documentation

Operators configure the service in `server.yml`. There are no user accounts, admin dashboard, or API credentials. See [configuration](docs/configuration.md), [API documentation](docs/API.md), and [server documentation](docs/SERVER.md).

## Development

Go builds and tests run in Docker through the Makefile:

```sh
make dev
make test
```

Optional integration and browser beta tests are available in `tests/`; the browser suite is run on demand with `./tests/e2e.sh`.

## License

This project is licensed under the MIT License; see [LICENSE.md](LICENSE.md).
