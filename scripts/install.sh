#!/bin/bash
# GitIgnore API Server - Installation Script

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

echo -e "${BLUE}🚀 GitIgnore API Server - Installation${NC}"
echo ""

# Check if running as root
if [ "$EUID" -eq 0 ]; then
    IS_ROOT=true
    CONFIG_DIR="/etc/gitignore"
    DATA_DIR="/var/lib/gitignore"
    LOGS_DIR="/var/log/gitignore"
else
    IS_ROOT=false
    CONFIG_DIR="$HOME/.config/gitignore"
    DATA_DIR="$HOME/.local/share/gitignore"
    LOGS_DIR="$HOME/.local/state/gitignore"
fi

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)
            OS="linux"
            ;;
        Darwin*)
            OS="darwin"
            ;;
        FreeBSD*)
            OS="freebsd"
            ;;
        *)
            echo -e "${RED}❌ Unsupported OS: $(uname -s)${NC}"
            exit 1
            ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo -e "${RED}❌ Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac
}

# Download binary
download_binary() {
    echo -e "${BLUE}📦 Downloading binary...${NC}"

    DOWNLOAD_URL="https://github.com/apimgr/gitignore/releases/${VERSION}/download/${BINARY_NAME}-${OS}-${ARCH}"

    if [ "$OS" = "windows" ]; then
        DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
    fi

    TEMP_FILE="/tmp/${BINARY_NAME}"

    if command -v curl &> /dev/null; then
        curl -LSs "$DOWNLOAD_URL" -o "$TEMP_FILE"
    elif command -v wget &> /dev/null; then
        wget -q "$DOWNLOAD_URL" -O "$TEMP_FILE"
    else
        echo -e "${RED}❌ Neither curl nor wget found${NC}"
        exit 1
    fi

    chmod +x "$TEMP_FILE"
    echo -e "${GREEN}✅ Downloaded successfully${NC}"
}

# Install binary
install_binary() {
    echo -e "${BLUE}📦 Installing binary...${NC}"

    if [ "$IS_ROOT" = true ]; then
        mv "$TEMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
        chmod +x "$INSTALL_DIR/$BINARY_NAME"
    else
        mkdir -p "$HOME/.local/bin"
        mv "$TEMP_FILE" "$HOME/.local/bin/$BINARY_NAME"
        chmod +x "$HOME/.local/bin/$BINARY_NAME"

        # Add to PATH if not already there
        if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
            echo -e "${YELLOW}⚠️  Add $HOME/.local/bin to your PATH${NC}"
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
        fi
    fi

    echo -e "${GREEN}✅ Binary installed${NC}"
}

# Create directories
create_directories() {
    echo -e "${BLUE}📁 Creating directories...${NC}"

    mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOGS_DIR"

    if [ "$IS_ROOT" = true ]; then
        chmod 755 "$CONFIG_DIR" "$DATA_DIR" "$LOGS_DIR"
    fi

    echo -e "${GREEN}✅ Directories created${NC}"
}

# Create systemd service
create_systemd_service() {
    if [ "$IS_ROOT" = false ]; then
        return
    fi

    if ! command -v systemctl &> /dev/null; then
        return
    fi

    echo -e "${BLUE}⚙️  Creating systemd service...${NC}"

    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=GitIgnore API Server
Documentation=https://github.com/apimgr/gitignore
After=network.target

[Service]
Type=simple
User=root
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --port 80
Restart=on-failure
RestartSec=5s

Environment=CONFIG_DIR=${CONFIG_DIR}
Environment=DATA_DIR=${DATA_DIR}
Environment=LOGS_DIR=${LOGS_DIR}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    echo -e "${GREEN}✅ Systemd service created${NC}"
}

# Main installation
main() {
    detect_os
    detect_arch

    echo -e "${BLUE}System: ${OS}-${ARCH}${NC}"
    echo -e "${BLUE}Install directory: ${INSTALL_DIR}${NC}"
    echo -e "${BLUE}Config directory: ${CONFIG_DIR}${NC}"
    echo -e "${BLUE}Data directory: ${DATA_DIR}${NC}"
    echo -e "${BLUE}Logs directory: ${LOGS_DIR}${NC}"
    echo ""

    if [ -f "../binaries/${BINARY_NAME}" ]; then
        # Use local binary if available
        echo -e "${BLUE}📦 Using local binary${NC}"
        TEMP_FILE="../binaries/${BINARY_NAME}"
    else
        download_binary
    fi

    install_binary
    create_directories

    if [ "$IS_ROOT" = true ]; then
        create_systemd_service
    fi

    echo ""
    echo -e "${GREEN}✅ Installation complete!${NC}"
    echo ""

    if [ "$IS_ROOT" = true ]; then
        echo -e "${BLUE}To start the service:${NC}"
        echo "  systemctl start ${SERVICE_NAME}"
        echo "  systemctl enable ${SERVICE_NAME}"
        echo ""
    else
        echo -e "${BLUE}To run the server:${NC}"
        echo "  ${BINARY_NAME}"
        echo ""
    fi

    echo -e "${YELLOW}⚠️  Admin credentials will be generated on first run${NC}"
    echo -e "${YELLOW}⚠️  Check: ${CONFIG_DIR}/admin_credentials${NC}"
}

main "$@"
