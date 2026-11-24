# GitIgnore API

[![CI](https://github.com/apimgr/gitignore/actions/workflows/ci.yml/badge.svg)](https://github.com/apimgr/gitignore/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/apimgr/gitignore)](https://github.com/apimgr/gitignore/releases)
[![License](https://img.shields.io/github/license/apimgr/gitignore)](LICENSE.md)
[![Docs](https://readthedocs.org/projects/apimgr-gitignore/badge/?version=latest)](https://apimgr-gitignore.readthedocs.io)

## About

GitIgnore serves a curated, embedded catalog of `.gitignore` templates through a public REST API, read-only GraphQL API, browser interface, and companion `gitignore-cli`. The Go server builds as a self-contained static binary or runs in Docker.

## Features

- Browse, search, preview, and combine templates in the server-rendered web interface.
- Retrieve templates, categories, and statistics through REST or GraphQL.
- Use unversioned routes compatible with the gitignore.io API.
- Connect AI clients through the stateless, read-only MCP endpoint. MCP requires a valid owner or operator Bearer token; use a separate linked token for each client.
- Run the companion CLI in shell pipelines and generate shell helpers and completions.
- Publish public `security.txt` and generated `llms.txt` discovery documents.
- Serve a public sitemap and valid empty Atom/RSS feeds; the embedded catalog has no timestamped public item pages.
- Use public About, Help, Privacy, Terms, Security, DPO, Preferences, and health pages. Contact reporting is linked from the Security page and security.txt; vulnerability reports return a tracking ID with a private status link usable once per UTC day.
- Synchronize theme and language preferences between devices without exporting consent or credentials.
- Choose from English, Arabic, German, Spanish, French, Italian, Japanese, and Chinese interfaces.

The catalog API is public and read-only. There are no user accounts, admin web panel, or user-submitted templates. See [IDEA.md](IDEA.md) for product scope.

## Production

Download the latest server and client artifacts from [GitHub Releases](https://github.com/apimgr/gitignore/releases/latest).

| Platform | Architectures | Server artifact | Client artifact |
|---|---|---|---|
| Linux | amd64, arm64 | `gitignore-linux-{amd64,arm64}` | `gitignore-cli-linux-{amd64,arm64}` |
| macOS | amd64, arm64 | `gitignore-darwin-{amd64,arm64}` | `gitignore-cli-darwin-{amd64,arm64}` |
| Windows | amd64, arm64 | `gitignore-windows-{amd64,arm64}.exe` | `gitignore-cli-windows-{amd64,arm64}.exe` |
| FreeBSD | amd64, arm64 | `gitignore-freebsd-{amd64,arm64}` | `gitignore-cli-freebsd-{amd64,arm64}` |

On Linux, download the matching artifact and install it with:

```sh
curl -LSsf https://github.com/apimgr/gitignore/releases/latest/download/gitignore-linux-amd64 \
  -o /usr/local/bin/gitignore && chmod +x /usr/local/bin/gitignore
```

On macOS, replace `gitignore` with the selected server or client artifact name, then remove the quarantine attribute after download:

```sh
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/;s/aarch64/arm64/')
curl -LSsf "https://github.com/apimgr/gitignore/releases/latest/download/gitignore-darwin-${ARCH}" \
  -o /usr/local/bin/gitignore && chmod +x /usr/local/bin/gitignore
xattr -d com.apple.quarantine /usr/local/bin/gitignore 2>/dev/null || true
```

Windows users should download the matching `.exe` and add its directory to `%PATH%`. FreeBSD users should download the matching artifact, make it executable with `chmod +x`, and place it on their `PATH`.

### Docker

```sh
docker run -d \
  --name gitignore \
  -p 172.17.0.1:64180:80 \
  -v ./volumes/config:/config:z \
  -v ./volumes/data:/data:z \
  ghcr.io/apimgr/gitignore:latest
```

The container stores configuration and data in the mounted directories. Use a deployment directory for these paths, outside the source checkout.

### Docker Compose

The production Compose file listens on `172.17.0.1:64180`. Copy it to a deployment directory before starting so persistent configuration and data stay outside the source checkout.

```sh
docker compose -f /path/to/deployment/docker-compose.yml up -d
```

See [Installation](docs/installation.md) and [Configuration](docs/configuration.md) for deployment and settings.

### Binary and service

Download the server artifact for Linux, macOS, Windows, or FreeBSD (amd64 or arm64) from [GitHub Releases](https://github.com/apimgr/gitignore/releases). Example:

```sh
chmod +x gitignore-linux-amd64
./gitignore-linux-amd64
```

On supported systems, install and manage the service with the server binary:

```sh
sudo gitignore --service --install
sudo gitignore --service start
```

Use `gitignore --service --help` for platform-specific operations.

## Client

Download the `gitignore-cli-{os}-{arch}` release for your platform. Template names can be composed directly into a file:

```sh
gitignore-cli Go Node > .gitignore
```

The client also supports `list`, `search`, `categories`, `category`, `get`, `combine`, and `stats`. See the [CLI reference](docs/cli.md) for options and shell integration.

## Configuration

On first startup, the server creates its configuration and required runtime directories. Edit `{config_dir}/server.yml` and supported area drop-ins under `{config_dir}/yaml.d/`; sensitive settings are stored in `{config_dir}/secrets.yml`. Area files are created when needed; built-in defaults apply to settings that are not explicitly saved.

Key settings include the API version and URL prefix, trusted proxies, rate limits, GeoIP, blocklists, CVE/Trivy data, contact and CAPTCHA behavior, metrics, and optional Tor/I2P services. See [Configuration](docs/configuration.md) for the setting map, environment variables, and precedence.

## API

The default versioned API base is `/api/v1`; operators may configure the version and URL prefix. Use `/api/autodiscover` to obtain the effective API base.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/list` | List template names |
| `GET /api/v1/templates/{name}` | Retrieve a template |
| `GET /api/v1/search?q={query}` | Search names and content |
| `GET /api/v1/combine?templates={names}` | Compose comma-separated templates |
| `GET /api/v1/categories` | List template categories |
| `GET /api/v1/stats` | Catalog statistics |
| `POST /api/v1/server/graphql` | Read-only GraphQL query |
| `POST /api/v1/server/mcp` | Stateless MCP; Bearer token required |
| `GET /api/v1/server/healthz` | Health status |
| `GET /api/v1/server/swagger` | OpenAPI document |

The GraphQL playground is `/server/docs/graphql`; Swagger UI is `/server/docs/swagger`. MCP aliases are `/api/mcp` and `/mcp`. Existing gitignore.io clients can use `/api/list` and `/api/{template-names}`. See the [API reference](docs/api.md) for response formats and complete routes.

## Other

- [Security and coordinated disclosure](docs/security.md)
- [Integrations and discovery](docs/integrations.md)
- [Server operations and public pages](docs/SERVER.md)
- [Troubleshooting and configuration](docs/configuration.md)
- [Translation guide](docs/translating.md)

## Development

Build, test, and static analysis use the `casjaysdev/go:latest` Docker toolchain through the project Makefile. The project test gate is:

```sh
make test
```

Build the server and client for supported platforms with:

```sh
make build
```

See [Development](docs/development.md) for repository layout and contribution guidance. AI.md defines the project implementation and verification requirements.

## Disclaimer

This software is provided “as is” without warranty of any kind. Evaluate it before using it in production.

- **No warranty:** The maintainers are not responsible for damages, data loss, or other issues arising from use of the software.
- **No professional advice:** The software does not provide legal, financial, medical, or other professional advice.
- **Third-party services:** External APIs and services are governed by their own terms.
- **Security:** The project follows security practices, but no software can be guaranteed free of vulnerabilities.

## License

The application is licensed under MIT; see [LICENSE.md](LICENSE.md). The embedded template dataset may have additional license requirements described there.
