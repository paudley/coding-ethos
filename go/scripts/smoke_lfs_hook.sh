#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

run_go_hook="${1:?run-go-hook path required}"
tmp_root="${2:?temporary root required}"

lfs_hook_dir="$tmp_root/lfs-hooks"
fake_bin="$tmp_root/fake-bin"
mkdir -p "$lfs_hook_dir" "$fake_bin"
ln -sf "$run_go_hook" "$lfs_hook_dir/post-commit"

cat > "$fake_bin/git" << 'FAKEGIT'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "rev-parse" && "${2:-}" == "--show-toplevel" ]]; then
  pwd
  exit 0
fi
if [[ "${1:-}" == "rev-parse" && "${2:-}" == "--path-format=absolute" && "${3:-}" == "--git-path" && "${4:-}" == "hooks" ]]; then
  printf '%s/.git/hooks\n' "$PWD"
  exit 0
fi
if [[ "${1:-}" != "lfs" ]]; then
  exit 1
fi
if [[ "${2:-}" == "version" ]]; then
  printf 'git-lfs/test\n'
  exit 0
fi
printf '%s\n' "$*" > "${CODING_ETHOS_FAKE_GIT_LOG:?}"
FAKEGIT
chmod +x "$fake_bin/git"

fake_git_log="$tmp_root/fake-git-lfs.log"
CODING_ETHOS_FAKE_GIT_LOG="$fake_git_log" \
  CODING_ETHOS_REAL_GIT="$fake_bin/git" \
  "$lfs_hook_dir/post-commit"
if [[ "$(cat "$fake_git_log")" != "lfs post-commit" ]]; then
  printf 'expected git lfs post-commit delegation, got:\n' >&2
  cat "$fake_git_log" >&2
  exit 1
fi
