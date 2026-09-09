# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile sync contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _assert_internal_target,
    _makefile_lines,
)


def test_agent_skill_sync_is_not_user_facing() -> None:
    makefile, lines = _makefile_lines()

    phony_block = makefile.split("##@ Help", maxsplit=1)[0]

    assert "\tsync-agent-skills \\" not in phony_block
    assert "\t_sync-agent-skills \\" not in phony_block
    assert "\tsync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-git-hooks \\" not in phony_block
    assert "\t_sync-parent-hook-runtime \\" not in phony_block
    assert "_sync-agent-skills: ensure-go\n" in makefile
    assert "_sync-consumer-agent-skills: ensure-go\n" in makefile
    assert (
        "_sync-git-hooks: ensure-go go-tools-install _sync-parent-hook-runtime\n"
        in makefile
    )
    assert (
        "_sync-parent-hook-runtime: ensure-go go-tools-install "
        "go-hook-runner-install policy-bundle-install\n" in makefile
    )
    for target in (
        "_sync-agent-skills",
        "_sync-consumer-agent-skills",
        "_sync-git-hooks",
        "_sync-parent-hook-runtime",
    ):
        _assert_internal_target(makefile, lines, target)
