#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

report_go_build_failure() {
  local root="${1:?repo root required}"
  local src_dir="${2:?src dir required}"
  local bin="${3:?bin required}"
  local output_file="${4:?output file required}"

  {
    printf 'FATAL: coding-ethos hook infrastructure build failed\n'
    printf 'This is not caused by the files being committed. The protected hook runtime could not rebuild its Go helper.\n'
    printf 'repo: %s\n' "$root"
    printf 'source: %s\n' "$src_dir"
    printf 'target: %s\n' "$bin"
    printf 'command: go build -C %q -buildvcs=false -o %q .\n' "$src_dir" "$bin"
    printf 'action: stop and ask an admin to update or repair the coding-ethos hook bundle; do not bypass hooks or rebuild protected files by hand.\n'
    printf 'go_output:\n'
    cat "$output_file"
  } >&2
}
