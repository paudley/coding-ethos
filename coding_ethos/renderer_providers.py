# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Render Claude, Gemini, and prompt-addon surfaces.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle
from coding_ethos.renderer_common import (
    GENERATED_NOTICE,
    agent_operating_discipline_lines,
    format_quick_ref,
    join_lines,
    principle_lines,
    repo_display_name,
    with_markdown_spdx,
)


def render_claude_md(bundle: EthosBundle) -> str:
    """Render the generated `CLAUDE.md` import hub."""
    claude_profile = bundle.agent_profiles.get("claude")
    lines = [
        GENERATED_NOTICE,
        "@AGENTS.md",
        "@.claude/ethos/MEMORY.md",
        "",
        "# Claude Code",
        "",
    ]
    if claude_profile:
        lines.extend(f"- {note}" for note in claude_profile.notes)
    else:
        lines.extend(
            [
                (
                    "- Keep the shared contract in `AGENTS.md`; use this file "
                    "only for Claude-specific guidance."
                ),
                (
                    "- The imported memory index mirrors Claude's memory "
                    "style and points at one deep note per ethos entry."
                ),
            ]
        )
    lines.append(
        "- Open the linked ethos note before changing architecture, "
        "validation, error handling, security, or delegation behavior."
    )
    operating_lines = agent_operating_discipline_lines(bundle)
    if operating_lines:
        lines.extend(["", *operating_lines])
    lines.extend(f"- {note}" for note in bundle.repo.agent_notes.get("claude", []))
    return join_lines(with_markdown_spdx(lines))


def render_claude_addendum(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the managed additive Claude block used in inject merges."""
    lines = [
        "## Coding Ethos",
        f"- Managed additive guidance for `{repo_display_name(bundle, repo_root)}`.",
        (
            "- Keep the existing repo-specific Claude guidance above; this "
            "section is only the generated ethos layer."
        ),
        "- Shared ethos index: `.agents/ethos/README.md`.",
        "- Claude memory index: `.claude/ethos/MEMORY.md`.",
        "- Optional prompt addon: `.agent-context/prompt-addons/claude.md`.",
        (
            "- Open the matching ethos note before changing architecture, "
            "validation, error handling, security, or delegation behavior."
        ),
    ]
    operating_lines = agent_operating_discipline_lines(
        bundle,
        heading="### Agent Operating Discipline",
    )
    if operating_lines:
        lines.extend(["", *operating_lines])
    repo_notes = bundle.repo.agent_notes.get("claude", [])
    if repo_notes:
        lines.extend(["", "### Claude Notes", *[f"- {note}" for note in repo_notes]])
    lines.extend(
        ["", "### High-Priority Principles", *principle_lines(bundle.principles[:5])]
    )
    return join_lines(with_markdown_spdx(lines))


def render_claude_memory(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the Claude memory index file."""
    lines = [
        GENERATED_NOTICE,
        f"# {repo_display_name(bundle, repo_root)} ethos memory",
        "",
        "## Shared Ethos",
    ]
    for principle in bundle.principles:
        summary = principle.directive or principle.summary
        lines.append(
            f"- [{principle.title}](../../.agents/ethos/{principle.id}.md) - {summary}"
        )
    return join_lines(with_markdown_spdx(lines))


def render_gemini_md(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the generated `GEMINI.md` root guidance file."""
    gemini_profile = bundle.agent_profiles.get("gemini")
    lines = [
        GENERATED_NOTICE,
        "@AGENTS.md",
        "",
        f"# GEMINI.md for {repo_display_name(bundle, repo_root)}",
        "",
    ]
    if gemini_profile:
        lines.extend(f"- {note}" for note in gemini_profile.notes)
    else:
        lines.extend(
            [
                "- `AGENTS.md` is the durable project contract for this repository.",
                (
                    "- Prefer targeted reads of `.agents/ethos/README.md` "
                    "and individual detail docs instead of inlining the full "
                    "corpus."
                ),
            ]
        )
    lines.append(
        "- Keep repo-specific conventions in `repo_ethos.yml`; regenerate "
        "after updating it."
    )
    operating_lines = agent_operating_discipline_lines(bundle)
    if operating_lines:
        lines.extend(["", *operating_lines])
    lines.extend(f"- {note}" for note in bundle.repo.agent_notes.get("gemini", []))
    return join_lines(with_markdown_spdx(lines))


def render_gemini_addendum(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the managed additive Gemini block used in inject merges."""
    lines = [
        "## Coding Ethos",
        f"- Managed additive guidance for `{repo_display_name(bundle, repo_root)}`.",
        (
            "- Keep the hand-written repo guidance above; use this generated "
            "section as the shared ethos layer."
        ),
        "- Durable repo contract: `AGENTS.md`.",
        "- Shared ethos index: `.agents/ethos/README.md`.",
        "- Optional prompt addon: `.agent-context/prompt-addons/gemini.md`.",
    ]
    operating_lines = agent_operating_discipline_lines(
        bundle,
        heading="### Agent Operating Discipline",
    )
    if operating_lines:
        lines.extend(["", *operating_lines])
    repo_notes = bundle.repo.agent_notes.get("gemini", [])
    if repo_notes:
        lines.extend(["", "### Gemini Notes", *[f"- {note}" for note in repo_notes]])
    lines.extend(
        ["", "### High-Priority Principles", *principle_lines(bundle.principles[:5])]
    )
    return join_lines(with_markdown_spdx(lines))


def render_prompt_addon(bundle: EthosBundle, agent: str, repo_root: Path) -> str:
    """Render the small fallback prompt addon for one supported agent."""
    top_principles = bundle.principles[:5]
    lines = [
        f"# {agent.title()} prompt addon",
        "",
        f"You are working in `{repo_display_name(bundle, repo_root)}`.",
        "Treat `AGENTS.md` as the source of truth when it is present.",
        (
            "If a task touches architecture, validation, error handling, or "
            "delegation, consult the matching detail doc under "
            "`.agents/ethos/` before acting."
        ),
        "",
        "Core principles:",
    ]
    lines.extend(
        f"- {principle.title}: {principle.directive or principle.summary}"
        for principle in top_principles
    )
    lines.extend(
        (
            f"- {principle.title} quick ref: "
            f"{format_quick_ref(principle.quick_ref, limit=2)}"
        )
        for principle in top_principles
        if principle.quick_ref
    )
    return join_lines(with_markdown_spdx(lines))
