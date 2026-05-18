# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Render per-principle detail Markdown documents.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.models import EthosBundle, Principle
from coding_ethos.renderer_common import (
    GENERATED_NOTICE,
    append_repo_context,
    join_lines,
    with_markdown_spdx,
)


def render_principle_detail(
    bundle: EthosBundle, principle: Principle, repo_root: Path
) -> str:
    """Render one per-principle detail document under `.agents/ethos/`."""
    lines = [
        GENERATED_NOTICE,
        f"# {principle.order:02d}. {principle.title}",
        "",
        "## Summary",
        principle.summary,
        "",
    ]

    if principle.directive:
        lines.extend(["## Directive", principle.directive, ""])

    if principle.quick_ref:
        lines.extend(
            ["## Quick Ref", *[f"- {item}" for item in principle.quick_ref], ""]
        )

    if principle.axioms:
        lines.extend(
            [
                "## Axioms",
                *[
                    f"- {axiom.axiom}" + (f" {axiom.action}" if axiom.action else "")
                    for axiom in principle.axioms
                ],
                "",
            ]
        )

    if principle.merge_topics:
        lines.extend(
            ["## Merge Topics", *[f"- {item}" for item in principle.merge_topics], ""]
        )

    if principle.tags:
        lines.extend(["## Tags", f"- {', '.join(principle.tags)}", ""])

    if principle.related:
        lines.extend(
            [
                "## Related",
                *[f"- [{related}]({related}.md)" for related in principle.related],
                "",
            ]
        )

    if principle.agent_hints:
        lines.extend(
            [
                "## Agent Hints",
                *[
                    f"- `{agent}`: {hint}"
                    for agent, hint in sorted(principle.agent_hints.items())
                ],
                "",
            ]
        )

    append_repo_context(lines, bundle, repo_root)

    for section in principle.sections:
        lines.extend([f"## {section.title}", section.body, ""])

    return join_lines(with_markdown_spdx(lines))
