# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Render generated CI configuration files for coding-ethos gates.

This module owns provider-specific CI templates so generated tool config
orchestration stays below the line-limit gate. The generated files are normal
managed config artifacts, which means `sync-tool-configs` writes them and
`check-tool-configs` verifies them through the shared hash manifest.
"""

from collections.abc import Mapping
from typing import Any, cast

HASH_SPDX_HEADER = (
    "# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. "
    "<paudley@blackcat.ca>\n"
    "# SPDX-License-Identifier: MIT\n\n"
)
GENERATED_CI_CONFIGS: tuple[str, ...] = (
    ".github/workflows/coding-ethos-sarif.yml",
    ".gitlab-ci.yml",
)


def _with_hash_spdx_header(content: str) -> str:
    return f"{HASH_SPDX_HEADER}{content.lstrip()}"


def _get(config: dict[str, Any], path: str, fallback: object) -> object:
    current: object = config
    for segment in path.split("."):
        if not isinstance(current, Mapping):
            return fallback
        mapping = cast(Mapping[object, object], current)
        if segment not in mapping:
            return fallback
        current = mapping[segment]
    return current


def _truthy_string(value: object) -> str:
    return str(value).strip()


def _configured_string(config: dict[str, Any], path: str, fallback: str) -> str:
    configured = _truthy_string(_get(config, path, ""))
    return configured or fallback


def _configured_bool(config: dict[str, Any], path: str, *, fallback: bool) -> bool:
    configured = _get(config, path, fallback)
    if isinstance(configured, bool):
        return configured
    if isinstance(configured, str):
        return configured.strip().lower() in {"1", "true", "yes", "on"}
    return bool(configured)


def _configured_int(config: dict[str, Any], path: str, fallback: int) -> int:
    configured = _get(config, path, fallback)
    if isinstance(configured, int):
        return configured
    if isinstance(configured, str):
        try:
            return int(configured.strip())
        except ValueError:
            return fallback
    return fallback


def ci_config_enabled(config: dict[str, Any], path: str, *, default: bool) -> bool:
    """Return whether a generated CI config path is enabled."""
    return bool(_get(config, path, default))


def render_github_sarif_workflow(config: dict[str, Any]) -> str:
    """Render a consumer GitHub Actions workflow for coding-ethos SARIF."""
    coding_ethos_path = _configured_string(
        config,
        "generated_config.ci.github_actions.coding_ethos_path",
        ".",
    )
    repo_root = _configured_string(
        config,
        "generated_config.ci.github_actions.repo_root",
        ".",
    )
    gate_command = _configured_string(
        config,
        "generated_config.ci.github_actions.gate_command",
        "make check",
    )
    sarif_path = _configured_string(
        config,
        "generated_config.ci.github_actions.sarif_path",
        "coding-ethos.sarif",
    )
    artifact_name = _configured_string(
        config,
        "generated_config.ci.github_actions.artifact_name",
        "coding-ethos-audit",
    )
    sarif_category = _configured_string(
        config,
        "generated_config.ci.github_actions.sarif_category",
        "policy",
    )
    timeout_minutes = _configured_int(
        config,
        "generated_config.ci.github_actions.timeout_minutes",
        30,
    )
    standalone_triggers = _configured_bool(
        config,
        "generated_config.ci.github_actions.standalone_triggers",
        fallback=False,
    )
    workflow_triggers = "on:\n  workflow_call:\n"
    if standalone_triggers:
        workflow_triggers = """on:
  workflow_call:
  pull_request:
  push:
    branches:
      - main
  workflow_dispatch:
"""
    return _with_hash_spdx_header(
        f"""
name: Coding Ethos SARIF Gate

{workflow_triggers}

permissions:
  actions: read
  contents: read
  security-events: write

