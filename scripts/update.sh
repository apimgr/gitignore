#!/bin/bash
# GitIgnore API Server - Update Script

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
BINARY_NAME="gitignore"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="gitignore"
VERSION="${VERSION:-latest}"
REPOSITORY_URL="${REPOSITORY_URL:-https://github.com/apimgr/gitignore}"
PROJECT_ORG="${PROJECT_ORG:-$(printf '%s\n' "$REPOSITORY_URL" | sed -E 's|https?://[^/]+/([^/]+)/.*|\1|')}"
INTERNAL_NAME="${INTERNAL_NAME:-$(basename "${REPOSITORY_URL%.git}")}"
case "$PROJECT_ORG:$INTERNAL_NAME" in
    *[!A-Za-z0-9_:-]*|:*|*:) echo "Invalid project temp directory identity" >&2; exit 1 ;;
esac
TEMP_PARENT="${TMPDIR:-/tmp}/${PROJECT_ORG}"
mkdir -p "$TEMP_PARENT"
TEMP_DIR=$(mktemp -d "$TEMP_PARENT/${INTERNAL_NAME}-XXXXXX")
TEMP_FILE="$TEMP_DIR/$BINARY_NAME.new"
cleanup_temp() {
    case "$TEMP_DIR" in
        "$TEMP_PARENT/${INTERNAL_NAME}-"*) rm -rf -- "$TEMP_DIR" ;;
    esac
}
trap cleanup_temp EXIT

echo -e "${BLUE}🔄 GitIgnore API Server - Update${NC}"
echo ""

# Check if running as root
if [ "$EUID" -eq 0 ]; then
    IS_ROOT=true
else
    IS_ROOT=false
    INSTALL_DIR="$HOME/.local/bin"
fi

# Check if binary exists
if [ ! -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    echo -e "${RED}❌ ${BINARY_NAME} not found in ${INSTALL_DIR}${NC}"
    echo -e "${YELLOW}Run install.sh first${NC}"
    exit 1
fi

# Get current version
CURRENT_VERSION=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null | grep -oP 'v\d+\.\d+\.\d+' || echo "unknown")
echo -e "${BLUE}Current version: ${CURRENT_VERSION}${NC}"

# Detect OS and architecture
detect_os() {
    case "$(uname -s)" in
        Linux*) OS="linux" ;;
        Darwin*) OS="darwin" ;;
        FreeBSD*) OS="freebsd" ;;
        *) echo -e "${RED}❌ Unsupported OS${NC}"; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo -e "${RED}❌ Unsupported architecture${NC}"; exit 1 ;;
    esac
}

detect_os
detect_arch

# Stop service if running
stop_service() {
    if [ "$IS_ROOT" = true ] && command -v systemctl &> /dev/null; then
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            echo -e "${YELLOW}🛑 Stopping service...${NC}"
            systemctl stop "$SERVICE_NAME"
        fi
    fi
}

# Download new binary
echo -e "${BLUE}📦 Downloading update...${NC}"

DOWNLOAD_URL="${REPOSITORY_URL}/releases/${VERSION}/download/${BINARY_NAME}-${OS}-${ARCH}"

if command -v curl &> /dev/null; then
    curl -LSs "$DOWNLOAD_URL" -o "$TEMP_FILE"
elif command -v wget &> /dev/null; then
    wget -q "$DOWNLOAD_URL" -O "$TEMP_FILE"
else
    echo -e "${RED}❌ Neither curl nor wget found${NC}"
    exit 1
fi

chmod +x "$TEMP_FILE"

# Backup old binary
echo -e "${BLUE}💾 Backing up current binary...${NC}"
cp "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}.bak"

# Stop service
stop_service

# Install new binary
echo -e "${BLUE}📦 Installing update...${NC}"
mv "$TEMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

# Get new version
NEW_VERSION=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null | grep -oP 'v\d+\.\d+\.\d+' || echo "unknown")
echo -e "${GREEN}✅ Updated to version: ${NEW_VERSION}${NC}"

# Start service if it was running
if [ "$IS_ROOT" = true ] && command -v systemctl &> /dev/null; then
    echo -e "${BLUE}🚀 Starting service...${NC}"
    systemctl start "$SERVICE_NAME"
    echo -e "${GREEN}✅ Service started${NC}"
fi

echo ""
echo -e "${GREEN}✅ Update complete!${NC}"
echo ""
echo -e "${BLUE}To rollback:${NC}"
echo "  mv ${INSTALL_DIR}/${BINARY_NAME}.bak ${INSTALL_DIR}/${BINARY_NAME}"
echo ""
