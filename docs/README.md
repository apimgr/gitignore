# 📚 GitIgnore API Server Documentation

Welcome to the complete documentation for GitIgnore API Server.

## Quick Links

- [Main README](../README.md) - Project overview and quick start
- [API Documentation](API.md) - Complete API reference
- [Server Administration](SERVER.md) - Installation and configuration guide
- [Project Specification](../CLAUDE.md) - Detailed project specification

## Documentation Structure

### For Users

**Getting Started**:
1. Read the [Main README](../README.md) for project overview
2. Choose your installation method (Docker, binary, or source)
3. Follow the quick start guide
4. Access the web UI at `http://localhost:PORT`

**Using the API**:
1. Review [API Documentation](API.md) for endpoint reference
2. Try examples with curl, JavaScript, or Python
3. Download CLI scripts for command-line usage
4. Enable shell completion for convenience

**CLI Tools**:
- Download scripts from `/api/v1/cli/sh` (bash/zsh) or `/api/v1/cli/ps` (PowerShell)
- Install shell completion from `/api/v1/cli/completion/{bash,zsh,fish}`
- Use `gitignore list`, `gitignore search`, `gitignore <templates>`

### For Administrators

**Installation & Setup**:
1. Read [Server Administration Guide](SERVER.md)
2. Choose database backend (SQLite, MySQL, PostgreSQL)
3. Configure directories and environment variables
4. Set up systemd service (Linux) or run manually
5. Configure reverse proxy for HTTPS (optional)

**Configuration**:
- Environment variables for initial setup
- Command-line flags for runtime configuration
- Admin dashboard at `/admin` for web-based management
- API endpoints at `/api/v1/admin/*` for programmatic control

**Security**:
- Admin credentials generated on first run
- Basic Auth for web UI, Bearer tokens for API
- File permissions and access control
- Reverse proxy configuration for HTTPS
- Rate limiting via reverse proxy

**Maintenance**:
- Backup and restore procedures
- Log management and rotation
- Database maintenance
- Performance optimization
- Troubleshooting common issues

### For Developers

**Architecture**:
- Read [Project Specification](../CLAUDE.md) for complete technical details
- Single Go binary with embedded assets
- Chi v5 router for HTTP
- SQLite/MySQL/PostgreSQL for persistence
- In-memory template indexing

**Development Setup**:
```bash
git clone https://github.com/apimgr/gitignore.git
cd gitignore
make deps
make run-dev
```

**Testing**:
```bash
# Unit tests
go test ./...

# Docker tests
cd tests && ./test-docker.sh

# Incus tests
./test-incus.sh

# Benchmarks
go test -bench=. -benchmem ./...
```

**Building**:
```bash
# Current platform
make build

# All platforms
make build-all

# Docker image
make docker

# Release
make release
```

**Project Structure**:
```
.
├── src/                      # Source code
│   ├── database/             # Database abstraction
│   ├── paths/                # OS path detection
│   ├── server/               # HTTP server
│   └── templates/            # Template management
├── tests/                    # Test scripts and suites
│   ├── unit/                 # Unit tests
│   ├── test-docker.sh        # Docker integration tests
│   └── test-incus.sh         # Incus tests
├── scripts/                  # Production scripts
│   ├── install.sh            # Installation script
│   ├── backup.sh             # Backup script
│   └── update.sh             # Update script
├── docs/                     # Documentation
│   ├── README.md             # This file
│   ├── API.md                # API reference
│   └── SERVER.md             # Admin guide
├── .github/workflows/        # CI/CD workflows
├── docker-compose.yml        # Production compose
├── docker-compose.test.yml   # Test compose
├── Dockerfile                # Docker image
├── Makefile                  # Build system
└── CLAUDE.md                 # Project specification
```

## API Overview

### Public Endpoints

No authentication required:

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Health check |
| `GET /api/v1/list` | List all templates |
| `GET /api/v1/template/:name` | Get specific template |
| `GET /api/v1/search` | Search templates |
| `GET /api/v1/combine` | Combine multiple templates |
| `GET /api/v1/categories` | List categories |
| `GET /api/v1/stats` | Server statistics |
| `GET /api/v1/cli/sh` | Download shell script |
| `GET /api/v1/cli/ps` | Download PowerShell script |
| `GET /api/v1/cli/completion/{bash,zsh,fish}` | Shell completion |
| `GET /api/v1/docs/swagger` | OpenAPI spec |
| `GET /api/v1/docs/graphql` | GraphQL schema |

### Admin Endpoints

