# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Contract checks for Makefile hook-runtime ownership behavior.

The Makefile is the operator surface for installing hooks and repairing local
runtime artifacts. These tests pin important ordering guarantees that protect
active consumer repos from generated-config drift during hook rebuilds.
"""

from pathlib import Path


def test_build_syncs_consumer_generated_outputs_before_runtime_install() -> None:
    makefile = Path("Makefile").read_text(encoding="utf-8")
    lines = makefile.splitlines()

    assert "sync-consumer-tool-configs:" in makefile
    assert "_sync-agent-skills:" in makefile
    assert "_sync-consumer-agent-skills:" in makefile
    assert "sync-agent-skills: ensure-uv" not in lines
    assert "sync-consumer-agent-skills: ensure-uv" not in lines
    build_line = next(
        line for line in makefile.splitlines() if line.startswith("build:")
    )

    assert "sync-tool-configs" in build_line
    assert "sync-consumer-tool-configs" in build_line
    assert "_sync-agent-skills" in build_line
    assert "_sync-consumer-agent-skills" in build_line
    assert build_line.index("sync-consumer-tool-configs") < build_line.index(
        "managed-toolchain-install"
    )
    assert build_line.index("_sync-agent-skills") < build_line.index(
        "managed-toolchain-install"
    )
    assert build_line.index("_sync-consumer-agent-skills") < build_line.index(
        "managed-toolchain-install"
    )


def test_agent_skill_sync_is_not_user_facing() -> None:
    makefile = Path("Makefile").read_text(encoding="utf-8")
    lines = makefile.splitlines()

    phony_block = makefile.split("##@ Help", maxsplit=1)[0]

    assert "\tsync-agent-skills \\" not in phony_block
    assert "\t_sync-agent-skills \\" not in phony_block
    assert "\tsync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-consumer-agent-skills \\" not in phony_block
    assert "_sync-agent-skills: ensure-uv\n" in makefile
    assert "_sync-agent-skills: ensure-uv ##" not in lines
    assert "_sync-consumer-agent-skills: ensure-uv\n" in makefile
    assert "_sync-consumer-agent-skills: ensure-uv ##" not in lines
