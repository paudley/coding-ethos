#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

needs_policy_compile() {
  [[ ! -f "$POLICY_BUNDLE" ]] && return 0
  for source in \
    "${ETHOS_ROOT}/coding_ethos.yml" \
    "${ETHOS_ROOT}/repo_ethos.yml" \
    "${ETHOS_ROOT}/config.yaml" \
    "${ROOT}/repo_config.yaml" \
    "${ROOT}/repo_config.yml"; do
    [[ -f "$source" && "$source" -nt "$POLICY_BUNDLE" ]] && return 0
  done
  return 1
}

runtime_failure() {
  local message="${1:?message required}"
  {
    printf 'FATAL: coding-ethos hook runtime is not installed or is stale\n'
    printf 'This is not caused by the files being committed. Hook execution does not rebuild protected runtime artifacts.\n'
    printf 'repo: %s\n' "$ROOT"
    printf 'bundle_root: %s\n' "$BUNDLE_ROOT"
    printf 'runtime_dir: %s\n' "$BIN_DIR"
    printf 'problem: %s\n' "$message"
    printf 'action: run make build from the coding-ethos repository, or ask an admin to update the installed hook runtime. Do not rebuild protected files by hand.\n'
  } >&2
  exit 127
}

require_runtime_file() {
  local path="${1:?path required}"
  local description="${2:?description required}"
  if [[ ! -e "$path" ]]; then
    runtime_failure "missing ${description}: ${path}"
  fi
}

require_runtime_binary() {
  local path="${1:?path required}"
  local description="${2:?description required}"
  if [[ ! -x "$path" ]]; then
    runtime_failure "missing or non-executable ${description}: ${path}"
  fi
}

require_policy_bundle() {
  require_runtime_file "$POLICY_BUNDLE" "compiled policy bundle"
  if needs_policy_compile; then
    runtime_failure "compiled policy bundle is older than its source configuration: ${POLICY_BUNDLE}"
  fi
}

require_policy_tool() {
  local name="${1:?tool name required}"
  require_runtime_binary "${TOOLS_BIN_DIR}/${name}" "$name"
}

compile_policy_bundle() {
  if [[ ! -d "$TOOLS_SRC_DIR" ]]; then
    echo "FATAL: policy tool source not found at ${TOOLS_SRC_DIR}" >&2
    exit 127
  fi
  build_policy_tool coding-ethos-policy
  if needs_policy_compile; then
    local -a args=(
      compile
      --primary "${ETHOS_ROOT}/coding_ethos.yml"
      --config "${ETHOS_ROOT}/config.yaml"
      --out-dir "$POLICY_DIR"
    )
    if [[ -f "${ROOT}/repo_config.yaml" ]]; then
      args+=(--repo-config "${ROOT}/repo_config.yaml")
    elif [[ -f "${ROOT}/repo_config.yml" ]]; then
      args+=(--repo-config "${ROOT}/repo_config.yml")
    fi
    "${TOOLS_BIN_DIR}/coding-ethos-policy" "${args[@]}" > /dev/null
  fi
}

install_runtime_artifacts() {
  compile_policy_bundle
  build_policy_tool coding-ethos-lint
  build_policy_tool coding-ethos-hook
  build_policy_tool coding-ethos-git
  build_policy_tool coding-ethos-git-hook
  build_policy_tool coding-ethos-agent-hooks
  build_go_binary "$GIT_HOOK_SRC_DIR" "$GIT_HOOK_BIN"
  install_git_wrapper_shim
  install_lint_tool_shims
}
