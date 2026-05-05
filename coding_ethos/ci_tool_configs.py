# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Render generated CI configuration files for coding-ethos gates.

This module owns provider-specific CI templates so generated tool config
orchestration stays below the line-limit gate. The generated files are normal
managed config artifacts, which means `sync-tool-configs` writes them and
`check-tool-configs` verifies them through the shared hash manifest.
"""

from typing import Any

from coding_ethos.config_access import (
    configured_bool,
    configured_choice,
    configured_int,
    configured_string,
)

HASH_SPDX_HEADER = (
    "# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. "
    "<paudley@blackcat.ca>\n"
    "# SPDX-License-Identifier: MIT\n\n"
)
GENERATED_CI_CONFIGS: tuple[str, ...] = (
    ".github/workflows/coding-ethos-sarif.yml",
    ".gitlab-ci.yml",
)
SANDBOX_MODES = {"auto", "off", "required"}
SETUP_UV_USES_LINE = (
    "        uses: astral-sh/setup-uv@37802adc94f370d6bfd71619e3f0bf239e1f3b78"
)


def _with_hash_spdx_header(content: str) -> str:
    return f"{HASH_SPDX_HEADER}{content.lstrip()}"


def ci_config_enabled(config: dict[str, Any], path: str, *, default: bool) -> bool:
    """Return whether a generated CI config path is enabled."""
    return configured_bool(config, path, fallback=default)


def render_github_sarif_workflow(config: dict[str, Any]) -> str:
    """Render a consumer GitHub Actions workflow for coding-ethos SARIF."""
    coding_ethos_path = configured_string(
        config,
        "generated_config.ci.github_actions.coding_ethos_path",
        ".",
    )
    repo_root = configured_string(
        config,
        "generated_config.ci.github_actions.repo_root",
        ".",
    )
    gate_command = configured_string(
        config,
        "generated_config.ci.github_actions.gate_command",
        "make check",
    )
    sarif_path = configured_string(
        config,
        "generated_config.ci.github_actions.sarif_path",
        "coding-ethos.sarif",
    )
    artifact_name = configured_string(
        config,
        "generated_config.ci.github_actions.artifact_name",
        "coding-ethos-audit",
    )
    sarif_category = configured_string(
        config,
        "generated_config.ci.github_actions.sarif_category",
        "policy",
    )
    sandbox_mode = configured_choice(
        config,
        "generated_config.ci.github_actions.sandbox_mode",
        "required",
        SANDBOX_MODES,
    )
    timeout_minutes = configured_int(
        config,
        "generated_config.ci.github_actions.timeout_minutes",
        30,
    )
    standalone_triggers = configured_bool(
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
  contents: read

jobs:
  coding-ethos:
    name: Coding Ethos SARIF Gate
    runs-on: ubuntu-latest
    timeout-minutes: {timeout_minutes}
    permissions:
      actions: read
      contents: read
      security-events: write
    env:
      CODING_ETHOS_PATH: {coding_ethos_path}
      CODING_ETHOS_REPO_ROOT: {repo_root}
      CODING_ETHOS_GATE_COMMAND: {gate_command}
      CODING_ETHOS_SARIF_PATH: {sarif_path}
      CODING_ETHOS_SARIF_CATEGORY: {sarif_category}
      CODING_ETHOS_SANDBOX_MODE: {sandbox_mode}
      CODING_ETHOS_FILES: ""
      CODING_ETHOS_GITHUB_BASE_REF: ${{{{ github.base_ref }}}}
      CODING_ETHOS_GITHUB_EVENT_NAME: ${{{{ github.event_name }}}}
      CODING_ETHOS_GITHUB_EVENT_BEFORE: ${{{{ github.event.before }}}}
      CODING_ETHOS_GITHUB_SHA: ${{{{ github.sha }}}}
    steps:
      - name: Check out repository
        uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd
        with:
          fetch-depth: 0
          submodules: recursive

      - name: Set up Go
        uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c
        with:
          go-version-file: ${{{{ env.CODING_ETHOS_PATH }}}}/go/go.mod
          cache-dependency-path: ${{{{ env.CODING_ETHOS_PATH }}}}/go/go.sum

      - name: Set up Python
        uses: actions/setup-python@a309ff8b426b58ec0e2a45f0f869d46889d02405
        with:
          python-version: "3.13"

      - name: Install uv
{SETUP_UV_USES_LINE}
        with:
          enable-cache: true

      - name: Build coding-ethos runtime
        env:
          GITHUB_TOKEN: ${{{{ github.token }}}}
        run: make -C "$CODING_ETHOS_PATH" build

      - name: Run project gate
        id: project-gate
        continue-on-error: true
        run: |
          ethos_path="$(cd "$CODING_ETHOS_PATH" && pwd)"
          export PATH="$ethos_path/bin:$PATH"
          if [ -n "$CODING_ETHOS_GATE_COMMAND" ]; then
            cd "$CODING_ETHOS_REPO_ROOT"
            bash -c "$CODING_ETHOS_GATE_COMMAND"
          fi

      - name: Emit coding-ethos SARIF
        id: emit-sarif
        if: ${{{{ always() }}}}
        continue-on-error: true
        run: '"$CODING_ETHOS_PATH/bin/coding-ethos-run" ci-sarif --provider github'

      - name: Upload coding-ethos SARIF
        if: ${{{{ always() && hashFiles(env.CODING_ETHOS_SARIF_PATH) != '' }}}}
        uses: github/codeql-action/upload-sarif@e46ed2cbd01164d986452f91f178727624ae40d7
        with:
          sarif_file: ${{{{ env.CODING_ETHOS_SARIF_PATH }}}}
          category: ${{{{ env.CODING_ETHOS_SARIF_CATEGORY }}}}

      - name: Upload coding-ethos audit artifacts
        if: ${{{{ always() }}}}
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
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
