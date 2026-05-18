# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile sync contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _makefile_lines,
)


def test_gemini_prompt_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    assert "sync-gemini-prompts: ensure-go" in makefile
    assert "check-gemini-prompts: ensure-hook-runtime" in makefile
    assert "sync-gemini-prompts: ensure-uv" not in lines
    assert "check-gemini-prompts: ensure-uv" not in lines

    gemini_block = makefile.split("_sync-agent-skills:", maxsplit=1)[0].split(
        "sync-gemini-prompts:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in gemini_block
    assert "--sync-gemini-prompts" not in gemini_block
    assert "--check-gemini-prompts" not in gemini_block
    check_gemini_block = makefile.split("_sync-agent-skills:", maxsplit=1)[0].split(
        "check-gemini-prompts:",
        maxsplit=1,
    )[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_gemini_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_gemini_block
