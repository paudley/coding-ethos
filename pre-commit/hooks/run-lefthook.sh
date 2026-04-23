#!/usr/bin/env bash
# Run Lefthook with repository-local resolution before system fallbacks.

set -euo pipefail

LEFTHOOK_VERSION="v1.13.6"
HOOK_NAME="$(basename "$0")"
ROOT="$(git rev-parse --show-toplevel)"
GIT_COMMON_DIR="$(git rev-parse --path-format=absolute --git-common-dir)"
if [[ -n "${CODE_ETHOS_PRECOMMIT_ROOT:-}" && -d "${CODE_ETHOS_PRECOMMIT_ROOT}" ]]; then
    BUNDLE_ROOT="${CODE_ETHOS_PRECOMMIT_ROOT}"
elif [[ -d "${ROOT}/code-ethos/pre-commit" ]]; then
    BUNDLE_ROOT="${ROOT}/code-ethos/pre-commit"
elif [[ -d "${ROOT}/pre-commit" ]]; then
    BUNDLE_ROOT="${ROOT}/pre-commit"
else
    echo "FATAL: could not locate pre-commit bundle under ${ROOT}" >&2
    exit 127
fi
REPO_LEFTHOOK="${GIT_COMMON_DIR}/coding-ethos-hooks/lefthook"
QUIET_FILTER="${BUNDLE_ROOT}/hooks/run-go-hook.sh"
HOOK_ARGS=("$@")

if [[ "${LEFTHOOK:-}" == "0" ]]; then
    echo "FATAL: LEFTHOOK=0 hook bypass is forbidden." >&2
    exit 1
fi

run_lefthook() {
    local -a command=("$@")

    if [[ -x "$QUIET_FILTER" ]]; then
        set +e
        "${command[@]}" run "$HOOK_NAME" "${HOOK_ARGS[@]}" 2>&1 | "$QUIET_FILTER" quiet-filter
        local status=${PIPESTATUS[0]}
        set -e
        exit "$status"
    fi

    exec "${command[@]}" run "$HOOK_NAME" "${HOOK_ARGS[@]}"
}

if [[ -x "$REPO_LEFTHOOK" ]]; then
    run_lefthook "$REPO_LEFTHOOK"
elif command -v lefthook >/dev/null 2>&1; then
    run_lefthook lefthook
elif command -v go >/dev/null 2>&1; then
    run_lefthook go run "github.com/evilmartians/lefthook@${LEFTHOOK_VERSION}"
else
    cat >&2 <<EOF
FATAL: cannot run Lefthook.

Tried:
  1. ${REPO_LEFTHOOK}
  2. lefthook from PATH
  3. go run github.com/evilmartians/lefthook@${LEFTHOOK_VERSION}

Run:
  cd ${BUNDLE_ROOT}
  make install-hooks
EOF
    exit 127
fi
