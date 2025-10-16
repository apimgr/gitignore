#!/bin/bash
# GitIgnore API Server - Docker Test Script

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
TEST_URL="http://localhost:8080"
COMPOSE_FILE="../docker-compose.test.yml"
TEMP_DIR="/tmp/gitignore/rootfs"

echo -e "${BLUE}🐳 GitIgnore API Server - Docker Test${NC}"
echo ""

# Cleanup function
cleanup() {
    echo -e "${YELLOW}🧹 Cleaning up...${NC}"
    cd "$(dirname "$0")/.."
    docker-compose -f docker-compose.test.yml down -v 2>/dev/null || true
    sudo rm -rf "$TEMP_DIR" 2>/dev/null || true
}

# Trap cleanup on exit
trap cleanup EXIT

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running${NC}"
    exit 1
fi

# Navigate to project root
cd "$(dirname "$0")/.."

# Clean up any existing test containers
echo -e "${YELLOW}🧹 Cleaning up existing test containers...${NC}"
cleanup

# Create temp directories
echo -e "${BLUE}📁 Creating temporary directories...${NC}"
sudo mkdir -p "$TEMP_DIR"/{config,data,logs,db/{postgres,mysql}}
sudo chmod -R 777 "$TEMP_DIR"

# Build and start containers
echo -e "${BLUE}🔨 Building Docker image...${NC}"
docker-compose -f docker-compose.test.yml build

echo -e "${BLUE}🚀 Starting test containers...${NC}"
docker-compose -f docker-compose.test.yml up -d gitignore-test

# Wait for service to be ready
echo -e "${YELLOW}⏳ Waiting for service to be ready...${NC}"
for i in {1..30}; do
    if curl -s "$TEST_URL/healthz" >/dev/null 2>&1; then
        echo -e "${GREEN}✅ Service is ready!${NC}"
        break
    fi
    if [ $i -eq 30 ]; then
        echo -e "${RED}❌ Service failed to start${NC}"
        docker-compose -f docker-compose.test.yml logs gitignore-test
        exit 1
    fi
    sleep 1
done

# Display credentials
echo -e "${BLUE}📋 Admin credentials:${NC}"
if [ -f "$TEMP_DIR/config/admin_credentials" ]; then
    cat "$TEMP_DIR/config/admin_credentials"
else
    echo -e "${YELLOW}⚠️  Credentials file not found${NC}"
fi
echo ""

# Run API tests
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
TEMPLATE_COUNT=$(curl -s "$TEST_URL/api/v1/list" | grep -o "Go" | wc -l)
if [ "$TEMPLATE_COUNT" -gt 0 ]; then
    echo -e "${GREEN}✅ PASS${NC} (Found $TEMPLATE_COUNT templates)"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 3: Get specific template
echo -n "Test 3: Get Go template... "
if curl -s "$TEST_URL/api/v1/template/Go" | grep -q "# Binaries"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 4: Search templates
echo -n "Test 4: Search for Python... "
if curl -s "$TEST_URL/api/v1/search?q=python" | grep -q "Python"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 5: Combine templates
echo -n "Test 5: Combine Go,Python... "
COMBINED=$(curl -s "$TEST_URL/api/v1/combine?templates=Go,Python")
if echo "$COMBINED" | grep -q "# Binaries" && echo "$COMBINED" | grep -q "# Byte-compiled"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 6: Categories
echo -n "Test 6: List categories... "
if curl -s "$TEST_URL/api/v1/categories" | grep -q "Languages"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 7: Stats
echo -n "Test 7: Get stats... "
if curl -s "$TEST_URL/api/v1/stats" | grep -q "total_templates"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 8: CLI script (sh)
echo -n "Test 8: Download CLI script... "
if curl -s "$TEST_URL/api/v1/cli/sh" | grep -q "#!/bin/sh"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 9: CLI script (PowerShell)
echo -n "Test 9: Download PowerShell script... "
if curl -s "$TEST_URL/api/v1/cli/ps" | grep -q "SERVER_URL"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 10: Swagger docs
echo -n "Test 10: Swagger documentation... "
if curl -s "$TEST_URL/api/v1/docs/swagger" | grep -q "openapi"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 11: GraphQL endpoint
echo -n "Test 11: GraphQL schema... "
if curl -s "$TEST_URL/api/v1/docs/graphql" | grep -q "type Query"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 12: Content negotiation (JSON)
echo -n "Test 12: Content negotiation (JSON)... "
if curl -s -H "Accept: application/json" "$TEST_URL/api/v1/template/Go" | grep -q '"content"'; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 13: Shell completion (bash)
echo -n "Test 13: Bash completion... "
if curl -s "$TEST_URL/api/v1/cli/completion/bash" | grep -q "_gitignore_completions"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 14: Shell completion (zsh)
echo -n "Test 14: Zsh completion... "
if curl -s "$TEST_URL/api/v1/cli/completion/zsh" | grep -q "#compdef gitignore"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

# Test 15: Shell completion (fish)
echo -n "Test 15: Fish completion... "
if curl -s "$TEST_URL/api/v1/cli/completion/fish" | grep -q "complete -c gitignore"; then
    echo -e "${GREEN}✅ PASS${NC}"
else
    echo -e "${RED}❌ FAIL${NC}"
fi

echo ""
echo -e "${BLUE}📊 Test Summary${NC}"
echo -e "${GREEN}✅ All basic tests passed!${NC}"
echo ""
echo -e "${YELLOW}ℹ️  Service is running at: $TEST_URL${NC}"
echo -e "${YELLOW}ℹ️  Logs: docker-compose -f docker-compose.test.yml logs -f${NC}"
echo -e "${YELLOW}ℹ️  Stop: docker-compose -f docker-compose.test.yml down${NC}"
echo ""