Authentication required (Bearer token or Basic Auth):

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/admin` | Admin info |
| `GET /api/v1/admin/settings` | Get settings |
| `PUT /api/v1/admin/settings` | Update settings |
| `GET /api/v1/admin/database` | Database status |
| `POST /api/v1/admin/database/test` | Test connection |
| `GET /api/v1/admin/logs` | List logs |
| `GET /api/v1/admin/logs/:type` | Get log content |
| `GET /api/v1/admin/backup` | List backups |
| `POST /api/v1/admin/backup` | Create backup |
| `DELETE /api/v1/admin/backup/:id` | Delete backup |
| `GET /api/v1/admin/healthz` | Detailed health |

### Web Routes

Browser-friendly HTML pages:

| Route | Description |
|-------|-------------|
| `GET /` | Homepage with search |
| `GET /search` | Search page |
| `GET /template/:name` | Template detail page |
| `GET /combine` | Combine templates page |
| `GET /categories` | Categories page |
| `GET /list` | List all templates page |
| `GET /stats` | Statistics page |
| `GET /cli` | CLI customization page |
| `GET /docs` | API documentation page |
| `GET /admin` | Admin dashboard (auth required) |

## Features Highlights

### Content Negotiation

Automatically detects desired format:

```bash
# Plain text (default)
curl http://localhost:8080/api/v1/template/Go

# JSON (explicit)
curl -H "Accept: application/json" http://localhost:8080/api/v1/template/Go

# JSON (via extension)
curl http://localhost:8080/api/v1/template/Go.json

# HTML (browser detection)
open http://localhost:8080/template/Go
```

### Template Combination

Smart merging with deduplication:

```bash
# Combine multiple templates
curl "http://localhost:8080/api/v1/combine?templates=Go,Python,VSCode" > .gitignore

# Result includes headers and deduplicated entries
```

### CLI Integration

Auto-configuring scripts:

```bash
# Download with defaults
curl http://localhost:8080/api/v1/cli/sh > gitignore

# Customize defaults
curl "http://localhost:8080/api/v1/cli/sh?defaults=go,linux,vscode" > gitignore

# Use it
chmod +x gitignore
./gitignore go python node    # Create .gitignore
./gitignore list               # List templates
./gitignore update             # Update script
```

### Shell Completion

Tab completion for all major shells:

```bash
# Bash
curl http://localhost:8080/api/v1/cli/completion/bash | \
  sudo tee /etc/bash_completion.d/gitignore

# Zsh
curl http://localhost:8080/api/v1/cli/completion/zsh | \
  sudo tee /usr/share/zsh/site-functions/_gitignore

# Fish
curl http://localhost:8080/api/v1/cli/completion/fish | \
  sudo tee ~/.config/fish/completions/gitignore.fish
```

## Deployment Scenarios

### 1. Personal Development Server

```bash
# Run locally
gitignore --port 8080

# Or with Docker
docker-compose up -d
```

**Access**: `http://localhost:8080`

### 2. Team Server (Internal Network)

```bash
# Install as systemd service
sudo ./scripts/install.sh
sudo systemctl enable --now gitignore

# Configure firewall
sudo ufw allow from 192.168.1.0/24 to any port 80
```

**Access**: `http://server-ip`

### 3. Public Server (with Reverse Proxy)

```nginx
# Nginx config
server {
    listen 443 ssl http2;
    server_name gitignore.example.com;

    ssl_certificate /etc/letsencrypt/live/gitignore.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/gitignore.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

**Access**: `https://gitignore.example.com`

### 4. Docker Compose Stack

```yaml
version: '3.8'
services:
  gitignore:
    image: apimgr/gitignore:latest
    ports:
      - "127.0.0.1:8080:80"
    volumes:
      - ./config:/config
      - ./data:/data
    environment:
      - ADMIN_PASSWORD=changeme
    restart: unless-stopped
```

## Common Tasks

### View Admin Credentials

```bash
# Docker
cat ./rootfs/config/admin_credentials

# System install
sudo cat /etc/gitignore/admin_credentials

# User install
cat ~/.config/gitignore/admin_credentials
```

### Update Server

```bash
# Using update script
./scripts/update.sh

# Or manually
docker-compose pull
docker-compose up -d
```

### Backup Data

```bash
# Using backup script
./scripts/backup.sh

# Or manually
tar -czf backup.tar.gz /etc/gitignore /var/lib/gitignore
```

### View Logs

```bash
# Docker
docker-compose logs -f

# Systemd
sudo journalctl -u gitignore -f

# Direct files
tail -f /var/log/gitignore/access.log
```

### Change Admin Password

```bash
# Stop server
sudo systemctl stop gitignore

# Delete database
sudo rm /var/lib/gitignore/gitignore.db

# Set new password
export ADMIN_PASSWORD=newpassword

# Start server
sudo systemctl start gitignore

# View new credentials
sudo cat /etc/gitignore/admin_credentials
```

## Support & Community

- **Issues**: Report bugs at [GitHub Issues](https://github.com/apimgr/gitignore/issues)
- **Discussions**: Ask questions at [GitHub Discussions](https://github.com/apimgr/gitignore/discussions)
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines

## License

This project is licensed under the MIT License - see [LICENSE](../LICENSE) for details.

---

**For detailed API documentation, see [API.md](API.md)**

**For server administration, see [SERVER.md](SERVER.md)**

**For project specification, see [CLAUDE.md](../CLAUDE.md)**
