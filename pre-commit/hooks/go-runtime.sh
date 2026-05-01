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
    printf 'FATAL: coding-ethos hook runtime is missing or invalid\n'
    printf 'This is not caused by the files being committed.\n'
    printf 'repo: %s\n' "$ROOT"
    printf 'bundle_root: %s\n' "$BUNDLE_ROOT"
    printf 'runtime_dir: %s\n' "$BIN_DIR"
    printf 'problem: %s\n' "$message"
    printf 'action: run make -C %q build, or ask an admin to repair the coding-ethos checkout.\n' "$ETHOS_ROOT"
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
}

require_fresh_policy_bundle() {
  require_policy_bundle
  require_runtime_file "$POLICY_METADATA" "compiled policy metadata"

  local python_bin
  if command -v python3 > /dev/null 2>&1; then
    python_bin=python3
  elif command -v python > /dev/null 2>&1; then
    python_bin=python
  else
    runtime_failure "python is required for strict policy metadata validation"
  fi

  if ! "$python_bin" - "$POLICY_METADATA" << 'PY'; then
from __future__ import annotations

import hashlib
import json
import sys
from pathlib import Path

metadata_path = Path(sys.argv[1])
metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
source_hashes = metadata.get("source_hashes") or {}
if not isinstance(source_hashes, dict) or not source_hashes:
    raise SystemExit(f"{metadata_path} does not contain source_hashes")

errors: list[str] = []
for raw_path, expected in sorted(source_hashes.items()):
    path = Path(raw_path)
    if not path.is_file():
        errors.append(f"missing policy source: {path}")
        continue
    actual = "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
    if actual != expected:
        errors.append(f"policy source hash mismatch: {path}")

if errors:
    raise SystemExit("\n".join(errors))
PY
    runtime_failure "compiled policy bundle does not match its source hash manifest: ${POLICY_METADATA}"
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

runtime_artifacts_missing() {
  [[ ! -x "$GIT_HOOK_BIN" ]] && return 0
  [[ ! -f "$POLICY_BUNDLE" ]] && return 0

  local tool
  for tool in \
    coding-ethos-agent-hooks \
    coding-ethos-policy \
    coding-ethos-lint \
    coding-ethos-hook \
    coding-ethos-hook-log \
    coding-ethos-mcp \
    coding-ethos-git-hook \
    coding-ethos-git; do
    [[ ! -x "${TOOLS_BIN_DIR}/${tool}" ]] && return 0
  done
  [[ ! -f "$MANAGED_TOOLCHAIN_MANIFEST" ]] && return 0
  for tool in \
    "${MANAGED_GO_BIN_DIR}/shfmt" \
    "${MANAGED_GO_BIN_DIR}/golangci-lint" \
    "${MANAGED_GITHUB_BIN_DIR}/shellcheck" \
    "${MANAGED_GITHUB_BIN_DIR}/actionlint" \
    "${MANAGED_GITHUB_BIN_DIR}/hadolint" \
    "${MANAGED_GITHUB_BIN_DIR}/dotenv-linter"; do
    [[ ! -x "$tool" ]] && return 0
  done

  return 1
}

run_bootstrap_build() {
  local build_output
  build_output="$(mktemp)"
  set +e
  CODING_ETHOS_BOOTSTRAP=1 CODE_ETHOS_CONSUMER_ROOT="$ROOT" \
    make -C "$ETHOS_ROOT" build > "$build_output" 2>&1
  local status=$?
  set -e

  if [[ "$status" -ne 0 ]]; then
    {
      printf 'FATAL: coding-ethos runtime bootstrap failed\n'
      printf 'repo: %s\n' "$ROOT"
      printf 'coding_ethos_root: %s\n' "$ETHOS_ROOT"
      printf 'command: CODE_ETHOS_CONSUMER_ROOT=%q make -C %q build\n' "$ROOT" "$ETHOS_ROOT"
      printf 'build output:\n'
      cat "$build_output"
    } >&2
    rm -f "$build_output"
    exit "$status"
  fi

  rm -f "$build_output"
}

bootstrap_runtime_if_missing() {
  if ! runtime_artifacts_missing; then
    return
  fi

  if [[ -n "${CODING_ETHOS_BOOTSTRAP:-}" ]]; then
    runtime_failure "runtime artifacts are missing while bootstrap is already active"
  fi

  local lock_dir="${ETHOS_ROOT}/build/runtime"
  local lock_file="${lock_dir}/bootstrap.lock"
  mkdir -p "$lock_dir"

  if command -v flock > /dev/null 2>&1; then
    (
      flock 9
      if runtime_artifacts_missing; then
        run_bootstrap_build
      fi
    ) 9> "$lock_file"
  else
    run_bootstrap_build
  fi

  if runtime_artifacts_missing; then
    runtime_failure "runtime artifacts are missing after bootstrap build"
  fi
}

install_runtime_artifacts() {
  compile_policy_bundle
  build_policy_tool coding-ethos-lint
  build_policy_tool coding-ethos-hook
  build_policy_tool coding-ethos-hook-log
  build_policy_tool coding-ethos-mcp
  build_policy_tool coding-ethos-git
  build_policy_tool coding-ethos-git-hook
  build_policy_tool coding-ethos-agent-hooks
  build_go_binary "$GIT_HOOK_SRC_DIR" "$GIT_HOOK_BIN"
  install_git_wrapper_shim
  install_lint_tool_shims
}
