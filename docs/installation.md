# Installation

GitIgnore runs as a static server binary or as a Docker container. The template catalog and locale files are embedded in the binaries.

## Docker Compose

The repository's production Compose file runs `ghcr.io/apimgr/gitignore:latest` with a Valkey cache, publishes the service on host address `172.17.0.1:64180`, and mounts persistent configuration and data directories. Valkey data is stored under `volumes/data/db/valkey`. Copy `docker/docker-compose.yml` to a deployment directory before starting it; the relative `volumes/` paths then remain outside the source checkout.

```sh
cp /path/to/gitignore/docker/docker-compose.yml ./docker-compose.yml
docker compose -f docker-compose.yml up -d
```

The container listens on port `80`. Configuration and data paths are set by the Compose environment. The health check invokes the binary's `--status` command.

## Binary

Download the matching server or client artifact from [GitHub Releases](https://github.com/apimgr/gitignore/releases/latest). The build matrix includes both binaries for every listed target:

| Platform | Architecture | Server | Client |
|---|---|---|---|
| Linux | amd64 | `gitignore-linux-amd64` | `gitignore-cli-linux-amd64` |
| Linux | arm64 | `gitignore-linux-arm64` | `gitignore-cli-linux-arm64` |
| macOS | amd64 | `gitignore-darwin-amd64` | `gitignore-cli-darwin-amd64` |
| macOS | arm64 | `gitignore-darwin-arm64` | `gitignore-cli-darwin-arm64` |
| Windows | amd64 | `gitignore-windows-amd64.exe` | `gitignore-cli-windows-amd64.exe` |
| Windows | arm64 | `gitignore-windows-arm64.exe` | `gitignore-cli-windows-arm64.exe` |
| FreeBSD | amd64 | `gitignore-freebsd-amd64` | `gitignore-cli-freebsd-amd64` |
| FreeBSD | arm64 | `gitignore-freebsd-arm64` | `gitignore-cli-freebsd-arm64` |

For Linux amd64, install the server with:

```sh
curl -LSsf https://github.com/apimgr/gitignore/releases/latest/download/gitignore-linux-amd64 \
  -o /usr/local/bin/gitignore && chmod +x /usr/local/bin/gitignore
gitignore --port 8080
```

For macOS, select the architecture automatically and remove the quarantine attribute after download:

```sh
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/;s/aarch64/arm64/')
curl -LSsf "https://github.com/apimgr/gitignore/releases/latest/download/gitignore-darwin-${ARCH}" \
  -o /usr/local/bin/gitignore && chmod +x /usr/local/bin/gitignore
xattr -d com.apple.quarantine /usr/local/bin/gitignore 2>/dev/null || true
```

On Windows, download the matching `.exe` and add its directory to `%PATH%`. On FreeBSD, download the matching artifact, run `chmod +x`, and add it to `PATH`.

The companion client is available in the same release under the `gitignore-cli-` artifact names. For example:

```sh
curl -LSsf https://github.com/apimgr/gitignore/releases/latest/download/gitignore-cli-linux-amd64 \
  -o /usr/local/bin/gitignore-cli && chmod +x /usr/local/bin/gitignore-cli
```

See the [CLI reference](cli.md) for client commands.

## Service installation

The server binary includes service-management commands for supported operating systems. For example, on Linux:

```sh
sudo gitignore --service --install
sudo gitignore --service start
```

Use `gitignore --service --help` for supported service operations. Service installation changes host service configuration and should be performed on the intended deployment host.

On Unix, a root startup creates the fixed `gitignore` service account if it is missing, assigns it ownership of runtime data, binds the listener, and then drops privileges. A non-root startup does not create an account and keeps the current user.

## Daemon mode

On supported Unix systems, start the server detached from the terminal with `--daemon`:

```sh
gitignore --daemon --port 8080
```

Windows uses the service integration; daemon mode is not supported there.

## Configuration

The server creates its configuration file and runtime directories on startup when needed. Operators edit `server.yml`; CLI flags and environment variables can override settings. See [configuration](configuration.md) for paths, precedence, and supported settings.