jobs:
  coding-ethos:
    name: Coding Ethos SARIF Gate
    runs-on: ubuntu-latest
    timeout-minutes: {timeout_minutes}
    env:
      CODING_ETHOS_PATH: {coding_ethos_path}
      CODING_ETHOS_REPO_ROOT: {repo_root}
      CODING_ETHOS_GATE_COMMAND: {gate_command}
      CODING_ETHOS_SARIF_PATH: {sarif_path}
      CODING_ETHOS_SARIF_CATEGORY: {sarif_category}
      CODING_ETHOS_FILES: ""
    steps:
      - name: Check out repository
        uses: actions/checkout@v6
        with:
          fetch-depth: 0
          submodules: recursive

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: ${{{{ env.CODING_ETHOS_PATH }}}}/go/go.mod
          cache-dependency-path: ${{{{ env.CODING_ETHOS_PATH }}}}/go/go.sum

      - name: Set up Python
        uses: actions/setup-python@v6
        with:
          python-version: "3.13"

      - name: Install uv
        uses: astral-sh/setup-uv@v7
        with:
          enable-cache: true

      - name: Build coding-ethos runtime
        run: make -C "$CODING_ETHOS_PATH" build

      - name: Run project gate
        id: project-gate
        continue-on-error: true
        run: |
          if [ -n "$CODING_ETHOS_GATE_COMMAND" ]; then
            cd "$CODING_ETHOS_REPO_ROOT"
            bash -c "$CODING_ETHOS_GATE_COMMAND"
          fi

      - name: Emit coding-ethos SARIF
        id: emit-sarif
        if: ${{{{ always() }}}}
        continue-on-error: true
        run: |
          set +e
          repo_root="$(cd "$CODING_ETHOS_REPO_ROOT" && pwd)"
          ethos_path="$(cd "$CODING_ETHOS_PATH" && pwd)"
          files_path="$(mktemp)"
          sarif_tmp="$CODING_ETHOS_SARIF_PATH.tmp"
          empty_git_sha="0000000000000000000000000000000000000000"
          rm -f "$sarif_tmp" "$CODING_ETHOS_SARIF_PATH"

          if [ -n "$CODING_ETHOS_FILES" ]; then
            printf '%s\\n' "$CODING_ETHOS_FILES" | tr ',' '\\n' > "$files_path"
          elif [ -n "${{{{ github.base_ref }}}}" ] &&
            git rev-parse --verify "origin/${{{{ github.base_ref }}}}" >/dev/null 2>&1
          then
            git diff --name-only "origin/${{{{ github.base_ref }}}}"...HEAD |
              while IFS= read -r path; do
                [ -f "$path" ] && printf '%s\\n' "$path"
              done > "$files_path"
          elif [ "${{{{ github.event_name }}}}" = "push" ] &&
            [ -n "${{{{ github.event.before }}}}" ] &&
            [ "${{{{ github.event.before }}}}" != "$empty_git_sha" ] &&
            git rev-parse --verify "${{{{ github.event.before }}}}^{{commit}}" \\
              >/dev/null 2>&1
          then
            git diff --name-only \\
              "${{{{ github.event.before }}}}" \\
              "${{{{ github.sha }}}}" |
              while IFS= read -r path; do
                [ -f "$path" ] && printf '%s\\n' "$path"
              done > "$files_path"
          else
            : > "$files_path"
          fi

          "$ethos_path/bin/coding-ethos-run" policy-lint \\
            --cwd "$repo_root" \\
            --scope files \\
            --files-from "$files_path" \\
            --sarif > "$sarif_tmp"
          status=$?
          if [ -s "$sarif_tmp" ]; then
            mv "$sarif_tmp" "$CODING_ETHOS_SARIF_PATH"
          else
            rm -f "$sarif_tmp"
          fi
          exit "$status"

      - name: Upload coding-ethos SARIF
        if: ${{{{ always() && hashFiles(env.CODING_ETHOS_SARIF_PATH) != '' }}}}
        uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: ${{{{ env.CODING_ETHOS_SARIF_PATH }}}}
          category: ${{{{ env.CODING_ETHOS_SARIF_CATEGORY }}}}

      - name: Upload coding-ethos audit artifacts
        if: ${{{{ always() }}}}
        uses: actions/upload-artifact@v7
        with:
          name: {artifact_name}
          if-no-files-found: ignore
          path: |
            ${{{{ env.CODING_ETHOS_SARIF_PATH }}}}
            ${{{{ env.CODING_ETHOS_REPO_ROOT }}}}/.coding-ethos/lint-runs/
            ${{{{ env.CODING_ETHOS_REPO_ROOT }}}}/.coding-ethos/hook-runs/

      - name: Fail on coding-ethos violations
        if: >-
          ${{{{ steps.project-gate.outcome == 'failure' ||
          steps.emit-sarif.outcome == 'failure' }}}}
        run: exit 1
"""
    )
