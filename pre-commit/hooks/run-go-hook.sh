#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

# Build and run cached Go hook helpers.

set -euo pipefail

REAL_GIT="${CODING_ETHOS_REAL_GIT:-/usr/bin/git}"
ROOT="$("$REAL_GIT" rev-parse --show-toplevel)"
GIT_COMMON_DIR="$("$REAL_GIT" rev-parse --path-format=absolute --git-common-dir)"
HOOKS_DIR="$("$REAL_GIT" rev-parse --path-format=absolute --git-path hooks)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ETHOS_ROOT="$(cd "${BUNDLE_ROOT}/.." && pwd)"
# shellcheck source=pre-commit/hooks/go-build-report.sh
source "${SCRIPT_DIR}/go-build-report.sh"
# shellcheck source=pre-commit/hooks/go-build-cache.sh
source "${SCRIPT_DIR}/go-build-cache.sh"
# shellcheck source=pre-commit/hooks/tool-capture.sh
source "${SCRIPT_DIR}/tool-capture.sh"
if [[ -z "${CODE_ETHOS_CONSUMER_ROOT:-}" && "${ROOT}" == "${ETHOS_ROOT}" ]]; then
  export CODE_ETHOS_CONSUMER_ROOT="${ROOT}"
fi
BIN_DIR="${GIT_COMMON_DIR}/coding-ethos-hooks"
GIT_HOOK_SRC_DIR="${BUNDLE_ROOT}/hooks/go-hooks"
GIT_HOOK_BIN="${BIN_DIR}/coding-ethos-git-hook"
TOOLS_SRC_DIR="${ETHOS_ROOT}/go"
TOOLS_BIN_DIR="${BIN_DIR}/bin"
POLICY_DIR="${BIN_DIR}/policy"
POLICY_BUNDLE="${POLICY_DIR}/policy-bundle.json"

start_hook_log() {
  if [[ -n "${CODE_ETHOS_HOOK_LOGGING_ACTIVE:-}" ]]; then
    return
  fi
  if [[ "${1:-}" == "cutover" ]]; then
    return
  fi

  local required_ignore
  for required_ignore in ".coding-ethos/" ".coding-ethos/hook-runs/example/stdout.log"; do
    if ! "$REAL_GIT" -C "$ROOT" check-ignore --quiet "$required_ignore"; then
      printf 'FATAL: %s is not ignored; add .coding-ethos/ to the repo .gitignore before hook logs are written\n' "$required_ignore" >&2
      exit 1
    fi
  done

  local log_root="${ROOT}/.coding-ethos/hook-runs"
  local timestamp
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  local run_id="${timestamp}-$PPID-$$"
  local run_dir="${log_root}/${run_id}"
  mkdir -p "$run_dir"

  local stdout_log="${run_dir}/stdout.log"
  local stderr_log="${run_dir}/stderr.log"
  local metadata_log="${run_dir}/metadata.env"
  {
    printf 'run_id=%q\n' "$run_id"
    printf 'started_at_utc=%q\n' "$timestamp"
    printf 'repo_root=%q\n' "$ROOT"
    printf 'bundle_root=%q\n' "$BUNDLE_ROOT"
    printf 'git_common_dir=%q\n' "$GIT_COMMON_DIR"
    printf 'command=%q' "$0"
    printf ' %q' "$@"
    printf '\n'
  } > "$metadata_log"

  set +e
  CODE_ETHOS_HOOK_LOGGING_ACTIVE=1 "$0" "$@" \
    > >(tee -a "$stdout_log") \
    2> >(tee -a "$stderr_log" >&2)
  local status=$?
  set -e

  {
    printf 'finished_at_utc=%q\n' "$(date -u +%Y%m%dT%H%M%SZ)"
    printf 'exit_code=%q\n' "$status"
  } >> "$metadata_log"

  exit "$status"
}

start_hook_log "$@"

cd "$ROOT"
mkdir -p "$BIN_DIR" "$TOOLS_BIN_DIR"

shell_quote() {
  printf "'%s'" "${1//\'/\'\\\'\'}"
}

