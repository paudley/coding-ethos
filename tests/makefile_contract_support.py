# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Shared Makefile parsing helpers for contract tests.

The helpers expose target-line and target-body parsing for focused contract tests.


Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path


def _makefile_lines() -> tuple[str, list[str]]:
    makefile = Path("Makefile").read_text(encoding="utf-8")
    return makefile, makefile.splitlines()


def _build_target_line(makefile: str) -> str:
    return next(line for line in makefile.splitlines() if line.startswith("build:"))


def _target_block(makefile: str, target: str) -> str:
    lines = makefile.splitlines()
    start = next(
        index for index, line in enumerate(lines) if line.startswith(f"{target}:")
    )
    end = len(lines)
    for index in range(start + 1, len(lines)):
        line = lines[index]
        if line and not line.startswith(("\t", " ", "#")) and ":" in line:
            end = index
            break
    return "\n".join(lines[start:end])


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
