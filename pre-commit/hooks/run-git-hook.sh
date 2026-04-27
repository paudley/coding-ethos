#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

HOOK_NAME="$(basename "$0")"
ROOT="$(git rev-parse --show-toplevel)"
if [[ -n "${CODE_ETHOS_PRECOMMIT_ROOT:-}" && -d "${CODE_ETHOS_PRECOMMIT_ROOT}" ]]; then
    BUNDLE_ROOT="${CODE_ETHOS_PRECOMMIT_ROOT}"
elif [[ -d "${ROOT}/coding-ethos/pre-commit" ]]; then
    BUNDLE_ROOT="${ROOT}/coding-ethos/pre-commit"
elif [[ -d "${ROOT}/code-ethos/pre-commit" ]]; then
    BUNDLE_ROOT="${ROOT}/code-ethos/pre-commit"
elif [[ -d "${ROOT}/pre-commit" ]]; then
    BUNDLE_ROOT="${ROOT}/pre-commit"
else
    echo "FATAL: could not locate pre-commit bundle under ${ROOT}" >&2
    exit 127
fi

exec "${BUNDLE_ROOT}/hooks/run-go-hook.sh" git-hook "${HOOK_NAME}" "$@"
