#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

CAPTURED_LINT_TOOLS=(
  ruff
  mypy
  pyright
  pylint
  shellcheck
  golangci-lint
  actionlint
  yamllint
  hadolint
)

real_tool_env_var() {
  local tool="${1:?tool required}"
  case "$tool" in
    golangci-lint)
      printf 'CODING_ETHOS_REAL_GOLANGCI_LINT\n'
      ;;
    *)
      printf 'CODING_ETHOS_REAL_%s\n' "${tool^^}" | tr '-' '_'
      ;;
  esac
}

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

resolved_real_tool_path() {
  local tool="${1:?tool required}"
  local env_name
  local real_tool
  env_name="$(real_tool_env_var "$tool")"
  real_tool="${!env_name:-}"

  if [[ -z "$real_tool" || ! -x "$real_tool" || "$real_tool" == "${TOOLS_BIN_DIR}/${tool}" ]]; then
    real_tool="$(real_tool_path "$tool" || true)"
  fi

  printf '%s\n' "$real_tool"
}

install_lint_tool_shims() {
  local tool
  for tool in "${CAPTURED_LINT_TOOLS[@]}"; do
    install_lint_tool_shim "$tool"
  done
}

install_lint_tool_shim() {
  local tool="${1:?tool required}"
  local real_tool
  real_tool="$(resolved_real_tool_path "$tool")"
  if [[ -z "$real_tool" ]]; then
    return
  fi

  local env_name
  local shim
  local tmp_shim
  env_name="$(real_tool_env_var "$tool")"
  shim="${TOOLS_BIN_DIR}/${tool}"
  tmp_shim="${shim}.tmp.$$"
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'export %s=%s\n' "$env_name" "$(shell_quote "$real_tool")"
    printf 'exec %s policy-tool %s "$@"\n' \
      "$(shell_quote "${SCRIPT_DIR}/run-go-hook.sh")" \
      "$(shell_quote "$tool")"
  } > "$tmp_shim"
  chmod +x "$tmp_shim"
  mv -f "$tmp_shim" "$shim"
}

run_policy_tool() {
  local tool_name="${1:-}"
  shift || true

  if ! captured_lint_tool "$tool_name"; then
    printf 'FATAL: unknown policy tool %q\n' "$tool_name" >&2
    exit 2
  fi

  run_captured_lint_tool "$tool_name" "$@"
}

captured_lint_tool() {
  local tool="${1:?tool required}"
  local known
  for known in "${CAPTURED_LINT_TOOLS[@]}"; do
    [[ "$known" == "$tool" ]] && return 0
  done

  return 1
}

run_captured_lint_tool() {
  local tool="${1:?tool required}"
  shift || true

  local real_tool
  real_tool="$(resolved_real_tool_path "$tool")"
  if [[ -z "$real_tool" ]]; then
    printf 'FATAL: real %s binary not found for lint capture\n' "$tool" >&2
    exit 127
  fi

  build_policy_tool coding-ethos-lint
  exec "${TOOLS_BIN_DIR}/coding-ethos-lint" \
    --capture-tool "$tool" \
    --tool-path "$real_tool" \
    --cwd "$ROOT" \
    "$@"
}
