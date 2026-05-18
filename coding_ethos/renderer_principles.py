# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Shared principle-list formatting helpers for generated Markdown.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle, EthosSkill, Principle


def repo_display_name(bundle: EthosBundle, repo_root: Path) -> str:
    """Provide focused helper behavior for the split module."""
    return bundle.repo.name or repo_root.name


def format_quick_ref(items: list[str], limit: int = 3) -> str:
    """Provide focused helper behavior for the split module."""
    return " | ".join(item.strip() for item in items[:limit])


def skill_entrypoint(skill: EthosSkill) -> str:
    """Provide focused helper behavior for the split module."""
    return f"{skill.id}/SKILL.md"


def skills_by_id(bundle: EthosBundle, skill_id: str) -> list[EthosSkill]:
    """Provide focused helper behavior for the split module."""
    return [skill for skill in bundle.skills if skill.id == skill_id]


def agent_operating_discipline_lines(
    bundle: EthosBundle,
    *,
    heading: str = "## Agent Operating Discipline",
) -> list[str]:
    """Provide focused helper behavior for the split module."""
    matching_skills = skills_by_id(bundle, "agent-operating-discipline")
    if not matching_skills:
        return []
    skill = matching_skills[0]

    return [
        heading,
        (
            "- Load `.agents/skills/"
            f"{skill_entrypoint(skill)}` before broad implementation, "
            "refactor, review, or debugging work."
        ),
        (
            "- State task interpretation, assumptions, ambiguity, and "
            "trade-offs before broad changes."
        ),
        (
            "- Prefer the smallest sufficient implementation; avoid "
            "speculative abstractions, options, and extension points."
        ),
        (
            "- Keep edits surgical: every changed line should trace to the "
            "request or cleanup directly caused by the change."
        ),
        (
            "- Define verifiable success criteria and run focused checks "
            "before claiming completion."
        ),
    ]


def append_repo_context(lines: list[str], bundle: EthosBundle, repo_root: Path) -> None:
    """Provide focused helper behavior for the split module."""
    if not (bundle.repo.name or bundle.repo.overview or bundle.repo.notes):
        return
    lines.extend(["## Repo Context"])
    if bundle.repo.name:
        lines.append(f"- Repo: `{repo_display_name(bundle, repo_root)}`")
    if bundle.repo.overview:
        lines.append(f"- Overview: {bundle.repo.overview}")
    lines.extend(f"- {note}" for note in bundle.repo.notes)
    lines.append("")


def principle_lines(principles: list[Principle]) -> list[str]:
    """Provide focused helper behavior for the split module."""
    lines: list[str] = []
    for principle in principles:
        primary_line = principle.directive or principle.summary
        if principle.tags:
            lines.append(
                f"- `{principle.order:02d}. {principle.title}`: {primary_line} "
                f"[tags: {', '.join(principle.tags)}]"
            )
        else:
            lines.append(
                f"- `{principle.order:02d}. {principle.title}`: {primary_line}"
            )
        if principle.quick_ref:
            lines.append(f"  Quick ref: {format_quick_ref(principle.quick_ref)}")
    return lines
