# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile sync contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _makefile_lines,
)


def test_tool_config_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    for target in (
        "sync-tool-configs",
        "sync-consumer-tool-configs",
        "fix-configs",
    ):
        assert f"{target}: ensure-go" in makefile
        assert f"{target}: ensure-uv" not in lines
    assert "check-tool-configs: ensure-hook-runtime" in makefile

    tool_config_block = makefile.split("sync-gemini-prompts:", maxsplit=1)[0].split(
        "sync-tool-configs:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in tool_config_block
    assert "--sync-tool-configs" not in tool_config_block
    assert "--check-tool-configs" not in tool_config_block
    check_tool_config_block = makefile.split("sync-gemini-prompts:", maxsplit=1)[
        0
    ].split("check-tool-configs:", maxsplit=1)[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_tool_config_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_tool_config_block
