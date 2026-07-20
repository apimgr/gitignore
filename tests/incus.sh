#!/usr/bin/env bash
# @@License : WTFPL
# GitIgnore API Server - Beta Testing (Incus/Debian + systemd)

set -eo pipefail

if ! command -v incus &>/dev/null; then
    echo "ERROR: incus not found. Install incus or use tests/docker.sh"
    exit 1
fi

PROJECT_NAME=$(basename "$PWD")
PROJECT_ORG=$(basename "$(dirname "$PWD")")
API_VERSION="v1"
CONTAINER_NAME="test-${PROJECT_NAME}-$$"
INCUS_IMAGE="images:debian/trixie"

trap 'incus delete "$CONTAINER_NAME" --force 2>/dev/null || true' EXIT

if [ -f "Makefile" ]; then
    echo "Building with make build..."
    make build
else
    echo "Building in Docker (no Makefile)..."
    GO_CACHE="${GO_CACHE:-$HOME/go/pkg/mod}"
    GO_BUILD="${GO_BUILD:-$HOME/.cache/go-build/${PROJECT_NAME}}"
    mkdir -p "$GO_CACHE" "$GO_BUILD" binaries
    docker run --rm \
      --name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
      -v "$PWD:/app" \
      -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
      -v "$GO_BUILD:/usr/local/share/go/cache" \
      -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
      casjaysdev/go:latest go build -buildvcs=false -trimpath -ldflags "-s -w" -o "/app/binaries/${PROJECT_NAME}" ./src
fi

echo "Launching Incus container (Debian + systemd)..."
incus launch "$INCUS_IMAGE" "$CONTAINER_NAME"

sleep 2

echo "Copying binary to container..."
incus file push "binaries/${PROJECT_NAME}" "$CONTAINER_NAME/usr/local/bin/"
incus exec "$CONTAINER_NAME" -- chmod +x "/usr/local/bin/${PROJECT_NAME}"

echo "Ensuring curl/jq are available for testing..."
incus exec "$CONTAINER_NAME" -- bash -c "command -v curl && command -v jq || (apt-get update && apt-get install -y curl jq)" >/dev/null 2>&1

echo "Running tests in Incus..."
incus exec "$CONTAINER_NAME" -- env API_VERSION="$API_VERSION" PROJECT_NAME="$PROJECT_NAME" bash -c '
    set -eo pipefail

    echo "=== Version Check ==="
    "${PROJECT_NAME}" --version

    echo "=== Help Check ==="
    "${PROJECT_NAME}" --help

    echo "=== Binary Info ==="
    ls -lh "/usr/local/bin/${PROJECT_NAME}"
    file "/usr/local/bin/${PROJECT_NAME}"

    echo "=== Service Install Test ==="
    "${PROJECT_NAME}" --service --install

    echo "=== Service Start Test ==="
    # inside Incus container - not a host-service mutation
    systemctl start "${PROJECT_NAME}"
    sleep 2
    # inside Incus container - not a host-service mutation
    systemctl status "${PROJECT_NAME}" || true

    BASE_URL="http://localhost:80"
    FAILED=0

    check() {
        if ! eval "$2" >/dev/null 2>&1; then
            echo "FAILED: $1"
            FAILED=1
        else
            echo "PASS: $1"
        fi
    }

    echo "=== Health Endpoint ==="
    check "GET /healthz" "curl -q -LSsf ${BASE_URL}/healthz | grep -q healthy"
    check "GET /api/${API_VERSION}/healthz" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/healthz | jq -e .status"
    check "GET /api/${API_VERSION}/healthz.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/healthz.txt"

    echo "=== Content Negotiation ==="
    check "Accept: application/json /api/${API_VERSION}/" "curl -q -LSsf -H \"Accept: application/json\" ${BASE_URL}/api/${API_VERSION}/ | jq ."
    check "Accept: text/html /" "curl -q -LSsf -H \"Accept: text/html\" ${BASE_URL}/ | grep -qi \"<!DOCTYPE html\""
    check "Accept: text/plain /" "curl -q -LSsf -H \"Accept: text/plain\" ${BASE_URL}/"

    echo "=== Template API Endpoints ==="
    check "GET /api/${API_VERSION}/list" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/list | jq ."
    check "GET /api/${API_VERSION}/list.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/list.txt"
    check "GET /api/${API_VERSION}/template/go" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/template/go"
    check "GET /api/${API_VERSION}/template/go.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/template/go.txt"
    check "GET /api/${API_VERSION}/template/go.json" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/template/go.json | jq ."
    check "GET /api/${API_VERSION}/categories" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/categories | jq ."
    check "GET /api/${API_VERSION}/search?q=go" "curl -q -LSsf \"${BASE_URL}/api/${API_VERSION}/search?q=go\" | jq ."
    check "GET /api/${API_VERSION}/combine?templates=go,node" "curl -q -LSsf \"${BASE_URL}/api/${API_VERSION}/combine?templates=go,node\""
    check "GET /api/${API_VERSION}/stats" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/stats | jq ."

    echo "=== gitignore.io Compatibility Layer ==="
    check "GET /api/list" "curl -q -LSsf ${BASE_URL}/api/list"
    check "GET /api/go,node" "curl -q -LSsf ${BASE_URL}/api/go,node | grep -q \"### Go ###\""

    echo "=== Frontend Smart Detection ==="
    check "CLI -> plain text /list" "curl -q -LSsf ${BASE_URL}/list"
    check "Browser -> HTML /categories" "curl -q -LSsfI -H \"Accept: text/html\" ${BASE_URL}/categories | grep -qi \"text/html\""

    echo "=== Well-Known / Special Files ==="
    check "GET /robots.txt" "curl -q -LSsf ${BASE_URL}/robots.txt"
    check "GET /.well-known/security.txt" "curl -q -LSsf ${BASE_URL}/.well-known/security.txt"
    check "GET /manifest.json" "curl -q -LSsf ${BASE_URL}/manifest.json | jq ."

    echo "=== Documentation Endpoints ==="
    check "GET /api/${API_VERSION}/openapi.json" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/openapi.json | jq ."
    check "GET /api/${API_VERSION}/schema.graphql" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/schema.graphql"

    echo "=== Binary Rename Test ==="
    cp "/usr/local/bin/${PROJECT_NAME}" /tmp/renamed-server
    chmod +x /tmp/renamed-server
    if /tmp/renamed-server --help 2>&1 | grep -q "renamed-server"; then
        echo "PASS: server binary rename works (--help shows actual name)"
    else
        echo "FAILED: server --help does not show renamed binary name"
        FAILED=1
    fi

    echo "=== Service Stop Test ==="
    # inside Incus container - not a host-service mutation
    systemctl stop "${PROJECT_NAME}"

    if [ "$FAILED" -ne 0 ]; then
        echo "=== One or more tests FAILED ==="
        exit 1
    fi

    echo "=== All tests passed ==="
'

echo "Incus tests completed successfully"
