# 🚫 GitIgnore API Server

> A comprehensive, self-hosted .gitignore template API with beautiful web interface and CLI tools

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-ready-blue?style=flat&logo=docker)](https://hub.docker.com/r/apimgr/gitignore)

## ✨ Features

- **500+ Templates**: Comprehensive collection of .gitignore templates for languages, frameworks, IDEs, and tools
- **RESTful API**: Clean, well-documented API with Swagger/OpenAPI support
- **GraphQL**: Query templates with GraphQL for flexible data retrieval
- **Web Interface**: Beautiful, responsive web UI with Dracula theme
- **CLI Scripts**: Auto-configuring shell scripts for bash, zsh, fish, and PowerShell
- **Shell Completion**: Tab completion support for all major shells
- **Content Negotiation**: Automatic format detection (JSON, plain text, HTML)
- **Template Combination**: Merge multiple templates with smart deduplication
- **Fast Search**: In-memory indexing for instant template lookups
- **Single Binary**: Self-contained executable with embedded assets
- **Multi-Database**: Support for SQLite, MySQL, PostgreSQL
- **Admin Dashboard**: Protected configuration interface
- **Docker Ready**: Official Docker images and docker-compose files
- **Cross-Platform**: Linux, macOS, Windows, FreeBSD support

## 🚀 Quick Start

### Using Docker (Recommended)

```bash
# Download docker-compose.yml
curl -LSs https://raw.githubusercontent.com/apimgr/gitignore/main/docker-compose.yml -o docker-compose.yml

# Start the service
docker-compose up -d

# View admin credentials
cat ./rootfs/config/admin_credentials

# Access the service
open http://localhost:64080
```

### Using Binary

```bash
# Download and install
curl -LSs https://raw.githubusercontent.com/apimgr/gitignore/main/scripts/install.sh | bash

# Run the service
gitignore

# Or as systemd service
sudo systemctl enable --now gitignore
```

### Using Go

```bash
# Clone and build
git clone https://github.com/apimgr/gitignore.git
cd gitignore
make build

# Run
./binaries/gitignore
```

## 📖 Usage

### Web Interface

Navigate to `http://localhost:PORT` in your browser to:
- Search and browse templates
- Combine multiple templates
- Preview template contents
- Download templates
- Access API documentation

### API Examples

```bash
# List all templates
curl http://localhost:8080/api/v1/list

# Get a specific template
curl http://localhost:8080/api/v1/template/Go

# Search templates
curl http://localhost:8080/api/v1/search?q=python

# Combine templates
curl http://localhost:8080/api/v1/combine?templates=Go,Python,VSCode

# Get as JSON
curl -H "Accept: application/json" http://localhost:8080/api/v1/template/Go
```

### CLI Script

```bash
# Download CLI script
curl http://localhost:8080/api/v1/cli/sh > gitignore
chmod +x gitignore
sudo mv gitignore /usr/local/bin/

# Use it
gitignore go linux vscode       # Create .gitignore with templates
gitignore list                   # List all templates
gitignore search python          # Search for templates
gitignore update                 # Update the script
```

### Shell Completion

```bash
# Bash
curl http://localhost:8080/api/v1/cli/completion/bash > /etc/bash_completion.d/gitignore

# Zsh
curl http://localhost:8080/api/v1/cli/completion/zsh > ~/.oh-my-zsh/completions/_gitignore

# Fish
curl http://localhost:8080/api/v1/cli/completion/fish > ~/.config/fish/completions/gitignore.fish
```

## 🔧 Configuration

### Environment Variables

```bash
# Server configuration
PORT=8080                        # HTTP port (default: random 64000-64999)
ADDRESS=0.0.0.0                  # Listen address

# Directories
CONFIG_DIR=/etc/gitignore        # Configuration directory
DATA_DIR=/var/lib/gitignore      # Data directory
LOGS_DIR=/var/log/gitignore      # Logs directory

# Database
DB_TYPE=sqlite                   # Database type (sqlite, mysql, postgres)
DB_PATH=/data/gitignore.db       # SQLite database path
DATABASE_URL=                    # Full database connection string

# Admin credentials (first run only)
ADMIN_USER=administrator         # Admin username
ADMIN_PASSWORD=                  # Admin password (random if not set)
ADMIN_TOKEN=                     # API token (random if not set)
```

### Command-Line Flags

```bash
gitignore [flags]

Flags:
  --config DIR      Configuration directory
  --data DIR        Data directory
  --logs DIR        Logs directory
  --port PORT       HTTP port
  --address ADDR    Listen address
  --db-type TYPE    Database type (sqlite, mysql, postgres)
  --db-path PATH    SQLite database path
  --db-url URL      Database connection string
  --dev             Enable development mode
  --version         Show version
  --help            Show help
```

## 🔐 Admin Access

Admin credentials are generated on first run and saved to `{CONFIG_DIR}/admin_credentials`.

**Web UI**: Navigate to `/admin` and login with username/password

**API**: Use Bearer token in Authorization header:
```bash
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/api/v1/admin/settings
```

## 📚 Documentation

- [API Documentation](docs/API.md) - Complete API reference
- [Server Administration](docs/SERVER.md) - Setup and configuration guide
- [Docker Deployment](docs/DOCKER.md) - Docker and docker-compose guide

## 🧪 Testing

```bash
# Run all tests
make test

# Test with Docker
cd tests
./test-docker.sh

# Test with Incus
./test-incus.sh

# Run benchmarks
go test -bench=. -benchmem ./...
```

## 🛠️ Development

```bash
# Install dependencies
make deps

# Run in development mode
make run-dev

# Build for all platforms
make build-all

# Create release
make release

# Clean build artifacts
make clean
```

## 📦 Installation

### System-wide (requires root)

```bash
sudo ./scripts/install.sh
sudo systemctl enable --now gitignore
```

### User installation

```bash
./scripts/install.sh
gitignore
```

### Update

```bash
./scripts/update.sh
```

### Backup

```bash
./scripts/backup.sh
```

## 🐳 Docker

### Production

```bash
# Using docker-compose (recommended)
docker-compose up -d

# Manual docker run
docker run -d \
  --name gitignore \
  -p 127.0.0.1:64080:80 \
  -v $(pwd)/rootfs/config:/config \
  -v $(pwd)/rootfs/data:/data \
  -v $(pwd)/rootfs/logs:/logs \
  -e ADMIN_PASSWORD=changeme \
  apimgr/gitignore:latest
```

### Testing

```bash
# Use test compose file
docker-compose -f docker-compose.test.yml up -d

# Or run test script
cd tests
./test-docker.sh
```

## 🏗️ Architecture

```
┌─────────────────────────────────────────┐
│         Single Go Binary                │
│  ┌─────────────────────────────────┐   │
│  │  Embedded Assets (go:embed)     │   │
│  │  • .gitignore templates (500+)  │   │
│  │  • HTML templates               │   │
│  │  • CSS/JS/Images                │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │  HTTP Server (Chi Router)       │   │
│  │  • Public routes                │   │
│  │  • Admin routes (protected)     │   │
│  │  • API v1 endpoints             │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │  Database (SQLite/MySQL/Pg)     │   │
│  │  • Admin credentials (hashed)   │   │
│  │  • Server settings              │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- .gitignore templates sourced from [github/gitignore](https://github.com/github/gitignore)
- Inspired by [gitignore.io](https://gitignore.io)
- Built with [Chi Router](https://github.com/go-chi/chi)
- UI styled with [Dracula Theme](https://draculatheme.com)

## 📞 Support

- **Issues**: [GitHub Issues](https://github.com/apimgr/gitignore/issues)
- **Discussions**: [GitHub Discussions](https://github.com/apimgr/gitignore/discussions)
- **Documentation**: [docs/](docs/)

---

**Made with ❤️ by the API Manager team**
