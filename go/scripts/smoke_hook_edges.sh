#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

case "${1:?usage: smoke_hook_edges.sh <gitlink|build-failure> ...}" in
  gitlink)
    lint_bin="${2:?lint bin required}"
    policy_dir="${3:?policy dir required}"
    git_repo="${4:?git repo required}"
    first_head="${5:?head sha required}"

    printf '==> validating staged gitlinks are ignored by file guards\n'
    mkdir -p "$git_repo/coding-ethos"
    git -C "$git_repo" update-index --add --cacheinfo 160000 "$first_head" coding-ethos
    set +e
    gitlink_output="$("$lint_bin" \
      --bundle "$policy_dir/policy-bundle.json" \
      --scope staged \
      --cwd "$git_repo" \
      --json 2>&1)"
    gitlink_status=$?
    set -e
    if [[ "$gitlink_status" -ne 0 ]]; then
      printf 'expected staged gitlink to pass file guards, got %s:\n%s\n' \
        "$gitlink_status" "$gitlink_output" >&2
      exit 1
    fi
    git -C "$git_repo" reset -- coding-ethos >/dev/null
    rm -rf "$git_repo/coding-ethos"
    ;;
  build-failure)
    run_go_hook="${2:?run-go-hook path required}"
    cutover_repo="${3:?cutover repo required}"

    printf '==> validating Go rebuild failures are actionable\n'
    rm -f "$cutover_repo/.git/coding-ethos-hooks/coding-ethos-git-hook"*
    set +e
    (
      cd "$cutover_repo"
      GOFLAGS=-not-a-real-go-flag "$run_go_hook" \
        >/tmp/coding-ethos-build-failure.out 2>&1
    )
    build_failure_status=$?
    set -e
    if [[ "$build_failure_status" -eq 0 ]] ||
      ! grep -q 'FATAL: coding-ethos hook infrastructure build failed' \
        /tmp/coding-ethos-build-failure.out ||
      ! grep -q 'not caused by the files being committed' \
        /tmp/coding-ethos-build-failure.out ||
      ! grep -q 'do not bypass hooks or rebuild protected files by hand' \
        /tmp/coding-ethos-build-failure.out; then
      printf 'expected actionable Go rebuild failure:\n' >&2
      cat /tmp/coding-ethos-build-failure.out >&2
      exit 1
    fi
    ;;
  *)
    printf 'unknown smoke hook edge command: %s\n' "$1" >&2
    exit 2
    ;;
esac
