#!/bin/bash
# GitIgnore API Server - Backup Script

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
BACKUP_DIR="${BACKUP_DIR:-./backups}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="gitignore_backup_${TIMESTAMP}"

echo -e "${BLUE}💾 GitIgnore API Server - Backup${NC}"
echo ""

# Detect directories
if [ "$EUID" -eq 0 ]; then
    CONFIG_DIR="${CONFIG_DIR:-/etc/gitignore}"
    DATA_DIR="${DATA_DIR:-/var/lib/gitignore}"
else
    CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/gitignore}"
    DATA_DIR="${DATA_DIR:-$HOME/.local/share/gitignore}"
fi

echo -e "${BLUE}Config directory: ${CONFIG_DIR}${NC}"
echo -e "${BLUE}Data directory: ${DATA_DIR}${NC}"
echo -e "${BLUE}Backup directory: ${BACKUP_DIR}${NC}"
echo ""

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Create backup archive
echo -e "${BLUE}📦 Creating backup archive...${NC}"

BACKUP_FILE="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz"

tar -czf "$BACKUP_FILE" \
    -C "$(dirname "$CONFIG_DIR")" "$(basename "$CONFIG_DIR")" \
    -C "$(dirname "$DATA_DIR")" "$(basename "$DATA_DIR")" \
    2>/dev/null || true

if [ -f "$BACKUP_FILE" ]; then
    BACKUP_SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
    echo -e "${GREEN}✅ Backup created: ${BACKUP_FILE}${NC}"
    echo -e "${BLUE}Size: ${BACKUP_SIZE}${NC}"
else
    echo -e "${RED}❌ Backup failed${NC}"
    exit 1
fi

# Create backup manifest
echo -e "${BLUE}📝 Creating manifest...${NC}"

cat > "${BACKUP_FILE}.manifest" <<EOF
GitIgnore API Server Backup
============================
Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Hostname: $(hostname)
User: $(whoami)

Directories:
  Config: ${CONFIG_DIR}
  Data: ${DATA_DIR}

Files:
$(tar -tzf "$BACKUP_FILE" | sed 's/^/  /')

Checksums:
  MD5: $(md5sum "$BACKUP_FILE" | cut -d' ' -f1)
  SHA256: $(sha256sum "$BACKUP_FILE" | cut -d' ' -f1)
EOF

echo -e "${GREEN}✅ Manifest created${NC}"

# Cleanup old backups (keep last 10)
echo -e "${BLUE}🧹 Cleaning up old backups...${NC}"

BACKUP_COUNT=$(ls -1 "${BACKUP_DIR}"/gitignore_backup_*.tar.gz 2>/dev/null | wc -l)

if [ "$BACKUP_COUNT" -gt 10 ]; then
    ls -1t "${BACKUP_DIR}"/gitignore_backup_*.tar.gz | tail -n +11 | xargs rm -f
    ls -1t "${BACKUP_DIR}"/gitignore_backup_*.tar.gz.manifest | tail -n +11 | xargs rm -f 2>/dev/null || true
    echo -e "${GREEN}✅ Cleaned up old backups${NC}"
else
    echo -e "${BLUE}No cleanup needed (${BACKUP_COUNT} backups)${NC}"
fi

echo ""
echo -e "${GREEN}✅ Backup complete!${NC}"
echo ""
echo -e "${BLUE}To restore this backup:${NC}"
echo "  tar -xzf ${BACKUP_FILE} -C /"
echo ""
