#!/usr/bin/env bash
# GitIgnore API Server - Backup wrapper
#
# This is a thin wrapper around the server binary's built-in backup command.
# The binary is the single source of truth for backup format: it writes a
# gzip tar containing an embedded manifest.json with per-file SHA-256
# checksums, and encrypts with AES-256-GCM (Argon2id-derived key) when
# backup encryption is enabled in server.yml. Never hand-roll a tar backup
# here - formats must not diverge, and a raw `tar -C /` restore is unsafe.

set -euo pipefail

BACKUP_SCRIPTNAME="$(basename "$0")"

# Resolve the gitignore binary: prefer one on PATH, then common install paths.
__backup_find_binary() {
  if command -v gitignore >/dev/null 2>&1; then
    command -v gitignore
    return 0
  fi
  local candidate
  for candidate in /usr/local/bin/gitignore /usr/bin/gitignore ./gitignore; do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

__backup_main() {
  local binary
  if ! binary="$(__backup_find_binary)"; then
    printf '%s: gitignore binary not found on PATH or in standard locations\n' \
      "$BACKUP_SCRIPTNAME" >&2
    exit 1
  fi

  # Pass any extra arguments (e.g. an explicit output filename) straight
  # through to the binary's maintenance backup command.
  exec "$binary" --maintenance backup "$@"
}

__backup_main "$@"
