#!/usr/bin/env bash
# @@License : WTFPL
set -euo pipefail

PROJECT_DIR=$(git rev-parse --show-toplevel)
PROJECT_NAME=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_name:[[:space:]]*/{sub(/^project_name:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
PROJECT_ORG=$(awk '/^## Project variables/{inside=1; next} /^## /{inside=0} inside && /^project_org:[[:space:]]*/{sub(/^project_org:[[:space:]]*/, ""); print; exit}' "$PROJECT_DIR/IDEA.md")
REMOTE=$(git -C "$PROJECT_DIR" remote get-url origin 2>/dev/null || true)
if [ -z "$PROJECT_NAME" ]; then PROJECT_NAME=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)(\.git)?$|\1|'); [ -n "$PROJECT_NAME" ] || PROJECT_NAME=$(basename "$PROJECT_DIR"); fi
if [ -z "$PROJECT_ORG" ]; then PROJECT_ORG=$(printf '%s\n' "$REMOTE" | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$|\1|'); [ -n "$PROJECT_ORG" ] || PROJECT_ORG=$(basename "$(dirname "$PROJECT_DIR")"); fi
TEMP_PARENT="${TMPDIR:-/tmp}/${PROJECT_ORG}"
mkdir -p "$TEMP_PARENT"
TEMP_DIR=$(mktemp -d "$TEMP_PARENT/${PROJECT_NAME}-e2e-XXXXXXXX")
chmod 700 "$TEMP_DIR"
cleanup() {
  result=$?
  if (( result == 0 )); then
    rm -rf -- "$TEMP_DIR"
  else
    printf 'E2E artifacts retained at %s\n' "$TEMP_DIR" >&2
  fi
  return "$result"
}
trap cleanup EXIT

CONTAINER_NAME="${PROJECT_NAME}-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
E2E_IMAGE="${PROJECT_E2E_IMAGE:-casjaysdev/go:latest}"

docker run --rm \
  --name "$CONTAINER_NAME" \
  --volume "$PROJECT_DIR:/app:ro" \
  --volume "$TEMP_DIR:/tmp/apimgr/gitignore-e2e:rw" \
  --workdir /app \
  --env PROJECT_E2E_TMP=/tmp/apimgr/gitignore-e2e \
  --env TMPDIR=/tmp/apimgr/gitignore-e2e \
  --env PROJECT_E2E_TEST_FILTER="${PROJECT_E2E_TEST_FILTER:-}" \
  "$E2E_IMAGE" sh -ec '
    if ! command -v chromium-browser >/dev/null 2>&1 && ! command -v chromium >/dev/null 2>&1; then
      apk add --no-cache --force-overwrite chromium su-exec >/dev/null
    fi
    mkdir -p /tmp/apimgr/gitignore-e2e/home
    adduser -D -h /tmp/apimgr/gitignore-e2e/home gitignore
    chown -R gitignore:gitignore /tmp/apimgr/gitignore-e2e
    exec su-exec gitignore:gitignore sh -ec "
      export HOME=/tmp/apimgr/gitignore-e2e/home
      export GOCACHE=/tmp/apimgr/gitignore-e2e/go-build
      export GOMODCACHE=/tmp/apimgr/gitignore-e2e/go-mod
      go mod download
      go build -buildvcs=false -trimpath -o /tmp/apimgr/gitignore-e2e/gitignore-server ./src
      export PROJECT_E2E_SERVER_BINARY=/tmp/apimgr/gitignore-e2e/gitignore-server
      export GOPROXY=off
      export HTTP_PROXY=http://127.0.0.1:1
      export HTTPS_PROXY=http://127.0.0.1:1
      export ALL_PROXY=http://127.0.0.1:1
      export http_proxy=http://127.0.0.1:1
      export https_proxy=http://127.0.0.1:1
      export all_proxy=http://127.0.0.1:1
      export NO_PROXY=127.0.0.1,localhost
      export no_proxy=127.0.0.1,localhost
      if [ -n \"\$PROJECT_E2E_TEST_FILTER\" ]; then
        go test -buildvcs=false -tags e2e -count=1 -run \"\$PROJECT_E2E_TEST_FILTER\" ./tests/e2e/...
      else
        go test -buildvcs=false -tags e2e -count=1 ./tests/e2e/...
      fi
    "
  '
