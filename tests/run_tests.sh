#!/usr/bin/env bash
# @@License : WTFPL
# GitIgnore API Server - Docker test runner

set -eo pipefail

if ! command -v docker &>/dev/null; then
    echo "ERROR: Docker is required for tests/run_tests.sh" >&2
    exit 69
fi
exec "$(dirname "$0")/docker.sh"