install_git_wrapper_shim() {
  local shim="${TOOLS_BIN_DIR}/git"
  local tmp_shim="${shim}.tmp.$$"
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'export CODING_ETHOS_REAL_GIT=%s\n' "$(shell_quote "$REAL_GIT")"
    printf 'exec %s policy-git "$@"\n' "$(shell_quote "${SCRIPT_DIR}/run-go-hook.sh")"
  } > "$tmp_shim"
  chmod +x "$tmp_shim"
  mv -f "$tmp_shim" "$shim"
}

install_git_hook_shims() {
  mkdir -p "$HOOKS_DIR"

  local hook
  for hook in pre-commit pre-push commit-msg; do
    cp "${SCRIPT_DIR}/run-git-hook.sh" "${HOOKS_DIR}/${hook}"
    chmod +x "${HOOKS_DIR}/${hook}"
  done

  for hook in post-commit post-merge post-checkout; do
    cp "${SCRIPT_DIR}/run-lfs-hook.sh" "${HOOKS_DIR}/${hook}"
    chmod +x "${HOOKS_DIR}/${hook}"
  done
}

verify_git_hook_shims() {
  local hook
  for hook in pre-commit pre-push commit-msg; do
    if [[ ! -x "${HOOKS_DIR}/${hook}" ]] ||
      ! cmp -s "${SCRIPT_DIR}/run-git-hook.sh" "${HOOKS_DIR}/${hook}"; then
      return 1
    fi
  done

  for hook in post-commit post-merge post-checkout; do
    if [[ ! -x "${HOOKS_DIR}/${hook}" ]] ||
      ! cmp -s "${SCRIPT_DIR}/run-lfs-hook.sh" "${HOOKS_DIR}/${hook}"; then
      return 1
    fi
  done
}

git_hook_fix_items() {
  local hook
  for hook in pre-commit pre-push commit-msg; do
    if [[ ! -x "${HOOKS_DIR}/${hook}" ]]; then
      printf '  git-hooks,%s missing or not executable,run cutover install\n' "${HOOKS_DIR}/${hook}"
    elif ! cmp -s "${SCRIPT_DIR}/run-git-hook.sh" "${HOOKS_DIR}/${hook}"; then
      printf '  git-hooks,%s is stale,run cutover install\n' "${HOOKS_DIR}/${hook}"
    fi
  done

  for hook in post-commit post-merge post-checkout; do
    if [[ ! -x "${HOOKS_DIR}/${hook}" ]]; then
      printf '  git-hooks,%s missing or not executable,run cutover install\n' "${HOOKS_DIR}/${hook}"
    elif ! cmp -s "${SCRIPT_DIR}/run-lfs-hook.sh" "${HOOKS_DIR}/${hook}"; then
      printf '  git-hooks,%s is stale,run cutover install\n' "${HOOKS_DIR}/${hook}"
    fi
  done
}

persist_agent_environment() {
  if [[ -z "${CLAUDE_ENV_FILE:-}" ]]; then
    return
  fi

  install_git_wrapper_shim
  {
    printf 'export CODING_ETHOS_REAL_GIT=%q\n' "$REAL_GIT"
    printf 'export CODING_ETHOS_RUN_GO_HOOK=%q\n' "${SCRIPT_DIR}/run-go-hook.sh"
    # shellcheck disable=SC2016
    printf 'export PATH=%q:"$PATH"\n' "$TOOLS_BIN_DIR"
  } >> "$CLAUDE_ENV_FILE"
}

has_arg() {
  local needle="${1:?arg required}"
  shift

  local arg
  for arg in "$@"; do
    [[ "$arg" == "$needle" ]] && return 0
  done

  return 1
}

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

run_agent_hook() {
  compile_policy_bundle
  install_git_wrapper_shim
  install_lint_tool_shims
  persist_agent_environment
  build_policy_tool coding-ethos-hook
  export CODING_ETHOS_RUN_GO_HOOK="${SCRIPT_DIR}/run-go-hook.sh"
  export CODING_ETHOS_GIT_SHIM_DIR="$TOOLS_BIN_DIR"
  exec "${TOOLS_BIN_DIR}/coding-ethos-hook" --bundle "$POLICY_BUNDLE" --json "$@"
}

run_policy_lint() {
  compile_policy_bundle
  build_policy_tool coding-ethos-lint
  exec "${TOOLS_BIN_DIR}/coding-ethos-lint" --bundle "$POLICY_BUNDLE" "$@"
}

