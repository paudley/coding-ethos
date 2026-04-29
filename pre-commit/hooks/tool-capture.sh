#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

real_tool_path() {
  local tool="${1:?tool required}"
  local path_entry
  local candidate
  local -a path_entries

  IFS=':' read -r -a path_entries <<< "${PATH:-}"
  for path_entry in "${path_entries[@]}"; do
    [[ -z "$path_entry" || "$path_entry" == "$TOOLS_BIN_DIR" ]] && continue
    candidate="${path_entry}/${tool}"
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

install_lint_tool_shims() {
  local real_ruff="${CODING_ETHOS_REAL_RUFF:-}"
  if [[ -z "$real_ruff" || ! -x "$real_ruff" || "$real_ruff" == "${TOOLS_BIN_DIR}/ruff" ]]; then
    real_ruff="$(real_tool_path ruff || true)"
  fi
  if [[ -z "$real_ruff" ]]; then
    return
  fi

  local shim="${TOOLS_BIN_DIR}/ruff"
  local tmp_shim="${shim}.tmp.$$"
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'export CODING_ETHOS_REAL_RUFF=%s\n' "$(shell_quote "$real_ruff")"
    printf 'exec %s policy-tool ruff "$@"\n' "$(shell_quote "${SCRIPT_DIR}/run-go-hook.sh")"
  } > "$tmp_shim"
  chmod +x "$tmp_shim"
  mv -f "$tmp_shim" "$shim"
}

run_policy_tool() {
  local tool_name="${1:-}"
  shift || true

  case "$tool_name" in
    ruff)
      run_policy_ruff "$@"
      ;;
    *)
      printf 'FATAL: unknown policy tool %q\n' "$tool_name" >&2
      exit 2
      ;;
  esac
}

run_policy_ruff() {
  local real_ruff="${CODING_ETHOS_REAL_RUFF:-}"
  if [[ -z "$real_ruff" || ! -x "$real_ruff" || "$real_ruff" == "${TOOLS_BIN_DIR}/ruff" ]]; then
    real_ruff="$(real_tool_path ruff || true)"
  fi
  if [[ -z "$real_ruff" ]]; then
    printf 'FATAL: real ruff binary not found for lint capture\n' >&2
    exit 127
  fi

  build_policy_tool coding-ethos-lint
  exec "${TOOLS_BIN_DIR}/coding-ethos-lint" \
    --capture-tool ruff \
    --tool-path "$real_ruff" \
    --cwd "$ROOT" \
    "$@"
}
