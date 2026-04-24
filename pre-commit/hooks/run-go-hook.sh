#!/usr/bin/env bash
# Build and run the cached Go hook helper.

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
GIT_COMMON_DIR="$(git rev-parse --path-format=absolute --git-common-dir)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ETHOS_ROOT="$(cd "${BUNDLE_ROOT}/.." && pwd)"
SRC_DIR="${BUNDLE_ROOT}/hooks/go-hooks"
BIN_DIR="${GIT_COMMON_DIR}/coding-ethos-hooks"
BIN="${BIN_DIR}/coding-ethos-hook"

cd "$ROOT"
mkdir -p "$BIN_DIR"

needs_build=false
if [[ ! -x "$BIN" ]]; then
    needs_build=true
elif find "$SRC_DIR" -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -newer "$BIN" | grep -q . || [[ "${ETHOS_ROOT}/config.yaml" -nt "$BIN" ]]; then
    needs_build=true
fi

if [[ "$needs_build" == true ]]; then
    TMP_BIN="${BIN}.tmp.$$"
    trap 'rm -f "$TMP_BIN"' EXIT
    go build -C "$SRC_DIR" -o "$TMP_BIN" .
    mv -f "$TMP_BIN" "$BIN"
    trap - EXIT
fi

exec "$BIN" "$@"
