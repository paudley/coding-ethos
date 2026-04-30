#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

usage() {
  printf 'Usage: %s go <module@version>\n' "$0" >&2
  exit 2
}

install_go_tool() {
  local module="${1:-}"
  [[ -n "$module" ]] || usage
  [[ -n "${GOBIN:-}" ]] || {
    printf 'FATAL: GOBIN must point at the managed coding-ethos Go bin directory\n' >&2
    exit 2
  }

  mkdir -p "$GOBIN"
  go install "$module"
}

case "${1:-}" in
  go)
    shift
    install_go_tool "$@"
    ;;
  *)
    usage
    ;;
esac
