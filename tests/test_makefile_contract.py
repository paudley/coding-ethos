# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Contract checks for Makefile hook-runtime ownership behavior.

The Makefile is the operator surface for installing hooks and repairing local
runtime artifacts. These tests pin important ordering guarantees that protect
active consumer repos from generated-config drift during hook rebuilds.
"""

from pathlib import Path


def _makefile_lines() -> tuple[str, list[str]]:
    makefile = Path("Makefile").read_text(encoding="utf-8")
    return makefile, makefile.splitlines()


def _build_target_line(makefile: str) -> str:
    return next(line for line in makefile.splitlines() if line.startswith("build:"))


def _assert_build_target_order(build_line: str) -> None:
    prerequisites = (
        "sync-consumer-tool-configs",
        "_sync-agent-skills",
        "_sync-consumer-agent-skills",
        "_sync-git-hooks",
    )
    for prerequisite in prerequisites:
        assert build_line.index(prerequisite) < build_line.index(
            "managed-toolchain-install"
        )
    assert build_line.index("policy-bundle-install") < build_line.index(
        "_sync-parent-hook-runtime"
    )


def _assert_internal_target(makefile: str, lines: list[str], target: str) -> None:
    assert f"{target}:" in makefile
    assert f"{target}: ensure-uv ##" not in lines
    assert f"{target}: ensure-go go-tools-install ##" not in lines


def test_build_syncs_consumer_generated_outputs_before_runtime_install() -> None:
    makefile, lines = _makefile_lines()

    assert "sync-consumer-tool-configs:" in makefile
    assert "sync-agent-skills: ensure-uv" not in lines
    assert "sync-consumer-agent-skills: ensure-uv" not in lines
    build_line = _build_target_line(makefile)

    assert "sync-tool-configs" in build_line
    assert "sync-consumer-tool-configs" in build_line
    assert "_sync-agent-skills" in build_line
    assert "_sync-consumer-agent-skills" in build_line
    assert "_sync-git-hooks" in build_line
    assert "_sync-parent-hook-runtime" in build_line
    _assert_build_target_order(build_line)


def test_agent_skill_sync_is_not_user_facing() -> None:
    makefile, lines = _makefile_lines()

    phony_block = makefile.split("##@ Help", maxsplit=1)[0]

    assert "\tsync-agent-skills \\" not in phony_block
    assert "\t_sync-agent-skills \\" not in phony_block
    assert "\tsync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-git-hooks \\" not in phony_block
    assert "\t_sync-parent-hook-runtime \\" not in phony_block
    assert "_sync-agent-skills: ensure-uv\n" in makefile
    assert "_sync-consumer-agent-skills: ensure-uv\n" in makefile
    assert "_sync-git-hooks: ensure-go go-tools-install\n" in makefile
    assert (
        "_sync-parent-hook-runtime: ensure-go go-tools-install policy-bundle-install\n"
        in makefile
    )
    for target in (
        "_sync-agent-skills",
        "_sync-consumer-agent-skills",
        "_sync-git-hooks",
        "_sync-parent-hook-runtime",
    ):
        _assert_internal_target(makefile, lines, target)
