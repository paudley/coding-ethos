#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

REAL_GIT="${CODING_ETHOS_REAL_GIT:-/usr/bin/git}"
HOOK_NAME="$(basename "$0")"
ROOT="$("$REAL_GIT" rev-parse --show-toplevel)"
if [[ -n "${CODE_ETHOS_PRECOMMIT_ROOT:-}" && -d "${CODE_ETHOS_PRECOMMIT_ROOT}" ]]; then
  BUNDLE_ROOT="${CODE_ETHOS_PRECOMMIT_ROOT}"
elif [[ -d "${ROOT}/coding-ethos/pre-commit" ]]; then
  BUNDLE_ROOT="${ROOT}/coding-ethos/pre-commit"
elif [[ -d "${ROOT}/code-ethos/pre-commit" ]]; then
  BUNDLE_ROOT="${ROOT}/code-ethos/pre-commit"
elif [[ -d "${ROOT}/pre-commit" ]]; then
  BUNDLE_ROOT="${ROOT}/pre-commit"
else
  {
    echo "FATAL: could not locate coding-ethos pre-commit bundle under ${ROOT}"
    echo "Expected ${ROOT}/coding-ethos/pre-commit."
    echo "If coding-ethos is a missing protected submodule, ask an admin to"
    echo "repair or upgrade it through the documented protected-submodule path."
  } >&2
  exit 127
fi

exec "$(cd "${BUNDLE_ROOT}/.." && pwd)/bin/coding-ethos-run" git-hook "${HOOK_NAME}" "$@"
