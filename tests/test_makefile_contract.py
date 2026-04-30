# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Contract checks for Makefile hook-runtime ownership behavior.

The Makefile is the operator surface for installing hooks and repairing local
runtime artifacts. These tests pin important ordering guarantees that protect
active consumer repos from generated-config drift during hook rebuilds.
"""

from pathlib import Path


def test_build_syncs_consumer_tool_configs_before_runtime_install() -> None:
    makefile = Path("Makefile").read_text(encoding="utf-8")

    assert "sync-consumer-tool-configs:" in makefile
    build_line = next(
        line for line in makefile.splitlines() if line.startswith("build:")
    )

    assert "sync-tool-configs" in build_line
    assert "sync-consumer-tool-configs" in build_line
    assert build_line.index("sync-consumer-tool-configs") < build_line.index(
        "managed-toolchain-install"
    )
