#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

set -euo pipefail

usage() {
  cat >&2 << 'USAGE'
Usage:
  install-github-binary.sh <owner/repo> <tag> <asset-substring> <binary-name> <dest-dir>

Downloads a GitHub release asset whose name contains <asset-substring>,
extracts it when it is a .tar.gz, .tgz, or .zip archive, and installs
<binary-name> into <dest-dir>. This helper is intentionally GitHub-only until
the managed toolchain has stronger provenance support for other sources.
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
import sys
import urllib.request

repo, tag, asset_substring = sys.argv[1:]
url = f"https://api.github.com/repos/{repo}/releases/tags/{tag}"
with urllib.request.urlopen(url, timeout=30) as response:
    release = json.load(response)

for asset in release.get("assets", []):
    name = asset.get("name", "")
    if asset_substring in name:
        print(asset["browser_download_url"])
        raise SystemExit(0)

raise SystemExit(f"no release asset for {repo}@{tag} contains {asset_substring!r}")
PY
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
  [[ "$#" -eq 5 ]] || usage
  require_tool python3
  require_tool curl

  local repo="$1"
  local tag="$2"
  local asset_substring="$3"
  local binary_name="$4"
  local dest_dir="$5"
  local url
  url="$(release_asset_url "$repo" "$tag" "$asset_substring")"

  local work_dir
  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' EXIT

  local archive="${work_dir}/${url##*/}"
  curl -fsSL "$url" -o "$archive"
  install_asset "$archive" "$binary_name" "$dest_dir" "$work_dir"
}

main "$@"
