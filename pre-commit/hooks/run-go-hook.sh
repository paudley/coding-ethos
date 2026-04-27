#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

# Build and run cached Go hook helpers.

set -euo pipefail

REAL_GIT="${CODING_ETHOS_REAL_GIT:-/usr/bin/git}"
ROOT="$("$REAL_GIT" rev-parse --show-toplevel)"
GIT_COMMON_DIR="$("$REAL_GIT" rev-parse --path-format=absolute --git-common-dir)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ETHOS_ROOT="$(cd "${BUNDLE_ROOT}/.." && pwd)"
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
	} >"$metadata_log"

	set +e
	CODE_ETHOS_HOOK_LOGGING_ACTIVE=1 "$0" "$@" \
		> >(tee -a "$stdout_log") \
		2> >(tee -a "$stderr_log" >&2)
	local status=$?
	set -e

	{
		printf 'finished_at_utc=%q\n' "$(date -u +%Y%m%dT%H%M%SZ)"
		printf 'exit_code=%q\n' "$status"
	} >>"$metadata_log"

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
	} >"$tmp_shim"
	chmod +x "$tmp_shim"
	mv -f "$tmp_shim" "$shim"
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
	} >>"$CLAUDE_ENV_FILE"
}

needs_go_build() {
	local src_dir="${1:?src dir required}"
	local bin="${2:?bin required}"

	[[ ! -x "$bin" ]] && return 0
	find "$src_dir" -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -newer "$bin" | grep -q .
}

build_go_binary() {
	local src_dir="${1:?src dir required}"
	local bin="${2:?bin required}"
	if needs_go_build "$src_dir" "$bin"; then
		local tmp_bin="${bin}.tmp.$$"
		rm -f "$tmp_bin"
		go build -C "$src_dir" -buildvcs=false -o "$tmp_bin" .
		mv -f "$tmp_bin" "$bin"
	fi
}

build_policy_tool() {
	local name="${1:?tool name required}"
	local src_dir="${TOOLS_SRC_DIR}/cmd/${name}"
	local bin="${TOOLS_BIN_DIR}/${name}"

	if [[ -x "$bin" ]] && ! find \
		"${TOOLS_SRC_DIR}/cmd/${name}" \
		"${TOOLS_SRC_DIR}/internal" \
		-type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) \
		-newer "$bin" | grep -q .; then
		return
	fi

	local tmp_bin="${bin}.tmp.$$"
	rm -f "$tmp_bin"
	go build -C "$src_dir" -buildvcs=false -o "$tmp_bin" .
	mv -f "$tmp_bin" "$bin"
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
		"${TOOLS_BIN_DIR}/coding-ethos-policy" "${args[@]}" >/dev/null
	fi
}

run_agent_hook() {
	compile_policy_bundle
	install_git_wrapper_shim
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
	build_policy_tool coding-ethos-agent-hooks
	if ! has_arg --hook-command "$@"; then
		set -- "$@" --hook-command "PATH=${TOOLS_BIN_DIR}:\$PATH ${SCRIPT_DIR}/run-go-hook.sh agent-hook"
	fi
	exec "${TOOLS_BIN_DIR}/coding-ethos-agent-hooks" "$@"
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
	policy-lint)
		shift
		run_policy_lint "$@"
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
