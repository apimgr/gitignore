#!/usr/bin/env bash
# @@License : WTFPL
set -euo pipefail

PROJECT_NAME=gitignore
PROJECT_ORG=apimgr
PROJECT_DIR=$(git rev-parse --show-toplevel)
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

RANDOM_SUFFIX=$(printf '%04x%04x' "$RANDOM" "$RANDOM")
CONTAINER_NAME="${PROJECT_NAME}-e2e-${RANDOM_SUFFIX}"

docker run --rm \
  --name "$CONTAINER_NAME" \
  --volume "$PROJECT_DIR:/app:ro" \
  --volume "$TEMP_DIR:/tmp/apimgr/gitignore-e2e:rw" \
  --workdir /app \
  --env PROJECT_E2E_TMP=/tmp/apimgr/gitignore-e2e \
  --env TMPDIR=/tmp/apimgr/gitignore-e2e \
  casjaysdev/go:latest sh -ec '
    apk add --no-cache chromium su-exec >/dev/null
    mkdir -p /tmp/apimgr/gitignore-e2e/home
    adduser -D -h /tmp/apimgr/gitignore-e2e/home gitignore
    chown -R gitignore:gitignore /tmp/apimgr/gitignore-e2e
    exec su-exec gitignore:gitignore sh -ec "
      export HOME=/tmp/apimgr/gitignore-e2e/home
      export GOCACHE=/tmp/apimgr/gitignore-e2e/go-build
      export GOMODCACHE=/tmp/apimgr/gitignore-e2e/go-mod
      go mod download
      export GOPROXY=off
      export HTTP_PROXY=http://127.0.0.1:1
      export HTTPS_PROXY=http://127.0.0.1:1
      export ALL_PROXY=http://127.0.0.1:1
      export http_proxy=http://127.0.0.1:1
      export https_proxy=http://127.0.0.1:1
      export all_proxy=http://127.0.0.1:1
      export NO_PROXY=127.0.0.1,localhost
      export no_proxy=127.0.0.1,localhost
      go test -buildvcs=false -tags e2e -count=1 ./tests/e2e/...
    "
  '