run_policy_lint_check() {
  build_policy_tool coding-ethos-lint
  local output
  set +e
  output="$("${TOOLS_BIN_DIR}/coding-ethos-lint" \
    --bundle "$POLICY_BUNDLE" \
    --json \
    "$@" 2>&1)"
  local status=$?
  set -e

  if [[ "$status" -eq 0 ]]; then
    return
  fi

  printf '%s\n' "$output" >&2
  exit "$status"
}

run_policy_git() {
  compile_policy_bundle
  install_git_wrapper_shim
  build_policy_tool coding-ethos-git
  exec "${TOOLS_BIN_DIR}/coding-ethos-git" --bundle "$POLICY_BUNDLE" "$@"
}

run_agent_hooks() {
  install_git_wrapper_shim
  install_lint_tool_shims
  build_policy_tool coding-ethos-agent-hooks
  if ! has_arg --hook-command "$@"; then
    set -- "$@" --hook-command "PATH=${TOOLS_BIN_DIR}:\$PATH ${SCRIPT_DIR}/run-go-hook.sh agent-hook"
  fi
  exec "${TOOLS_BIN_DIR}/coding-ethos-agent-hooks" "$@"
}

agent_hook_command() {
  printf 'PATH=%s:$%s %s agent-hook' "$TOOLS_BIN_DIR" PATH "${SCRIPT_DIR}/run-go-hook.sh"
}

run_agent_hooks_tool() {
  install_git_wrapper_shim
  install_lint_tool_shims
  build_policy_tool coding-ethos-agent-hooks
  if ! has_arg --hook-command "$@"; then
    set -- "$@" --hook-command "$(agent_hook_command)"
  fi
  "${TOOLS_BIN_DIR}/coding-ethos-agent-hooks" "$@"
}

cutover_report() {
  local action="${1:?action required}"
  local status="${2:?status required}"
  local git_hooks="${3:?git hooks status required}"
  local agent_hooks="${4:?agent hooks status required}"
  local repo_ignores="${5:?repo ignores status required}"
  local runtime="${6:?runtime status required}"
  local fix_items_file="${7:-}"

  cat << EOF
format: toon
command: cutover
action: ${action}
status: ${status}
repo: ${ROOT}
surfaces[4]{name,status}:
  git-hooks,${git_hooks}
  agent-hooks,${agent_hooks}
  repo-ignores,${repo_ignores}
  policy-runtime,${runtime}
EOF

  if [[ -n "$fix_items_file" && -s "$fix_items_file" ]]; then
    local item_count
    item_count="$(wc -l < "$fix_items_file" | tr -d ' ')"
    printf 'fix_first[%s]{surface,problem,action}:\n' "$item_count"
    cat "$fix_items_file"
  fi
}

agent_hook_fix_items() {
  local output_file="${1:?agent verify output required}"

  if grep -q 'settings do not contain expected hooks for all providers' "$output_file"; then
    printf '  agent-hooks,native agent settings missing or stale,run cutover install\n'
    return
  fi

  if grep -q 'Codex hooks feature' "$output_file" ||
    grep -q 'codex_hooks' "$output_file"; then
    printf '  agent-hooks,.codex/config.toml missing codex_hooks=true,run cutover install\n'
  fi

  if grep -q '.gemini/settings.json' "$output_file" ||
    grep -q 'Gemini' "$output_file"; then
    printf '  agent-hooks,.gemini/settings.json missing expected hook,run cutover install\n'
  fi
}

runtime_fix_items() {
  local output_file="${1:?runtime verify output required}"

  if [[ -s "$output_file" ]]; then
    printf '  policy-runtime,git-hook validate failed,inspect policy runtime validation output\n'
  fi
}

required_ignore_fix_items() {
  local required_ignore
  for required_ignore in ".coding-ethos/" ".coding-ethos/hook-runs/example/stdout.log"; do
    if ! "$REAL_GIT" -C "$ROOT" check-ignore --quiet "$required_ignore"; then
      printf '  repo-ignores,%s is not ignored,add .coding-ethos/ to .gitignore\n' "$required_ignore"
    fi
  done
}

