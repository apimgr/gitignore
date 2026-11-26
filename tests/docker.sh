#!/usr/bin/env bash
# @@License : WTFPL
# GitIgnore API Server - Beta Testing (Docker/Alpine)

set -eo pipefail

PROJECT_DIR=$(git rev-parse --show-toplevel)
PROJECT_NAME=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_name:[[:space:]]*/{sub(/^project_name:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
PROJECT_ORG=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_org:[[:space:]]*/{sub(/^project_org:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
REMOTE=$(git -C "$PROJECT_DIR" remote get-url origin 2>/dev/null || true)
if [ -z "$PROJECT_NAME" ]; then PROJECT_NAME=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)(\.git)?$|\1|'); [ -n "$PROJECT_NAME" ] || PROJECT_NAME=$(basename "$PROJECT_DIR"); fi
if [ -z "$PROJECT_ORG" ]; then PROJECT_ORG=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$|\1|'); [ -n "$PROJECT_ORG" ] || PROJECT_ORG=$(basename "$(dirname "$PROJECT_DIR")"); fi
API_VERSION="v1"
TEMP_PARENT="${TMPDIR:-/tmp}/${PROJECT_ORG}"
mkdir -p "$TEMP_PARENT"
TEST_TEMP_DIR=$(mktemp -d "$TEMP_PARENT/${PROJECT_NAME}-docker-XXXXXXXX")
chmod 700 "$TEST_TEMP_DIR"
BUILD_DIR="$TEST_TEMP_DIR/binaries"
cleanup() {
    status=$?
    if [ -n "$TEST_TEMP_DIR" ] && [ -d "$TEST_TEMP_DIR" ]; then
        rm -rf -- "$TEST_TEMP_DIR"
    fi
    return "$status"
}
trap cleanup EXIT

if [ -f "Makefile" ]; then
    echo "Building host binaries with make local..."
    make local BINDIR="$BUILD_DIR" RELDIR="$TEST_TEMP_DIR/releases"
else
    echo "Building in Docker (no Makefile)..."
    GO_CACHE="${GO_CACHE:-$TEST_TEMP_DIR/go-mod}"
    GO_BUILD="${GO_BUILD:-$TEST_TEMP_DIR/go-build}"
    mkdir -p "$GO_CACHE" "$GO_BUILD" "$BUILD_DIR"
    docker run --rm \
      --name "${PROJECT_NAME}-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')" \
      -v "$PWD:/app" \
      -v "$BUILD_DIR:/build" \
      -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
      -v "$GO_BUILD:/usr/local/share/go/cache" \
      -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
      casjaysdev/go:latest go build -buildvcs=false -trimpath -ldflags "-s -w" -o "/build/${PROJECT_NAME}" ./src
fi

echo "Testing in Docker (Alpine)..."
docker run --rm \
  --name "${PROJECT_NAME}-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')" \
  -v "$BUILD_DIR:/app" \
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
    TEST_TMP=/tmp/apimgr/gitignore-test
    mkdir -p "$TEST_TMP"
    DATABASE_DIR="$TEST_TMP/database" /app/'"${PROJECT_NAME}"' --config "$TEST_TMP/config" --data "$TEST_TMP/data" --cache "$TEST_TMP/cache" --log "$TEST_TMP/logs" --backup "$TEST_TMP/backup" --port 64580 --address 0.0.0.0 > "$TEST_TMP/server.log" 2>&1 &
    SERVER_PID=$!
    sleep 3
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "Server exited before the first request"
        cat "$TEST_TMP/server.log"
        exit 1
    fi
    id gitignore >/dev/null
    [ "$(stat -c %U "$TEST_TMP/data")" = gitignore ]
    [ "$(stat -c %U "$TEST_TMP/database")" = gitignore ]
    echo "PASS: startup creates the fixed service account"

    BASE_URL="http://127.0.0.1:64580"
    FAILED=0

    check() {
        if ! eval "$2" >/dev/null 2>&1; then
            echo "FAILED: $1"
            FAILED=1
            if ! kill -0 "$SERVER_PID" 2>/dev/null; then echo "Server PID $SERVER_PID is no longer running"; fi
            eval "$2" 2>&1 | head -c 500 || true
            echo
        else
            echo "PASS: $1"
        fi
    }

    echo "=== Health Endpoint ==="
    check "GET /server/healthz" "curl -q -LSsf -H \"Accept: text/plain\" ${BASE_URL}/server/healthz | grep -qi Status:"
    check "GET /api/${API_VERSION}/server/healthz" "curl -q -LSsf -A gitignore-cli/1.0 -H \"Accept: application/json\" ${BASE_URL}/api/${API_VERSION}/server/healthz | jq -e .status"
    check "GET /api/${API_VERSION}/server/healthz.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/server/healthz.txt"

    echo "=== Content Negotiation ==="
    check "Accept: application/json /api/${API_VERSION}/" "curl -q -LSsf -H \"Accept: application/json\" ${BASE_URL}/api/${API_VERSION}/ | jq ."
    check "Accept: text/html /" "curl -q -LSsf -H \"Accept: text/html\" ${BASE_URL}/ | grep -qi \"<!DOCTYPE html\""
    check "Accept: text/plain /api/${API_VERSION}/" "curl -q -LSsf -H \"Accept: text/plain\" ${BASE_URL}/api/${API_VERSION}/"

    echo "=== Template API Endpoints ==="
    check "GET /api/${API_VERSION}/list" "curl -q -LSsf -H \"Accept: application/json\" ${BASE_URL}/api/${API_VERSION}/list | jq ."
    check "GET /api/${API_VERSION}/list.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/list.txt"
    check "GET /api/${API_VERSION}/templates/go" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/templates/go"
    check "GET /api/${API_VERSION}/templates/go.txt" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/templates/go.txt"
    check "GET /api/${API_VERSION}/templates/go.json" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/templates/go.json | jq ."
    check "GET /api/${API_VERSION}/categories" "curl -q -LSsf -H \"Accept: application/json\" ${BASE_URL}/api/${API_VERSION}/categories | jq ."
    check "GET /api/${API_VERSION}/search?q=go" "curl -q -LSsf -H \"Accept: application/json\" \"${BASE_URL}/api/${API_VERSION}/search?q=go\" | jq ."
    check "GET /api/${API_VERSION}/combine?templates=go,node" "curl -q -LSsf \"${BASE_URL}/api/${API_VERSION}/combine?templates=go,node\""
    check "GET /api/${API_VERSION}/stats" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/stats | jq ."

    echo "=== gitignore.io Compatibility Layer ==="
    check "GET /api/list" "curl -q -LSsf ${BASE_URL}/api/list"
    check "GET /api/go,node" "curl -q -LSsf ${BASE_URL}/api/go,node | grep -q \"### Go ###\""

    echo "=== Frontend Smart Detection ==="
    check "CLI -> plain text list" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/list.txt"
    check "Browser -> HTML /categories" "curl -q -LSsf -D - -o /dev/null -H \"Accept: text/html\" ${BASE_URL}/categories | grep -qi \"text/html\""

    echo "=== Well-Known / Special Files ==="
    check "GET /robots.txt" "curl -q -LSsf ${BASE_URL}/robots.txt"
    check "GET /.well-known/security.txt" "curl -q -LSsf ${BASE_URL}/.well-known/security.txt"
    check "GET /manifest.json" "curl -q -LSsf ${BASE_URL}/manifest.json | jq ."

    echo "=== Documentation Endpoints ==="
    check "GET /api/${API_VERSION}/server/swagger" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/server/swagger | jq ."
    GRAPHQL_PAYLOAD=$(printf "%s" "{\"query\":\"{ __typename }\"}" | base64)
    check "POST /api/${API_VERSION}/server/graphql" "echo $GRAPHQL_PAYLOAD | base64 -d | curl -q -LSsf -X POST -H \"Content-Type: application/json\" --data-binary @- ${BASE_URL}/api/${API_VERSION}/server/graphql | jq -e .data"
    GRAPHQL_GET_QUERY=$(printf "%s" "{ __typename }" | base64)
    check "GET /api/${API_VERSION}/server/graphql?query=..." "printf %s $GRAPHQL_GET_QUERY | base64 -d | curl -q -LSsf --get --data-urlencode query@- ${BASE_URL}/api/${API_VERSION}/server/graphql | jq -e .data"
    check "GET /api/${API_VERSION}/server/graphql usage" "curl -q -LSsf ${BASE_URL}/api/${API_VERSION}/server/graphql | grep -qi parameter"

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
    kill "$SERVER_PID" 2>/dev/null || true
    wait $SERVER_PID 2>/dev/null || true

    if [ "$FAILED" -ne 0 ]; then
        echo "=== Server Log ==="
        cat "$TEST_TMP/server.log"
        echo "=== One or more tests FAILED ==="
        exit 1
    fi

    echo "=== All tests passed ==="
'

echo "Docker tests completed successfully"
