#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

case "${1:?usage: smoke_hook_edges.sh <gitlink|build-failure> ...}" in
  install-runtime)
    repo_root="${2:?repo root required}"
    go_bin="${3:?go bin required}"
    policy_dir="${4:?policy dir required}"
    target_repo="${5:?target repo required}"
    common_dir="$(git -C "$target_repo" rev-parse --path-format=absolute --git-common-dir)"
    mkdir -p "$common_dir/coding-ethos-hooks/bin" "$common_dir/coding-ethos-hooks/policy"
    cp "$go_bin"/coding-ethos-* "$common_dir/coding-ethos-hooks/bin/"
    cp "$policy_dir"/policy-bundle.json "$common_dir/coding-ethos-hooks/policy/"
    go build -C "$repo_root/pre-commit/hooks/go-hooks" \
      -buildvcs=false \
      -o "$common_dir/coding-ethos-hooks/coding-ethos-git-hook" .
    ;;
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
    git -C "$git_repo" reset -- coding-ethos > /dev/null
    rm -rf "$git_repo/coding-ethos"
    ;;
  build-failure)
    run_go_hook="${2:?run-go-hook path required}"
    cutover_repo="${3:?cutover repo required}"

    printf '==> validating hook execution does not auto-rebuild Go helpers\n'
    set +e
    (
      cd "$cutover_repo"
      printf '{"hook_event_name":"Stop","tool_name":"Stop"}\n' |
        GOFLAGS=-not-a-real-go-flag "$run_go_hook" agent-hook \
          > /tmp/coding-ethos-build-failure.out 2>&1
    )
    build_failure_status=$?
    set -e
    if [[ "$build_failure_status" -ne 0 ]] ||
      grep -q 'go build' /tmp/coding-ethos-build-failure.out ||
      grep -q 'hook infrastructure build failed' /tmp/coding-ethos-build-failure.out; then
      printf 'expected hook execution to use installed binaries without rebuilding:\n' >&2
      cat /tmp/coding-ethos-build-failure.out >&2
      exit 1
    fi

    printf '==> validating legacy runtime cache is not the hook source of truth\n'
    hook_bin="$cutover_repo/.git/coding-ethos-hooks/bin/coding-ethos-hook"
    hook_bin_backup="${hook_bin}.smoke-backup"
    mv "$hook_bin" "$hook_bin_backup"
    set +e
    (
      cd "$cutover_repo"
      printf '{"hook_event_name":"Stop","tool_name":"Stop"}\n' |
        "$run_go_hook" agent-hook > /tmp/coding-ethos-missing-runtime.out 2>&1
    )
    missing_runtime_status=$?
    set -e
    mv "$hook_bin_backup" "$hook_bin"
    if [[ "$missing_runtime_status" -ne 0 ]] ||
      grep -q 'go build' /tmp/coding-ethos-missing-runtime.out ||
      grep -q 'hook runtime is not installed or is stale' \
        /tmp/coding-ethos-missing-runtime.out; then
      printf 'expected checkout runtime to ignore stale legacy cache:\n' >&2
      cat /tmp/coding-ethos-missing-runtime.out >&2
      exit 1
    fi
    ;;
  *)
    printf 'unknown smoke hook edge command: %s\n' "$1" >&2
    exit 2
    ;;
esac
