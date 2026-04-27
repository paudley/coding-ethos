#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

HOOK_NAME="$(basename "$0")"

if ! command -v git >/dev/null 2>&1; then
	exit 0
fi

if ! git lfs version >/dev/null 2>&1; then
	exit 0
fi

exec git lfs "$HOOK_NAME" "$@"
