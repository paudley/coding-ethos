# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Assertions for generated CLI rendering artifacts.

Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path


def _assert_rendered_targets(repo_root: Path) -> None:
    _assert_rendered_root_files(repo_root)
    _assert_rendered_detail_docs(repo_root)
    assert (repo_root / ".agent-context/prompt-addons/codex.md").exists()


def _assert_rendered_root_files(repo_root: Path) -> None:
    agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
    claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
    ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
    gemini_md = (repo_root / "GEMINI.md").read_text(encoding="utf-8")
    assert "Widget Service" in agents_md
    assert "Processes widgets." in agents_md
    assert "Enforce simple SOLID designs." in agents_md
    assert "Quick ref:" in agents_md
    assert "# Test Ethos" in ethos_md
    assert "## Repo Context" in ethos_md
    assert "## 01. SOLID is Law" in ethos_md
    assert "### Directive" in ethos_md
    assert "@AGENTS.md" in claude_md
    assert "Open the matching ethos doc" in claude_md
    assert "@AGENTS.md" in gemini_md


def _assert_rendered_detail_docs(repo_root: Path) -> None:
    detail_doc = (repo_root / ".agents/ethos/solid-is-law.md").read_text(
        encoding="utf-8"
    )
    memory_index = (repo_root / ".claude/ethos/MEMORY.md").read_text(encoding="utf-8")
    assert "## Overview" in detail_doc
    assert "## Quick Ref" in detail_doc
    assert "## Merge Topics" in detail_doc
    assert "## Agent Hints" in detail_doc
    assert "## Repo Addendum" in detail_doc
    assert "../../.agents/ethos/solid-is-law.md" in memory_index
