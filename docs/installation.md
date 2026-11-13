# Installation

GitIgnore runs as a static server binary or as a Docker container. The template catalog and locale files are embedded in the binaries.

## Docker Compose

The repository's production Compose file uses `ghcr.io/apimgr/gitignore:latest`, publishes the service on host address `172.17.0.1:64180`, and mounts persistent configuration and data directories. Copy `docker/docker-compose.yml` to a deployment directory before starting it; the relative `volumes/` paths then remain outside the source checkout.

```sh
docker compose -f docker-compose.yml up -d
```

The container listens on port `80`. Configuration and data paths are set by the Compose environment. The health check invokes the binary's `--status` command.

## Binary

Download the artifact for your operating system and architecture from the [GitHub releases page](https://github.com/apimgr/gitignore/releases). Release builds cover Linux, macOS, Windows, and FreeBSD on amd64 and arm64. A Linux example:

```sh
chmod +x gitignore-linux-amd64
./gitignore-linux-amd64 --port 8080
```

The companion client is published separately as `gitignore-cli-{os}-{arch}` (with `.exe` on Windows). See the [CLI reference](cli.md).

## Service installation

The server binary includes service-management commands for supported operating systems. For example, on Linux:

```sh
sudo gitignore --service --install
sudo gitignore --service start
```

Use `gitignore --service --help` for supported service operations. Service installation changes host service configuration and should be performed on the intended deployment host.

## Daemon mode

On supported Unix systems, start the server detached from the terminal with `--daemon`:

```sh
gitignore --daemon --port 8080
```

Windows uses the service integration; daemon mode is not supported there.

## Configuration

The server creates its configuration file and runtime directories on startup when needed. Operators edit `server.yml`; CLI flags and environment variables can override settings. See [configuration](configuration.md) for paths, precedence, and supported settings.
