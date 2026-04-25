#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

repo_root="${1:?usage: smoke.sh /path/to/coding-ethos}"
go_bin="${2:?usage: smoke.sh /path/to/coding-ethos /tmp/bin}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

policy_dir="$tmp_root/policy"
git_repo="$tmp_root/repo"

policy_bin="$go_bin/coding-ethos-policy"
lint_bin="$go_bin/coding-ethos-lint"
hook_bin="$go_bin/coding-ethos-hook"
git_bin="$go_bin/coding-ethos-git"

for bin in "$policy_bin" "$lint_bin" "$hook_bin" "$git_bin"; do
  if [[ ! -x "$bin" ]]; then
    printf 'missing executable: %s\n' "$bin" >&2
    exit 1
  fi
done

printf '==> compiling policy bundle\n'
"$policy_bin" compile \
  --primary "$repo_root/coding_ethos.yml" \
  --config "$repo_root/config.yaml" \
  --out-dir "$policy_dir" \
  --generated-at "2026-04-24T00:00:00Z" >/dev/null

printf '==> validating lint block decision\n'
lint_output="$("$lint_bin" \
  --bundle "$policy_dir/policy-bundle.json" \
  --staged \
  --argv "git commit --no-verify -m test" \
  --json)"
if ! grep -q '"status": "blocked"' <<<"$lint_output"; then
  printf 'expected lint status blocked, got:\n%s\n' "$lint_output" >&2
  exit 1
fi
if ! grep -q '"policy_id": "git.hook_bypass"' <<<"$lint_output"; then
  printf 'expected lint hook bypass decision, got:\n%s\n' "$lint_output" >&2
  exit 1
fi

printf '==> validating hook block exit\n'
set +e
hook_output="$(printf '%s' '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git commit --no-verify -m test"}}' \
  | "$hook_bin" --bundle "$policy_dir/policy-bundle.json" --json 2>&1)"
hook_status=$?
set -e
if [[ "$hook_status" -ne 2 ]]; then
  printf 'expected hook exit 2, got %s:\n%s\n' "$hook_status" "$hook_output" >&2
  exit 1
fi
if ! grep -q '"status": "blocked"' <<<"$hook_output"; then
  printf 'expected hook status blocked, got:\n%s\n' "$hook_output" >&2
  exit 1
fi

printf '==> creating disposable git repo\n'
git init "$git_repo" >/dev/null
git -C "$git_repo" config user.email test@example.com
git -C "$git_repo" config user.name Test
printf 'x\n' > "$git_repo/file.txt"
git -C "$git_repo" add file.txt

printf '==> validating git wrapper allows normal commit\n'
(
  cd "$git_repo"
  "$git_bin" \
    --bundle "$policy_dir/policy-bundle.json" \
    --real-git "$(command -v git)" \
    commit -m "test" >/dev/null
)
first_head="$(git -C "$git_repo" rev-parse HEAD)"

printf 'y\n' > "$git_repo/file.txt"
git -C "$git_repo" add file.txt

printf '==> validating git wrapper blocks bypass commit\n'
set +e
(
  cd "$git_repo"
  "$git_bin" \
    --bundle "$policy_dir/policy-bundle.json" \
    --real-git "$(command -v git)" \
    commit --no-verify -m "bypass" >/tmp/coding-ethos-git-smoke.out 2>&1
)
git_status=$?
set -e
if [[ "$git_status" -ne 2 ]]; then
  printf 'expected git wrapper exit 2, got %s:\n' "$git_status" >&2
  cat /tmp/coding-ethos-git-smoke.out >&2
  exit 1
fi
second_head="$(git -C "$git_repo" rev-parse HEAD)"
if [[ "$first_head" != "$second_head" ]]; then
  printf 'expected HEAD to remain at %s, got %s\n' "$first_head" "$second_head" >&2
  exit 1
fi

printf 'go tools smoke passed\n'