run_cutover_verify() {
  local action="${1:-verify}"
  local git_hooks=PASS
  local agent_hooks=PASS
  local repo_ignores=PASS
  local runtime=PASS
  local status=ready
  local agent_verify_output
  local repo_ignore_output
  local runtime_verify_output
  local fix_items_output
  agent_verify_output="$(mktemp)"
  repo_ignore_output="$(mktemp)"
  runtime_verify_output="$(mktemp)"
  fix_items_output="$(mktemp)"

  if ! verify_git_hook_shims; then
    git_hooks=FAIL
    status=blocked
    git_hook_fix_items >> "$fix_items_output"
  fi

  if ! run_agent_hooks_tool verify --root "$ROOT" > "$agent_verify_output" 2>&1; then
    agent_hooks=FAIL
    status=blocked
    agent_hook_fix_items "$agent_verify_output" >> "$fix_items_output"
  fi

  if ! CODE_ETHOS_HOOK_LOGGING_ACTIVE=1 "$0" policy-lint \
    --scope cutover \
    --cwd "$ROOT" \
    --json \
    > "$repo_ignore_output" 2>&1; then
    repo_ignores=FAIL
    status=blocked
    required_ignore_fix_items >> "$fix_items_output"
  fi

  if ! CODE_ETHOS_HOOK_LOGGING_ACTIVE=1 CODE_ETHOS_PRECOMMIT_ROOT="$BUNDLE_ROOT" \
    "$0" git-hook validate \
    > "$runtime_verify_output" 2>&1; then
    runtime=FAIL
    status=blocked
    runtime_fix_items "$runtime_verify_output" >> "$fix_items_output"
  fi

  cutover_report "$action" "$status" "$git_hooks" "$agent_hooks" "$repo_ignores" "$runtime" \
    "$fix_items_output"

  if [[ "$status" != ready ]]; then
    if [[ "$agent_hooks" == FAIL ]]; then
      printf 'agent hook verify output:\n' >&2
      cat "$agent_verify_output" >&2
    fi
    if [[ "$runtime" == FAIL ]]; then
      printf 'policy runtime verify output:\n' >&2
      cat "$runtime_verify_output" >&2
    fi
    if [[ "$repo_ignores" == FAIL ]]; then
      printf 'repo ignore verify output:\n' >&2
      cat "$repo_ignore_output" >&2
    fi
    rm -f "$agent_verify_output" "$repo_ignore_output" "$runtime_verify_output" "$fix_items_output"
    return 1
  fi

  rm -f "$agent_verify_output" "$repo_ignore_output" "$runtime_verify_output" "$fix_items_output"
}

run_cutover() {
  local action="${1:-verify}"
  case "$action" in
    verify)
      run_cutover_verify
      ;;
    install)
      install_git_hook_shims
      run_agent_hooks_tool sync --root "$ROOT"
      run_cutover_verify install
      ;;
    *)
      printf 'FATAL: unknown cutover action %q\n' "$action" >&2
      printf 'Usage: %s cutover <verify|install>\n' "$0" >&2
      exit 2
      ;;
  esac
}

run_git_hook() {
  local hook_name="${1:-}"

  compile_policy_bundle
  case "$hook_name" in
    pre-commit | pre-push | commit-msg | validate)
      ;;
    *)
      printf 'FATAL: unknown git hook %q\n' "$hook_name" >&2
      exit 1
      ;;
  esac

  build_policy_tool coding-ethos-git-hook
  build_go_binary "$GIT_HOOK_SRC_DIR" "$GIT_HOOK_BIN"
  exec "${TOOLS_BIN_DIR}/coding-ethos-git-hook" \
    --bundle "$POLICY_BUNDLE" \
    --runner "$GIT_HOOK_BIN" \
    --cwd "$ROOT" \
    "$@"
}

case "${1:-}" in
  agent-hook)
    shift
    run_agent_hook "$@"
    ;;
  git-hook)
    shift
    run_git_hook "$@"
    ;;
  agent-hooks)
    shift
    run_agent_hooks "$@"
    ;;
  cutover)
    shift
    run_cutover "$@"
    ;;
  policy-lint)
    shift
    run_policy_lint "$@"
    ;;
  policy-tool)
    shift
    run_policy_tool "$@"
    ;;
  policy-git)
    shift
    run_policy_git "$@"
    ;;
  *)
    build_go_binary "$GIT_HOOK_SRC_DIR" "$GIT_HOOK_BIN"
    exec "$GIT_HOOK_BIN" "$@"
    ;;
esac
