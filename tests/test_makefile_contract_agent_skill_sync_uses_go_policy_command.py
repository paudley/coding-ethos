# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile sync contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _makefile_lines,
)


def test_agent_skill_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    assert "_sync-agent-skills: ensure-go" in makefile
    assert "check-agent-skills: ensure-hook-runtime" in makefile
    assert "_sync-agent-skills: ensure-uv" not in lines
    assert "check-agent-skills: ensure-uv" not in lines

    skill_block = makefile.split("_sync-agent-hooks:", maxsplit=1)[0].split(
        "_sync-agent-skills:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in skill_block
    assert "--sync-agent-skills" not in skill_block
    assert "--check-agent-skills" not in skill_block
    check_skill_block = makefile.split("build:", maxsplit=1)[0].split(
        "check-agent-skills:",
        maxsplit=1,
    )[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_skill_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_skill_block
