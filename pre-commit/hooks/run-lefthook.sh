#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

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
REPO_LEFTHOOK_VERSION="${GIT_COMMON_DIR}/coding-ethos-hooks/lefthook.version"
REPO_LEFTHOOK_DIR="$(dirname "${REPO_LEFTHOOK}")"
QUIET_FILTER="${BUNDLE_ROOT}/hooks/run-go-hook.sh"
HOOK_ARGS=("$@")

if [[ "${LEFTHOOK:-}" == "0" ]]; then
    echo "FATAL: LEFTHOOK=0 hook bypass is forbidden." >&2
    exit 1
fi

run_lefthook() {
    local -a command=("$@")
    local -a run_args=("run" "--no-auto-install")

    if [[ "$HOOK_NAME" == "pre-commit" ]]; then
        run_args+=("--no-stage-fixed")
    fi

    if [[ -x "$QUIET_FILTER" ]]; then
        set +e
        "${command[@]}" "${run_args[@]}" "$HOOK_NAME" "${HOOK_ARGS[@]}" 2>&1 | "$QUIET_FILTER" quiet-filter
        local status=${PIPESTATUS[0]}
        set -e
        exit "$status"
    fi

    exec "${command[@]}" "${run_args[@]}" "$HOOK_NAME" "${HOOK_ARGS[@]}"
}

ensure_repo_lefthook() {
    mkdir -p "${REPO_LEFTHOOK_DIR}"

    if [[ -x "${REPO_LEFTHOOK}" ]] &&
        [[ -f "${REPO_LEFTHOOK_VERSION}" ]] &&
        [[ "$(<"${REPO_LEFTHOOK_VERSION}")" == "${LEFTHOOK_VERSION}" ]]; then
        return
    fi

    if ! command -v go >/dev/null 2>&1; then
        cat >&2 <<EOF
FATAL: cannot install the repo-local Lefthook binary.

Expected:
  ${REPO_LEFTHOOK}

Fix:
  1. install Go
  2. run: cd ${BUNDLE_ROOT%/pre-commit} && make install-hooks
EOF
        exit 127
    fi

    GOBIN="${REPO_LEFTHOOK_DIR}" go install "github.com/evilmartians/lefthook@${LEFTHOOK_VERSION}"
    printf '%s\n' "${LEFTHOOK_VERSION}" > "${REPO_LEFTHOOK_VERSION}"
}

ensure_repo_lefthook

if [[ -x "$REPO_LEFTHOOK" ]]; then
    run_lefthook "$REPO_LEFTHOOK"
else
    cat >&2 <<EOF
FATAL: cannot run Lefthook.

Expected:
  ${REPO_LEFTHOOK}

Run:
  cd ${BUNDLE_ROOT}
  make install-hooks
EOF
    exit 127
fi
