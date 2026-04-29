#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

go_source_hash() {
  local hash_cmd=(sha256sum)
  command -v sha256sum > /dev/null || hash_cmd=(shasum -a 256)
  find "$@" -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -print0 |
    sort -z | xargs -0 "${hash_cmd[@]}" | "${hash_cmd[@]}" | awk '{print $1}'
}

build_cached_go() {
  local src_dir="${1:?src dir required}"
  local bin="${2:?bin required}"
  shift 2
  local source_hash
  source_hash="$(go_source_hash "$@")"
  local stamp="${bin}.stamp"
  if [[ -x "$bin" && -f "$stamp" && ! "$bin" -nt "$stamp" &&
    "$(cat "$stamp")" == "$source_hash" ]]; then
    return
  fi

  local tmp_bin="${bin}.tmp.$$"
  local build_output
  build_output="$(mktemp)"
  rm -f "$tmp_bin"
  if ! go build -C "$src_dir" -buildvcs=false -o "$tmp_bin" . > "$build_output" 2>&1; then
    report_go_build_failure "$ROOT" "$src_dir" "$bin" "$build_output"
    rm -f "$tmp_bin" "$build_output"
    exit 127
  fi
  rm -f "$build_output"
  mv -f "$tmp_bin" "$bin"
  printf '%s\n' "$source_hash" > "$stamp"
}

build_go_binary() {
  build_cached_go "${1:?src dir required}" "${2:?bin required}" "$1"
}

build_policy_tool() {
  local name="${1:?tool name required}"
  local src_dir="${TOOLS_SRC_DIR}/cmd/${name}"
  build_cached_go "$src_dir" "${TOOLS_BIN_DIR}/${name}" "$src_dir" "${TOOLS_SRC_DIR}/internal"
}
