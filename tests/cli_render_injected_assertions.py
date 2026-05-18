# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Assertions for managed injected root-file blocks.

Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path


def _assert_injected_root_files(repo_root: Path) -> None:
    _assert_injected_agents_md(repo_root)
    _assert_injected_claude_md(repo_root)
    _assert_injected_ethos_md(repo_root)
    assert (repo_root / ".agents/ethos/solid-is-law.md").exists()


def _assert_injected_agents_md(repo_root: Path) -> None:
    agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
    assert "# Existing agents" in agents_md
    assert "Keep this." in agents_md
    assert "<!-- coding-ethos:begin managed AGENTS.md -->" in agents_md
    assert "## Coding Ethos" in agents_md
    assert ".agents/ethos/README.md" in agents_md


def _assert_injected_claude_md(repo_root: Path) -> None:
    claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
    assert "# Existing claude" in claude_md
    assert "Local workflow notes." in claude_md
    assert "<!-- coding-ethos:begin imports CLAUDE.md -->" in claude_md
    assert "@AGENTS.md" in claude_md
    assert "@.claude/ethos/MEMORY.md" in claude_md
    assert "<!-- coding-ethos:begin managed CLAUDE.md -->" in claude_md


def _assert_injected_ethos_md(repo_root: Path) -> None:
    ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
    assert "Stale content." not in ethos_md
    assert "# Test Ethos" in ethos_md
    assert "## 01. SOLID is Law" in ethos_md
