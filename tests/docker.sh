#!/usr/bin/env bash
# @@License : WTFPL
# GitIgnore API Server - Beta Testing (Docker/Alpine)

set -eo pipefail

PROJECT_NAME=$(basename "$PWD")
PROJECT_ORG=$(basename "$(dirname "$PWD")")
API_VERSION="v1"

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

echo "Testing in Docker (Alpine)..."
docker run --rm \
  --name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
  -v "$PWD/binaries:/app" \
  -e API_VERSION="$API_VERSION" \
  alpine:latest sh -c '
    set -e

    apk add --no-cache curl bash file jq >/dev/null

    chmod +x /app/'"${PROJECT_NAME}"'

    echo "=== Version Check ==="
    /app/'"${PROJECT_NAME}"' --version

    echo "=== Help Check ==="
    /app/'"${PROJECT_NAME}"' --help

    echo "=== Binary Info ==="
    ls -lh /app/'"${PROJECT_NAME}"'
    file /app/'"${PROJECT_NAME}"'

    echo "=== Starting Server for API Tests ==="
    /app/'"${PROJECT_NAME}"' --port 64580 --address 0.0.0.0 > /tmp/server.log 2>&1 &
    SERVER_PID=$!
    sleep 3

    BASE_URL="http://localhost:64580"
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
    cp /app/'"${PROJECT_NAME}"' /app/renamed-server
    chmod +x /app/renamed-server
    if /app/renamed-server --help 2>&1 | grep -q "renamed-server"; then
        echo "PASS: server binary rename works (--help shows actual name)"
    else
        echo "FAILED: server --help does not show renamed binary name"
        FAILED=1
    fi

    echo "=== Stopping Server ==="
    kill $SERVER_PID
    wait $SERVER_PID 2>/dev/null || true

    if [ "$FAILED" -ne 0 ]; then
        echo "=== One or more tests FAILED ==="
        exit 1
    fi

    echo "=== All tests passed ==="
'

echo "Docker tests completed successfully"
