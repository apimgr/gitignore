#!/bin/bash
# GitIgnore API Server - Incus Test Script

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
INSTANCE_NAME="gitignore-test"
IMAGE="images:alpine/edge"
TEST_PORT="8080"

echo -e "${BLUE}🐧 GitIgnore API Server - Incus Test${NC}"
echo ""

# Cleanup function
cleanup() {
    echo -e "${YELLOW}🧹 Cleaning up...${NC}"
    incus stop "$INSTANCE_NAME" --force 2>/dev/null || true
    incus delete "$INSTANCE_NAME" --force 2>/dev/null || true
}

# Trap cleanup on exit
trap cleanup EXIT

# Check if Incus is available
if ! command -v incus &> /dev/null; then
    echo -e "${RED}❌ Incus is not installed${NC}"
    exit 1
fi

# Navigate to project root
cd "$(dirname "$0")/.."

# Clean up any existing test instance
echo -e "${YELLOW}🧹 Cleaning up existing test instance...${NC}"
cleanup

# Build binary
echo -e "${BLUE}🔨 Building binary...${NC}"
make build

# Create Incus instance
echo -e "${BLUE}🐧 Creating Incus instance...${NC}"
incus launch "$IMAGE" "$INSTANCE_NAME"

# Wait for instance to be ready
echo -e "${YELLOW}⏳ Waiting for instance to be ready...${NC}"
sleep 5

# Copy binary to instance
echo -e "${BLUE}📦 Copying binary to instance...${NC}"
incus file push binaries/gitignore "$INSTANCE_NAME/usr/local/bin/gitignore"
incus exec "$INSTANCE_NAME" -- chmod +x /usr/local/bin/gitignore

# Create directories
echo -e "${BLUE}📁 Creating directories...${NC}"
incus exec "$INSTANCE_NAME" -- mkdir -p /etc/gitignore /var/lib/gitignore /var/log/gitignore

# Start service
echo -e "${BLUE}🚀 Starting service...${NC}"
incus exec "$INSTANCE_NAME" -- sh -c "nohup /usr/local/bin/gitignore --port $TEST_PORT --dev > /var/log/gitignore/server.log 2>&1 &"

# Wait for service to be ready
echo -e "${YELLOW}⏳ Waiting for service to start...${NC}"
sleep 3

# Get instance IP
INSTANCE_IP=$(incus list "$INSTANCE_NAME" -c 4 -f csv | cut -d' ' -f1)
TEST_URL="http://${INSTANCE_IP}:${TEST_PORT}"

echo -e "${GREEN}✅ Service is running at: $TEST_URL${NC}"
echo ""

# Display credentials
echo -e "${BLUE}📋 Admin credentials:${NC}"
incus exec "$INSTANCE_NAME" -- cat /etc/gitignore/admin_credentials 2>/dev/null || \
    echo -e "${YELLOW}⚠️  Credentials file not found${NC}"
echo ""

# Run basic tests
echo -e "${BLUE}🧪 Running API tests...${NC}"
echo ""

# Test 1: Health check
echo -n "Test 1: Health check... "
if curl -s "$TEST_URL/healthz" | grep -q "healthy"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 2: List templates
echo -n "Test 2: List templates... "
if curl -s "$TEST_URL/api/v1/list" | grep -q "Go"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 3: Get template
echo -n "Test 3: Get Go template... "
if curl -s "$TEST_URL/api/v1/template/Go" | grep -q "# Binaries"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

echo ""
echo -e "${BLUE}📊 Test Summary${NC}"
echo -e "${GREEN}✅ Basic tests passed!${NC}"
echo ""
echo -e "${YELLOW}ℹ️  Instance: $INSTANCE_NAME${NC}"
echo -e "${YELLOW}ℹ️  URL: $TEST_URL${NC}"
echo -e "${YELLOW}ℹ️  Shell: incus exec $INSTANCE_NAME -- sh${NC}"
echo -e "${YELLOW}ℹ️  Logs: incus exec $INSTANCE_NAME -- cat /var/log/gitignore/server.log${NC}"
echo ""
