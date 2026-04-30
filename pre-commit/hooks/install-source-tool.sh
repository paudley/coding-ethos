#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

usage() {
  printf 'Usage: %s go <module@version> | rust <crate@version> <binary>\n' "$0" >&2
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

install_rust_tool() {
  local crate_spec="${1:-}"
  local binary="${2:-}"
  [[ -n "$crate_spec" && -n "$binary" ]] || usage
  [[ -n "${GOBIN:-}" ]] || {
    printf 'FATAL: GOBIN must point at the managed coding-ethos bin directory\n' >&2
    exit 2
  }

  local crate="${crate_spec%@*}"
  local version="${crate_spec##*@}"
  if [[ "$crate" == "$version" ]]; then
    printf 'FATAL: Rust crate spec must include @version: %s\n' "$crate_spec" >&2
    exit 2
  fi

  local work_dir
  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' RETURN
  cargo install "$crate" --version "$version" --locked --root "$work_dir"
  mkdir -p "$GOBIN"
  install -m 0755 "${work_dir}/bin/${binary}" "${GOBIN}/${binary}"
}

case "${1:-}" in
  go)
    shift
    install_go_tool "$@"
    ;;
  rust)
    shift
    install_rust_tool "$@"
    ;;
  *)
    usage
    ;;
esac
