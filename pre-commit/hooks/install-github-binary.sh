#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

usage() {
  cat >&2 << 'USAGE'
Usage:
  install-github-binary.sh <owner/repo> <tag> <asset-substring> <binary-name> <dest-dir> <sha256>

Downloads a GitHub release asset whose name contains <asset-substring>,
extracts it when it is a .tar.gz, .tgz, .tar.xz, .txz, or .zip archive, and installs
<binary-name> into <dest-dir>. This helper is intentionally GitHub-only until
the managed toolchain has stronger provenance support for other sources.
The downloaded asset must match the exact expected SHA-256 digest.

If GITHUB_TOKEN is set, it is used for GitHub API and asset requests.
USAGE
  exit 2
}

require_tool() {
  command -v "$1" > /dev/null 2>&1 || {
    printf 'FATAL: required tool not found: %s\n' "$1" >&2
    exit 127
  }
}

release_asset_url() {
  local repo="${1:?repo required}"
  local tag="${2:?tag required}"
  local asset_substring="${3:?asset substring required}"

  python3 - "$repo" "$tag" "$asset_substring" << 'PY'
from __future__ import annotations

import json
import os
import sys
import urllib.request

repo, tag, asset_substring = sys.argv[1:]
url = f"https://api.github.com/repos/{repo}/releases/tags/{tag}"
request = urllib.request.Request(url)
token = os.environ.get("GITHUB_TOKEN")
if token:
    request.add_header("Authorization", f"Bearer {token}")
    request.add_header("X-GitHub-Api-Version", "2022-11-28")
with urllib.request.urlopen(request, timeout=30) as response:
    release = json.load(response)

for asset in release.get("assets", []):
    name = asset.get("name", "")
    if asset_substring in name:
        print(asset["browser_download_url"])
        raise SystemExit(0)

raise SystemExit(f"no release asset for {repo}@{tag} contains {asset_substring!r}")
PY
}

verify_sha256() {
  local path="${1:?path required}"
  local expected="${2:?expected sha256 required}"

  local actual
  actual="$(
    python3 - "$path" << 'PY'
import hashlib
import sys
from pathlib import Path

print(hashlib.sha256(Path(sys.argv[1]).read_bytes()).hexdigest())
PY
  )"
  if [[ "$actual" != "$expected" ]]; then
    printf 'FATAL: SHA-256 mismatch for %s\n' "$path" >&2
    printf 'expected: %s\n' "$expected" >&2
    printf 'actual:   %s\n' "$actual" >&2
    exit 1
  fi
}

curl_github_asset() {
  local url="${1:?url required}"
  local output="${2:?output required}"
  local -a curl_args=(-fsSL "$url" -o "$output")
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    curl_args=(
      -H "Authorization: Bearer ${GITHUB_TOKEN}"
      -H "X-GitHub-Api-Version: 2022-11-28"
      "${curl_args[@]}"
    )
  fi

  curl "${curl_args[@]}"
}

install_asset() {
  local archive="${1:?archive required}"
  local binary_name="${2:?binary name required}"
  local dest_dir="${3:?destination dir required}"
  local work_dir="${4:?work dir required}"

  case "$archive" in
    *.tar.gz | *.tgz)
      tar -xzf "$archive" -C "$work_dir"
      ;;
    *.tar.xz | *.txz)
      tar -xJf "$archive" -C "$work_dir"
      ;;
    *.zip)
      require_tool unzip
      unzip -q "$archive" -d "$work_dir"
      ;;
    *)
      mkdir -p "$dest_dir"
      install -m 0755 "$archive" "${dest_dir}/${binary_name}"
      return
      ;;
  esac

  local found
  found="$(find "$work_dir" -type f -name "$binary_name" -perm -u+x | head -n 1)"
  [[ -n "$found" ]] || {
    printf 'FATAL: %s not found in downloaded GitHub asset\n' "$binary_name" >&2
    exit 1
  }

  mkdir -p "$dest_dir"
  install -m 0755 "$found" "${dest_dir}/${binary_name}"
}

main() {
  [[ "$#" -eq 6 ]] || usage
  require_tool python3
  require_tool curl

  local repo="$1"
  local tag="$2"
  local asset_substring="$3"
  local binary_name="$4"
  local dest_dir="$5"
  local expected_sha256="$6"
  local url
  url="$(release_asset_url "$repo" "$tag" "$asset_substring")"

  local work_dir
  work_dir="$(mktemp -d)"
  trap 'rm -rf -- '"$(printf '%q' "$work_dir")" EXIT

  local archive="${work_dir}/${url##*/}"
  curl_github_asset "$url" "$archive"
  verify_sha256 "$archive" "$expected_sha256"
  install_asset "$archive" "$binary_name" "$dest_dir" "$work_dir"
}

main "$@"
