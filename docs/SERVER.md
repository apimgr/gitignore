# 🖥️ GitIgnore Server Administration Guide

Complete guide for installing, configuring, and managing GitIgnore API Server.

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Database](#database)
- [Logging](#logging)
- [Backup & Restore](#backup--restore)
- [Systemd Service](#systemd-service)
- [Reverse Proxy](#reverse-proxy)
- [Security](#security)
- [Troubleshooting](#troubleshooting)

---

## Installation

### Method 1: Docker (Recommended)

```bash
# Download docker-compose.yml
curl -LSs https://raw.githubusercontent.com/apimgr/gitignore/main/docker-compose.yml \
  -o docker-compose.yml

# Create directories
mkdir -p rootfs/{config,data,logs}

# Start service
docker-compose up -d

# View logs
docker-compose logs -f

# View admin credentials
cat rootfs/config/admin_credentials
```

### Method 2: Installation Script

```bash
# Download and run installer
curl -LSs https://raw.githubusercontent.com/apimgr/gitignore/main/scripts/install.sh | sudo bash

# Or download binary manually
VERSION=v1.0.0
OS=linux  # linux, darwin, windows, freebsd
ARCH=amd64  # amd64, arm64

curl -LSs https://github.com/apimgr/gitignore/releases/download/${VERSION}/gitignore-${OS}-${ARCH} \
  -o /usr/local/bin/gitignore

chmod +x /usr/local/bin/gitignore
```

### Method 3: Build from Source

```bash
# Clone repository
git clone https://github.com/apimgr/gitignore.git
cd gitignore

# Install dependencies
make deps

# Build binary
make build

# Install (optional)
sudo cp binaries/gitignore /usr/local/bin/
```

---

## Configuration

### Directory Structure

The server uses OS-specific directories:

#### Linux/BSD (with root)
```
/etc/gitignore/          # Configuration
/var/lib/gitignore/      # Data (database)
/var/log/gitignore/      # Logs
/run/gitignore/          # Runtime files (PID, socket)
```

#### Linux/BSD (without root)
```
~/.config/gitignore/         # Configuration
~/.local/share/gitignore/    # Data
~/.local/state/gitignore/    # Logs
~/.local/run/gitignore/      # Runtime files
```

#### macOS (with privileges)
```
/Library/Application Support/GitIgnore/        # Configuration & Data
/Library/Logs/GitIgnore/                       # Logs
/var/run/gitignore/                            # Runtime files
```

#### macOS (without privileges)
```
~/Library/Application Support/GitIgnore/       # Configuration & Data
~/Library/Logs/GitIgnore/                      # Logs
~/Library/Application Support/GitIgnore/run/   # Runtime files
```

#### Windows
```
C:\ProgramData\GitIgnore\config\    # Configuration
C:\ProgramData\GitIgnore\data\      # Data
C:\ProgramData\GitIgnore\logs\      # Logs
```

#### Docker
```
/config      # Configuration (mount to ./rootfs/config)
/data        # Data (mount to ./rootfs/data)
/logs        # Logs (mount to ./rootfs/logs)
```

### Environment Variables

```bash
# Server
PORT=8080                      # HTTP port (default: random 64000-64999)
ADDRESS=0.0.0.0                # Listen address (default: 0.0.0.0)

# Directories
CONFIG_DIR=/etc/gitignore      # Override config directory
DATA_DIR=/var/lib/gitignore    # Override data directory
LOGS_DIR=/var/log/gitignore    # Override logs directory

# Database
DB_TYPE=sqlite                 # Database type: sqlite, mysql, postgres
DB_PATH=/data/gitignore.db     # SQLite database path
DATABASE_URL=                  # Full connection string

# PostgreSQL example
DATABASE_URL=postgres://user:pass@localhost:5432/gitignore

# MySQL example
DATABASE_URL=mysql://user:pass@localhost:3306/gitignore

# Admin (first run only)
ADMIN_USER=administrator       # Admin username
ADMIN_PASSWORD=secure123       # Admin password (random if not set)
ADMIN_TOKEN=abc123...          # API token (random if not set)

# Development
DEV=false                      # Enable development mode
```

### Command-Line Flags

```bash
gitignore [flags]

Server:
  --port PORT          HTTP port (default: random 64000-64999)
  --address ADDR       Listen address (default: 0.0.0.0)

Directories:
  --config DIR         Configuration directory
  --data DIR           Data directory
  --logs DIR           Logs directory

Database:
  --db-type TYPE       Database type (sqlite, mysql, postgres)
  --db-path PATH       SQLite database path
  --db-url URL         Database connection string

Development:
  --dev                Enable development mode

Information:
  --version            Show version
  --status             Check if server is running
  --help               Show help
```

### Settings File

Settings are stored in the database, but can be configured via environment or API.

**Default Settings**:

```yaml
server:
  title: "GitIgnore API"
  address: "0.0.0.0"
  port: 64000-64999 (random)

database:
  type: "sqlite"
  path: "{DATA_DIR}/gitignore.db"

logging:
  level: "info"           # debug, info, warn, error
  format: "json"          # json, text
  access: true            # Log HTTP access
  rotate: true            # Rotate logs
  max_size_mb: 100        # Max log file size
  max_age_days: 30        # Max log age

templates:
  cache: true             # Cache templates in memory
  auto_update: false      # Auto-update from GitHub

security:
  cors_enabled: false     # Enable CORS
  cors_origins: []        # Allowed origins
  rate_limit: 0           # Requests per minute (0 = unlimited)
```

---

## Authentication

### First Run

On first startup, the server generates admin credentials:

1. **Checks** if credentials exist in database
2. **Generates** random password (16 chars) and token (64 hex chars) if not set
3. **Saves** to database (SHA-256 hashed)
4. **Writes** to `{CONFIG_DIR}/admin_credentials` (permissions: 0600)
5. **Displays** in console output

**Credential File Example**:

```
====================================
GITIGNORE API - ADMIN CREDENTIALS
====================================
WEB UI LOGIN:
  URL:      http://server:port/admin
  Username: administrator
  Password: R4nd0mP4ssw0rd!

API TOKEN:
  Header:   Authorization: Bearer <token>
  Token:    abc123def456...

Created: 2024-01-01 12:00:00
====================================
⚠️  SAVE THESE CREDENTIALS SECURELY
⚠️  This message is shown once only
====================================
```

### Authentication Methods

#### 1. Web UI (Basic Auth)

Navigate to `/admin` in your browser:

```bash
# Browser will prompt for credentials
open http://localhost:8080/admin

# Or use curl
curl -u administrator:password http://localhost:8080/admin
```

#### 2. API (Bearer Token)

Use the token for programmatic access:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8080/api/v1/admin/settings
```

### Changing Credentials

To change admin credentials:

**Option 1: Delete database and restart**

```bash
# Stop server
sudo systemctl stop gitignore

# Delete database
sudo rm /var/lib/gitignore/gitignore.db

# Set new credentials
export ADMIN_USER=newadmin
export ADMIN_PASSWORD=newpassword

# Restart server
sudo systemctl start gitignore
```

**Option 2: Use SQL directly** (advanced)

```bash
# Connect to database
sqlite3 /var/lib/gitignore/gitignore.db

# Update credentials (use SHA-256 hashes)
UPDATE admin_credentials SET
  username = 'newuser',
  password_hash = '<sha256_hash>',
  token_hash = '<sha256_hash>'
WHERE id = 1;
```

---

## Database

### SQLite (Default)

SQLite requires no setup - a database file is created automatically.

```bash
# Location
/var/lib/gitignore/gitignore.db

# Or custom path
gitignore --db-path /custom/path/gitignore.db
```

**Advantages**:
- No external dependencies
- Single file
- Fast for small/medium loads

**Disadvantages**:
- Single-writer limitation
- Not ideal for high concurrency

### PostgreSQL

**Setup**:

```bash
# Install PostgreSQL
sudo apt-get install postgresql

# Create database
sudo -u postgres createdb gitignore
sudo -u postgres createuser gitignore

# Set password
sudo -u postgres psql
ALTER USER gitignore WITH PASSWORD 'secure_password';
GRANT ALL PRIVILEGES ON DATABASE gitignore TO gitignore;
\q

# Configure server
export DATABASE_URL="postgres://gitignore:secure_password@localhost:5432/gitignore?sslmode=disable"

# Or use flag
gitignore --db-url "postgres://gitignore:secure_password@localhost:5432/gitignore?sslmode=disable"
```

**Docker Compose**:

```yaml
version: '3.8'
services:
  gitignore:
    environment:
      - DATABASE_URL=postgres://gitignore:changeme@postgres:5432/gitignore
    depends_on:
      - postgres

  postgres:
    image: postgres:16-alpine
    environment:
      - POSTGRES_DB=gitignore
      - POSTGRES_USER=gitignore
      - POSTGRES_PASSWORD=changeme
    volumes:
      - ./rootfs/db/postgres:/var/lib/postgresql/data
```

### MySQL

**Setup**:

```bash
# Install MySQL
sudo apt-get install mysql-server

# Create database
sudo mysql
CREATE DATABASE gitignore;
CREATE USER 'gitignore'@'localhost' IDENTIFIED BY 'secure_password';
GRANT ALL PRIVILEGES ON gitignore.* TO 'gitignore'@'localhost';
FLUSH PRIVILEGES;
EXIT;

# Configure server
export DATABASE_URL="mysql://gitignore:secure_password@localhost:3306/gitignore"

# Or use flag
gitignore --db-url "mysql://gitignore:secure_password@localhost:3306/gitignore"
```

### Database Maintenance

**Backup**:

```bash
# SQLite
cp /var/lib/gitignore/gitignore.db /backup/gitignore.db.bak

# PostgreSQL
pg_dump gitignore > gitignore.sql

# MySQL
mysqldump gitignore > gitignore.sql
```

**Restore**:

```bash
# SQLite
cp /backup/gitignore.db.bak /var/lib/gitignore/gitignore.db

# PostgreSQL
psql gitignore < gitignore.sql

# MySQL
mysql gitignore < gitignore.sql
```

---

## Logging

### Log Files

```
access.log    # HTTP access logs
error.log     # Application errors
audit.log     # Admin actions
```

### Log Formats

**JSON** (default):
```json
{
  "time": "2024-01-01T12:00:00Z",
  "level": "info",
  "method": "GET",
  "path": "/api/v1/list",
  "status": 200,
  "duration_ms": 5,
  "ip": "192.168.1.1"
}
```

**Text**:
```
2024-01-01 12:00:00 INFO GET /api/v1/list 200 5ms 192.168.1.1
```

### Log Rotation

Logs are automatically rotated when:
- File size exceeds `max_size_mb` (default: 100MB)
- File age exceeds `max_age_days` (default: 30 days)

Old logs are compressed: `access.log.1.gz`, `access.log.2.gz`, etc.

### Viewing Logs

```bash
# Follow all logs
tail -f /var/log/gitignore/*.log

# Follow access log
tail -f /var/log/gitignore/access.log

# Search errors
grep -i error /var/log/gitignore/error.log

# View via API (admin only)
curl -H "Authorization: Bearer TOKEN" \
  http://localhost:8080/api/v1/admin/logs/access
```

---

## Backup & Restore

### Using Backup Script

```bash
# Create backup
./scripts/backup.sh

# Backups saved to ./backups/
# gitignore_backup_20240101_120000.tar.gz
# gitignore_backup_20240101_120000.tar.gz.manifest

# Restore backup
tar -xzf gitignore_backup_20240101_120000.tar.gz -C /
```

### Manual Backup

```bash
# Stop service
sudo systemctl stop gitignore

# Backup directories
tar -czf gitignore_backup.tar.gz \
  /etc/gitignore \
  /var/lib/gitignore

# Restart service
sudo systemctl start gitignore
```

### Automated Backups

**Cron** (daily at 2 AM):

```bash
# Add to crontab
sudo crontab -e

# Add line
0 2 * * * /path/to/scripts/backup.sh
```

**Systemd Timer**:

Create `/etc/systemd/system/gitignore-backup.timer`:

```ini
[Unit]
Description=GitIgnore Backup Timer

[Timer]
OnCalendar=daily
OnCalendar=02:00
Persistent=true

[Install]
WantedBy=timers.target
```

Create `/etc/systemd/system/gitignore-backup.service`:

```ini
[Unit]
Description=GitIgnore Backup

[Service]
Type=oneshot
ExecStart=/path/to/scripts/backup.sh
```

Enable:

```bash
sudo systemctl enable --now gitignore-backup.timer
```

---

## Systemd Service

### Installation

The install script creates a systemd service automatically when run as root.

**Manual Creation**:

Create `/etc/systemd/system/gitignore.service`:

```ini
[Unit]
Description=GitIgnore API Server
Documentation=https://github.com/apimgr/gitignore
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/gitignore --port 80
Restart=on-failure
RestartSec=5s

Environment=CONFIG_DIR=/etc/gitignore
Environment=DATA_DIR=/var/lib/gitignore
Environment=LOGS_DIR=/var/log/gitignore

[Install]
WantedBy=multi-user.target
```

### Service Management

```bash
# Start service
sudo systemctl start gitignore

# Stop service
sudo systemctl stop gitignore

# Restart service
sudo systemctl restart gitignore

# Enable on boot
sudo systemctl enable gitignore

# Disable on boot
sudo systemctl disable gitignore

# View status
sudo systemctl status gitignore

# View logs
sudo journalctl -u gitignore -f
```

---

## Reverse Proxy

### Nginx

```nginx
server {
    listen 80;
    server_name gitignore.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
    }
}

# HTTPS
server {
    listen 443 ssl http2;
    server_name gitignore.example.com;

    ssl_certificate /etc/letsencrypt/live/gitignore.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/gitignore.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Host $host;
    }
}
```

### Caddy

```caddy
gitignore.example.com {
    reverse_proxy localhost:8080
}
```

### Apache

```apache
<VirtualHost *:80>
    ServerName gitignore.example.com

    ProxyPreserveHost On
    ProxyPass / http://127.0.0.1:8080/
    ProxyPassReverse / http://127.0.0.1:8080/

    RequestHeader set X-Forwarded-Proto "http"
    RequestHeader set X-Forwarded-Host "%{HTTP_HOST}s"
</VirtualHost>
```

---

## Security

### Best Practices

1. **Change default credentials immediately**
2. **Use HTTPS in production**
3. **Restrict admin routes to internal network**
4. **Enable rate limiting via reverse proxy**
5. **Keep software updated**
6. **Use strong passwords**
7. **Rotate API tokens regularly**
8. **Monitor logs for suspicious activity**

### File Permissions

```bash
# Config directory (contains credentials)
sudo chmod 700 /etc/gitignore
sudo chmod 600 /etc/gitignore/admin_credentials

# Data directory
sudo chmod 755 /var/lib/gitignore
sudo chmod 644 /var/lib/gitignore/gitignore.db

# Logs directory
sudo chmod 755 /var/log/gitignore
sudo chmod 644 /var/log/gitignore/*.log
```

### Firewall

```bash
# Allow only localhost
sudo ufw allow from 127.0.0.1 to any port 8080

# Or specific network
sudo ufw allow from 192.168.1.0/24 to any port 8080

# Public access (not recommended without reverse proxy)
sudo ufw allow 8080/tcp
```

---

## Troubleshooting

### Server Won't Start

**Check if port is in use**:

```bash
sudo lsof -i :8080
sudo netstat -tulpn | grep 8080
```

**Check permissions**:

```bash
ls -la /etc/gitignore
ls -la /var/lib/gitignore
```

**Check logs**:

```bash
sudo journalctl -u gitignore -n 50
cat /var/log/gitignore/error.log
```

### Database Errors

**SQLite locked**:

```bash
# Stop all instances
sudo systemctl stop gitignore
pkill gitignore

# Check for stale locks
lsof /var/lib/gitignore/gitignore.db

# Restart
sudo systemctl start gitignore
```

**Connection errors**:

```bash
# Test database connection
gitignore --db-url "postgres://..." --status

# Check database is running
sudo systemctl status postgresql
sudo systemctl status mysql
```

### Performance Issues

**Check resource usage**:

```bash
top -p $(pidof gitignore)
htop -p $(pidof gitignore)
```

**Enable caching**:

Templates are cached in memory by default. If disabled, enable it:

```bash
curl -H "Authorization: Bearer TOKEN" \
  -X PUT \
  -d '{"settings":{"templates.cache":"true"}}' \
  http://localhost:8080/api/v1/admin/settings
```

**Database optimization**:

```bash
# SQLite
sqlite3 /var/lib/gitignore/gitignore.db "VACUUM; ANALYZE;"

# PostgreSQL
psql gitignore -c "VACUUM ANALYZE;"

# MySQL
mysqlcheck -o gitignore
```

### Can't Access Admin Panel

**Verify credentials**:

```bash
cat /etc/gitignore/admin_credentials
```

**Test authentication**:

```bash
# Test Basic Auth
curl -u username:password http://localhost:8080/admin

# Test Bearer token
curl -H "Authorization: Bearer TOKEN" \
  http://localhost:8080/api/v1/admin
```

**Reset credentials**:

```bash
# Delete database and restart
sudo systemctl stop gitignore
sudo rm /var/lib/gitignore/gitignore.db
sudo systemctl start gitignore
```

### Enable Debug Mode

```bash
# Via flag
gitignore --dev

# Via environment
export DEV=true
gitignore

# Via systemd
sudo systemctl edit gitignore

# Add:
[Service]
Environment=DEV=true

# Restart
sudo systemctl restart gitignore
```

---

## Getting Help

- **Documentation**: https://github.com/apimgr/gitignore/tree/main/docs
- **Issues**: https://github.com/apimgr/gitignore/issues
- **Discussions**: https://github.com/apimgr/gitignore/discussions

---

**For API documentation, see [API.md](API.md)**
