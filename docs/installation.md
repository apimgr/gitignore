# Installation

## Docker (Recommended)

```bash
docker run -d \
  --name gitignore \
  -p 127.0.0.1:8080:8080 \
  -v gitignore-config:/config \
  -v gitignore-data:/data \
  ghcr.io/apimgr/gitignore:latest
```

The container listens on port 8080 by default. Templates are embedded in the
binary, so no external template volume is required.

## Binary

Download the matching artifact from
[releases](https://github.com/apimgr/gitignore/releases):

```bash
# Linux AMD64
wget https://github.com/apimgr/gitignore/releases/latest/download/gitignore-linux-amd64
chmod +x gitignore-linux-amd64
./gitignore-linux-amd64 --port 8080
```

Builds are published for linux, darwin, windows, and freebsd on both amd64 and
arm64.

## Systemd Service

The server can install and manage its own systemd unit:

```bash
sudo ./gitignore --service --install
sudo systemctl start gitignore
sudo systemctl enable gitignore
```

## Running as a Daemon

On Unix platforms the server can detach from the terminal:

```bash
./gitignore --daemon --port 8080 --pid /var/run/gitignore.pid
```

On Windows use `--service --install` instead; `--daemon` is not supported there.

## Configuration

See [Configuration](configuration.md) for all options and directory locations.
