#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail
# shellcheck disable=SC2034

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
  bandit
  sqlfluff
  tombi
  dotenv-linter
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

managed_tool_path() {
  local tool="${1:?tool required}"
  case "$tool" in
    ruff | mypy | pyright | pylint | yamllint | bandit | sqlfluff | tombi)
      managed_uv_tool_wrapper "$tool"
      ;;
    shellcheck | actionlint | hadolint | dotenv-linter)
      local binary="${MANAGED_GITHUB_BIN_DIR}/${tool}"
      [[ -x "$binary" ]] || return 1
      printf '%s\n' "$binary"
      ;;
    golangci-lint)
      local binary="${MANAGED_GO_BIN_DIR}/${tool}"
      [[ -x "$binary" ]] || return 1
      printf '%s\n' "$binary"
      ;;
    *)
      return 1
      ;;
  esac
}

install_lint_tool_shims() {
  local tool
  for tool in "${CAPTURED_LINT_TOOLS[@]}"; do
    if ! managed_tool_path "$tool" > /dev/null 2>&1; then
      rm -f "${TOOLS_BIN_DIR}/${tool}"
      continue
    fi
    install_lint_tool_shim "$tool"
  done
}

install_lint_tool_shim() {
  local tool="${1:?tool required}"

  local env_name
  local shim
  local tmp_shim
  env_name="$(real_tool_env_var "$tool")"
  shim="${TOOLS_BIN_DIR}/${tool}"
  tmp_shim="${shim}.tmp.$$"
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'unset %s\n' "$env_name"
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

  enforce_generated_tool_config_integrity

  local managed_tool
  managed_tool="$(managed_tool_path "$tool" || true)"
  if [[ -z "$managed_tool" ]]; then
    printf 'FATAL: coding-ethos managed %s runner is not configured for lint capture\n' "$tool" >&2
    exit 127
  fi

  local -a resolved_args
  resolve_lint_target_args resolved_args "$@"
  local -a enforced_args
  enforce_lint_tool_config "$tool" enforced_args "${resolved_args[@]}"

  require_policy_tool coding-ethos-lint
  exec "${TOOLS_BIN_DIR}/coding-ethos-lint" \
    --bundle "$POLICY_BUNDLE" \
    --capture-tool "$tool" \
    --tool-path "$managed_tool" \
    --cwd "$ETHOS_ROOT" \
    --trace-root "$ROOT" \
    -- \
    "${enforced_args[@]}"
}

enforce_generated_tool_config_integrity() {
  local output
  set +e
  output="$(uv run --project "$ETHOS_ROOT" python "${ETHOS_ROOT}/main.py" \
    --repo "$ROOT" \
    --check-tool-configs 2>&1)"
  local status=$?
  set -e
  if [[ "$status" -eq 0 ]]; then
    return
  fi

  printf 'format: toon\n'
  printf 'tool: coding-ethos-config-integrity\n'
  printf 'status: FAIL\n'
  printf 'title: GENERATED TOOL CONFIG DRIFT\n'
  local -a drifted_configs=()
  if [[ -n "$output" ]]; then
    local line
    while IFS= read -r line; do
      [[ -n "$line" ]] || continue
      drifted_configs+=("$(repo_relative_config_path "$line")")
    done < <(printf '%s\n' "$output" | sed '/^[[:space:]]*$/d')
  fi

  if [[ "${#drifted_configs[@]}" -gt 0 ]]; then
    printf 'message: Hey - lint failed - you modified: %s. Restore it before continuing. Or run make -C coding-ethos fix-configs.\n' \
      "$(join_config_paths "${drifted_configs[@]}")"
    printf 'drifted_configs[%s]{file}:\n' "${#drifted_configs[@]}"
    local path
    for path in "${drifted_configs[@]}"; do
      printf '  %s\n' "$path"
    done
  else
    printf 'message: Hey - lint failed - generated tool config files were modified or are missing. Restore them before continuing. Or run make -C coding-ethos fix-configs.\n'
  fi
  printf 'next[1]{action}:\n'
  printf '  Run make -C coding-ethos fix-configs\, then rerun the lint command.\n'
  exit 2
}

repo_relative_config_path() {
  local path="${1:?path required}"
  local root_prefix="${ROOT%/}/"
  if [[ "$path" == "$root_prefix"* ]]; then
    printf '%s\n' "${path#"$root_prefix"}"
    return
  fi

  printf '%s\n' "$path"
}

join_config_paths() {
  local first=true
  local path
  for path in "$@"; do
    if [[ "$first" == true ]]; then
      first=false
    else
      printf '; '
    fi
    printf '%s' "$path"
  done
}

