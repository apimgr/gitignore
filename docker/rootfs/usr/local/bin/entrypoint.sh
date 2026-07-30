#!/usr/bin/env bash
set -eo pipefail

# =============================================================================
# Container Entrypoint Script - MINIMAL
# Only: set env, build flags, exec binary.
# The binary handles EVERYTHING else: directories, permissions, user/group,
# Tor, and all runtime initialization (see PART 26 / PART 31).
# =============================================================================

APP_NAME="gitignore"
APP_BIN="/usr/local/bin/${APP_NAME}"

# Export environment defaults (the binary reads these)
export TZ="${TZ:-America/New_York}"
export CONFIG_DIR="${CONFIG_DIR:-/config/${APP_NAME}}"
export DATA_DIR="${DATA_DIR:-/data/${APP_NAME}}"

log() { echo "[entrypoint] $(date '+%Y-%m-%dT%H:%M:%S%z') $*"; }

log "Starting ${APP_NAME}..."

# Build flags from environment
FLAGS="--address ${ADDRESS:-0.0.0.0} --port ${PORT:-80}"
[[ "${DEBUG:-false}" == "true" ]] && FLAGS="$FLAGS --debug"

# Exec the binary so it replaces this shell as PID 1 and receives signals
# directly from tini. The binary handles all setup: dirs, perms, user/group,
# Tor, etc.
exec "$APP_BIN" $FLAGS "$@"
