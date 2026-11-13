# GitIgnore API

GitIgnore serves an embedded catalog of `.gitignore` templates through a public REST API, a read-only GraphQL API, a server-rendered browser interface, and the `gitignore-cli` companion client. It runs as a self-contained Go binary or Docker container.

## Features

- Browse, search, preview, and combine templates in the web interface.
- Retrieve templates, categories, and statistics through REST or GraphQL.
- Use the unversioned routes compatible with the gitignore.io API.
- Run the companion CLI in shell pipelines and download generated shell helpers and completions.
- Use the public About, Help, Privacy, Contact, Terms, Security, and health pages.
- Choose from eight bundled interface languages: English, Arabic, German, Spanish, French, Italian, Japanese, and Chinese.

The service is public and read-only. It has no user accounts, admin web panel, or user-submitted templates. See [IDEA.md](IDEA.md) for product scope.

## Run with Docker

The production Compose file publishes the service on `172.17.0.1:64180`. Copy it to a deployment directory before starting it so persistent configuration and data stay outside the source checkout.

```sh
docker compose -f /path/to/deployment/docker-compose.yml up -d
```

See [installation](docs/installation.md) and [configuration](docs/configuration.md) for deployment details.

## Build and run

Build the server and CLI for the supported platforms with the project Makefile. Builds and tests run in the `casjaysdev/go:latest` Docker image.

```sh
make build
```

Run the generated server binary in the supported runtime environment:

```sh
./binaries/gitignore-linux-amd64 --help
./binaries/gitignore-linux-amd64
```

Use the CLI to write one or more templates to a `.gitignore` file:

```sh
./binaries/gitignore-cli-linux-amd64 Go Node > .gitignore
```

## API

The versioned API is rooted at `/api/v1` by default:

```sh
# List templates (Compose publishes on this host address)
curl http://172.17.0.1:64180/api/v1/list

# Retrieve a template
curl http://172.17.0.1:64180/api/v1/templates/Go

# Search and combine templates
curl 'http://172.17.0.1:64180/api/v1/search?q=python'
curl 'http://172.17.0.1:64180/api/v1/combine?templates=Go,Python,VSCode'

# OpenAPI document
curl http://172.17.0.1:64180/api/v1/server/swagger
```

GraphQL is available at `/graphql`, `/api/graphql`, `/api/v1/graphql`, and `/api/v1/server/graphql`; its browser playground is `/server/docs/graphql`. The interactive OpenAPI UI is `/server/docs/swagger`. Existing gitignore.io clients can use `/api/list` and `/api/{template-names}`. See [API documentation](docs/api.md) for routes and response formats.

## Documentation

- [Installation](docs/installation.md)
- [Configuration](docs/configuration.md)
- [API reference](docs/api.md)
- [CLI reference](docs/cli.md)
- [Security](docs/security.md)
- [Integrations and discovery](docs/integrations.md)
- [Development](docs/development.md)
- [Translation guide](docs/translating.md)

## License

The application is licensed under MIT; see [LICENSE.md](LICENSE.md). The embedded template dataset may include content with additional license requirements described in that file.
