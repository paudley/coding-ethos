# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Render Codex/AGENTS root files and managed addenda.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle
from coding_ethos.renderer_common import (
    GENERATED_NOTICE,
    agent_operating_discipline_lines,
    command_lines,
    join_lines,
    path_lines,
    principle_lines,
    repo_display_name,
    with_markdown_spdx,
)


def render_agents_md(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the repo's generated `AGENTS.md` root file."""
    codex_profile = bundle.agent_profiles.get("codex")
    lines = [
        GENERATED_NOTICE,
        "# AGENTS.md",
        "",
        "## Purpose",
        f"- Repo: `{repo_display_name(bundle, repo_root)}`",
    ]
    if bundle.repo.overview:
        lines.append(f"- Overview: {bundle.repo.overview}")
    elif bundle.overview:
        lines.append(f"- Shared ethos: {bundle.overview.replace(chr(10), ' ')}")

    rendered_command_lines = command_lines(bundle)
    if rendered_command_lines:
        lines.extend(["", "## Commands", *rendered_command_lines])

    rendered_path_lines = path_lines(bundle)
    if rendered_path_lines:
        lines.extend(["", "## Key Paths", *rendered_path_lines])

    if bundle.repo.notes:
        lines.extend(
            ["", "## Repo Notes", *[f"- {note}" for note in bundle.repo.notes]]
        )

    operating_lines = agent_operating_discipline_lines(bundle)
    if operating_lines:
        lines.extend(["", *operating_lines])

    lines.extend(
        [
            "",
            "## Non-Negotiable Ethos",
            *principle_lines(bundle.principles),
            "",
            "## Detail Docs",
            (
                "- Deep reference notes live in `.agents/ethos/README.md` "
                "and the linked per-principle docs."
            ),
        ]
    )

    combined_notes: list[str] = []
    if codex_profile:
        combined_notes.extend(codex_profile.notes)
    combined_notes.extend(bundle.repo.agent_notes.get("codex", []))
    if combined_notes:
        lines.extend(["", "## Codex Notes", *[f"- {note}" for note in combined_notes]])

    return join_lines(with_markdown_spdx(lines))


def render_agents_addendum(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the managed additive AGENTS block used in inject merges."""
    codex_profile = bundle.agent_profiles.get("codex")
    lines = [
        "## Coding Ethos",
        f"- Managed additive guidance for `{repo_display_name(bundle, repo_root)}`.",
        (
            "- Keep the hand-written repo guidance above authoritative for "
            "repo-specific operations and edge cases."
        ),
        "- Shared ethos index: `.agents/ethos/README.md`.",
        "- Per-principle detail docs: `.agents/ethos/*.md`.",
        "- Optional prompt addon: `.agent-context/prompt-addons/codex.md`.",
        (
            "- Update `repo_ethos.yml` when repo-specific guidance should "
            "refine shared ethos behavior."
        ),
    ]
    operating_lines = agent_operating_discipline_lines(
        bundle,
        heading="### Agent Operating Discipline",
    )
    if operating_lines:
        lines.extend(["", *operating_lines])
    lines.extend(["", "### Ethos Priorities", *principle_lines(bundle.principles)])

    combined_notes: list[str] = []
    if codex_profile:
        combined_notes.extend(codex_profile.notes)
    combined_notes.extend(bundle.repo.agent_notes.get("codex", []))
    if combined_notes:
        lines.extend(["", "### Codex Notes", *[f"- {note}" for note in combined_notes]])

    return join_lines(with_markdown_spdx(lines))
