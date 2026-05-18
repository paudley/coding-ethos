# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Public renderer API for generated coding-ethos files.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle
from coding_ethos.renderer_agents import render_agents_addendum, render_agents_md
from coding_ethos.renderer_common import AgentRootSurface
from coding_ethos.renderer_details import render_principle_detail
from coding_ethos.renderer_ethos import render_ethos_md, render_shared_ethos_index
from coding_ethos.renderer_providers import (
    render_claude_addendum,
    render_claude_md,
    render_claude_memory,
    render_gemini_addendum,
    render_gemini_md,
    render_prompt_addon,
)

__all__ = [
    "agent_root_surfaces",
    "render_agent_addendum",
    "render_agent_root_outputs",
    "render_claude_memory",
    "render_ethos_md",
    "render_principle_detail",
    "render_prompt_addon",
    "render_shared_ethos_index",
    "required_root_imports",
    "root_merge_topics",
]


def render_claude_root(bundle: EthosBundle, repo_root: Path) -> str:
    """Provide focused helper behavior for the split module."""
    del repo_root
    return render_claude_md(bundle)


_AGENT_ROOT_SURFACES: tuple[AgentRootSurface, ...] = (
    AgentRootSurface(
        agent="codex",
        path="AGENTS.md",
        render_root=render_agents_md,
        render_addendum=render_agents_addendum,
        merge_topics=("repo commands", "key paths", "repo operating notes"),
    ),
    AgentRootSurface(
        agent="claude",
        path="CLAUDE.md",
        render_root=render_claude_root,
        render_addendum=render_claude_addendum,
        imports=("@AGENTS.md", "@.claude/ethos/MEMORY.md"),
        merge_topics=(
            "Claude imports",
            "memory links",
            "Claude-specific workflow notes",
        ),
    ),
    AgentRootSurface(
        agent="gemini",
        path="GEMINI.md",
        render_root=render_gemini_md,
        render_addendum=render_gemini_addendum,
        imports=("@AGENTS.md",),
        merge_topics=(
            "Gemini root guidance",
            "linked detail docs",
            "repo operating notes",
        ),
    ),
)


def agent_root_surfaces() -> tuple[AgentRootSurface, ...]:
    """Return registered generated root-file surfaces for supported agents."""
    return _AGENT_ROOT_SURFACES


def required_root_imports(target_name: str) -> list[str]:
    """Return required import lines for a generated root file."""
    for surface in _AGENT_ROOT_SURFACES:
        if surface.path == target_name:
            return list(surface.imports)
    return []


def render_agent_root_outputs(bundle: EthosBundle, repo_root: Path) -> dict[str, str]:
    """Render registered root files for all supported agent surfaces."""
    return {
        surface.path: surface.render_root(bundle, repo_root)
        for surface in _AGENT_ROOT_SURFACES
    }


def render_agent_addendum(
    bundle: EthosBundle, repo_root: Path, target_name: str, fallback: str
) -> str:
    """Render the managed addendum for a registered root file."""
    for surface in _AGENT_ROOT_SURFACES:
        if surface.path == target_name:
            return surface.render_addendum(bundle, repo_root)
    return fallback


def root_merge_topics(target_name: str) -> list[str]:
    """Return root-file-specific merge topics for registered agent surfaces."""
    for surface in _AGENT_ROOT_SURFACES:
        if surface.path == target_name:
            return list(surface.merge_topics)
    return []