enforce_lint_tool_config() {
  local tool="${1:?tool required}"
  local -n out_ref="${2:?output array required}"
  : "${out_ref[@]+${out_ref[*]}}"
  shift 2 || true

  if lint_info_command "$@"; then
    out_ref=("$@")
    return
  fi

  case "$tool" in
    ruff)
      if [[ "${1:-}" == "check" ]]; then
        shift || true
        out_ref=("check" "--config" "${ETHOS_ROOT}/ruff.toml" "$@")
      else
        out_ref=("--config" "${ETHOS_ROOT}/ruff.toml" "$@")
      fi
      ;;
    mypy)
      local consumer_python
      consumer_python="$(consumer_python_executable || true)"
      if [[ -n "$consumer_python" ]]; then
        out_ref=(
          "--config-file" "${ETHOS_ROOT}/mypy.ini"
          "--python-executable" "$consumer_python"
          "$@"
        )
      else
        out_ref=("--config-file" "${ETHOS_ROOT}/mypy.ini" "$@")
      fi
      ;;
    pyright)
      out_ref=("--project" "${ETHOS_ROOT}/pyrightconfig.json" "$@")
      ;;
    pylint)
      out_ref=("--rcfile" "${ETHOS_ROOT}/.pylintrc" "$@")
      ;;
    yamllint)
      out_ref=("-c" "${ETHOS_ROOT}/.yamllint.yml" "$@")
      ;;
    bandit)
      out_ref=("-c" "${ETHOS_ROOT}/.bandit.yml" "-f" "json" "$@")
      ;;
    sqlfluff)
      if [[ "${1:-}" == "lint" ]]; then
        shift || true
        out_ref=("lint" "--config" "${ETHOS_ROOT}/.sqlfluff" "--format" "json" "$@")
      else
        out_ref=("--config" "${ETHOS_ROOT}/.sqlfluff" "--format" "json" "$@")
      fi
      ;;
    tombi)
      if [[ "${1:-}" == "lint" ]]; then
        shift || true
        out_ref=("lint" "--quiet" "--error-on-warnings" "$@")
      else
        out_ref=("lint" "--quiet" "--error-on-warnings" "$@")
      fi
      ;;
    dotenv-linter)
      if [[ "${1:-}" == "check" ]]; then
        shift || true
      fi
      out_ref=("--plain" "--quiet" "check" "$@")
      ;;
    *)
      out_ref=("$@")
      ;;
  esac
}

consumer_python_executable() {
  local candidate
  for candidate in \
    "${ROOT}/.venv/bin/python" \
    "${ROOT}/.venv/bin/python3"; do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return
    fi
  done

  return 1
}

lint_info_command() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      --help | -h | help | --version | -V | version)
        return 0
        ;;
    esac
  done

  return 1
}

resolve_lint_target_args() {
  local output_name="${1:?output array required}"
  local -n resolved_args_ref="$output_name"
  : "${resolved_args_ref[@]+${resolved_args_ref[*]}}"
  shift || true

  resolved_args_ref=()
  local arg
  for arg in "$@"; do
    append_resolved_lint_target_arg "$output_name" "$arg"
  done
}

append_resolved_lint_target_arg() {
  local -n append_args_ref="${1:?output array required}"
  local arg="${2:?arg required}"

  if lint_target_arg_should_passthrough "$arg"; then
    append_args_ref+=("$arg")
    return
  fi

  if lint_target_arg_has_glob "$arg"; then
    append_lint_target_glob_matches "$1" "$arg"
    return
  fi

  append_args_ref+=("$(resolve_lint_target_path "$arg")")
}

lint_target_arg_should_passthrough() {
  local arg="${1:?arg required}"

  if [[ "$arg" == -* || "$arg" == *"="* || "$arg" == "." || "$arg" == "./..." || "$arg" == "..." ]]; then
    return 0
  fi

  return 1
}

lint_target_arg_has_glob() {
  local arg="${1:?arg required}"
  [[ "$arg" == *"*"* || "$arg" == *"?"* || "$arg" == *"["* ]]
}

append_lint_target_glob_matches() {
  local -n glob_args_ref="${1:?output array required}"
  local arg="${2:?arg required}"
  local matches=()
  local base
  for base in "${INVOCATION_CWD:-$ROOT}" "$ROOT"; do
    lint_target_glob_matches_from_base matches "$base" "$arg"
    if [[ "${#matches[@]}" -gt 0 ]]; then
      glob_args_ref+=("${matches[@]}")
      return
    fi
  done

  glob_args_ref+=("$arg")
}

lint_target_glob_matches_from_base() {
  local -n matches_ref="${1:?matches array required}"
  local base="${2:?base required}"
  local pattern="${3:?pattern required}"
  matches_ref=()

  local match
  while IFS= read -r match; do
    [[ -n "$match" ]] || continue
    matches_ref+=("$(resolve_lint_target_path "$match")")
  done < <(
    cd "$base" &&
      compgen -G "$pattern" |
      sort
  )
}

resolve_lint_target_path() {
  local arg="${1:?arg required}"

  if [[ "$arg" = /* ]]; then
    printf '%s\n' "$arg"
    return
  fi

  local base="${INVOCATION_CWD:-$ROOT}"
  local candidate="${base}/${arg}"
  if [[ -e "$candidate" ]]; then
    (cd "$(dirname "$candidate")" && printf '%s/%s\n' "$PWD" "$(basename "$candidate")")
    return
  fi

  candidate="${ROOT}/${arg}"
  if [[ -e "$candidate" ]]; then
    (cd "$(dirname "$candidate")" && printf '%s/%s\n' "$PWD" "$(basename "$candidate")")
    return
  fi

  printf '%s\n' "$arg"
}

managed_uv_tool_wrapper() {
  local tool="${1:?tool required}"
  local uv_bin="${UV:-uv}"
  command -v "$uv_bin" > /dev/null 2>&1 || return 1

  local wrapper_dir="${TOOLS_BIN_DIR}/managed-tools"
  local wrapper="${wrapper_dir}/${tool}"
  mkdir -p "$wrapper_dir"
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'exec %s run --project %s %s "$@"\n' \
      "$(shell_quote "$uv_bin")" \
      "$(shell_quote "${BUNDLE_ROOT}/hooks")" \
      "$(shell_quote "$tool")"
  } > "$wrapper"
  chmod +x "$wrapper"

  printf '%s\n' "$wrapper"
}
