# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Render shared ethos indexes and per-principle detail docs.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle, Principle
from coding_ethos.renderer_common import (
    GENERATED_NOTICE,
    ethos_title,
    join_lines,
    repo_display_name,
    with_markdown_spdx,
)
from coding_ethos.renderer_details import render_principle_detail

__all__ = [
    "render_ethos_md",
    "render_principle_detail",
    "render_shared_ethos_index",
]


def render_shared_ethos_index(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the shared `.agents/ethos/README.md` index."""
    lines = [
        GENERATED_NOTICE,
        f"# {repo_display_name(bundle, repo_root)} ethos reference",
        "",
        "## Principles",
    ]
    for principle in bundle.principles:
        summary = principle.directive or principle.summary
        lines.append(
            f"- [{principle.order:02d}. {principle.title}]"
            f"({principle.id}.md) - {summary}"
        )
    return join_lines(with_markdown_spdx(lines))


def ethos_repo_context_lines(bundle: EthosBundle, repo_root: Path) -> list[str]:
    """Provide focused helper behavior for the split module."""
    if not (bundle.repo.name or bundle.repo.overview or bundle.repo.notes):
        return []

    lines = ["## Repo Context", f"- Repo: `{repo_display_name(bundle, repo_root)}`"]
    if bundle.repo.overview:
        lines.append(f"- Overview: {bundle.repo.overview}")
    lines.extend(f"- {note}" for note in bundle.repo.notes)
    lines.append("")

    return lines


def principle_axiom_lines(principle: Principle) -> list[str]:
    """Provide focused helper behavior for the split module."""
    if not principle.axioms:
        return []

    lines = ["### Axioms"]
    lines.extend(
        f"- {axiom.axiom}" + (f" {axiom.action}" if axiom.action else "")
        for axiom in principle.axioms
    )
    lines.append("")

    return lines


def ethos_principle_lines(principle: Principle) -> list[str]:
    """Provide focused helper behavior for the split module."""
    lines = [
        "",
        f"## {principle.order:02d}. {principle.title}",
        "",
        principle.summary,
        "",
    ]

    if principle.directive:
        lines.extend(["### Directive", principle.directive, ""])
    if principle.quick_ref:
        lines.extend(
            ["### Quick Ref", *[f"- {item}" for item in principle.quick_ref], ""]
        )

    lines.extend(principle_axiom_lines(principle))

    if principle.tags:
        lines.extend(["### Tags", f"- {', '.join(principle.tags)}", ""])

    for section in principle.sections:
        lines.extend([f"### {section.title}", section.body, ""])

    return lines


def render_ethos_md(bundle: EthosBundle, repo_root: Path) -> str:
    """Render the full human-readable `ETHOS.md` document."""
    lines = [
        GENERATED_NOTICE,
        f"# {ethos_title(bundle)}",
        "",
    ]

    if bundle.overview:
        lines.extend([bundle.overview, ""])

    lines.extend(ethos_repo_context_lines(bundle, repo_root))

    lines.extend(["## Principles"])
    for principle in bundle.principles:
        lines.extend(ethos_principle_lines(principle))

    return join_lines(with_markdown_spdx(lines))
