# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Principle override application helpers for repo overlays.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path
from typing import Any

from coding_ethos.loader_overlay_fields import apply_override_fields
from coding_ethos.models import Principle, PrincipleSection
from coding_ethos.presets import build_agent_hints, build_merge_topics, build_quick_ref


def apply_principle_override(
    principle: Principle,
    *,
    override: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    """Provide focused helper behavior for the split module."""
    (
        explicit_agent_hints,
        recalc_quick_ref,
        recalc_merge_topics,
        recalc_agent_hints,
    ) = apply_override_fields(principle, override, repo_ethos_path)
    recalc_quick_ref = apply_override_sections(
        principle,
        override,
        recalc_quick_ref=recalc_quick_ref,
    )
    principle.body = "\n\n".join(
        section.body for section in principle.sections
    ).rstrip()
    finalize_override(
        principle,
        override,
        explicit_agent_hints=explicit_agent_hints,
        recalc_quick_ref=recalc_quick_ref,
        recalc_merge_topics=recalc_merge_topics,
        recalc_agent_hints=recalc_agent_hints,
    )


def apply_override_sections(
    principle: Principle,
    override: dict[str, Any],
    *,
    recalc_quick_ref: bool,
) -> bool:
    """Provide focused helper behavior for the split module."""
    prepend = str(override.get("prepend", "")).strip()
    append = str(override.get("append", "")).strip()
    if prepend:
        principle.sections.insert(
            0,
            PrincipleSection(
                id="repo-preface",
                title="Repo Preface",
                summary=prepend.splitlines()[0].strip(),
                body=prepend,
                kind="repo_context",
            ),
        )
        recalc_quick_ref = True
    if append:
        principle.sections.append(
            PrincipleSection(
                id="repo-addendum",
                title="Repo Addendum",
                summary=append.splitlines()[0].strip(),
                body=append,
                kind="repo_context",
            ),
        )
        recalc_quick_ref = True
    return recalc_quick_ref


def finalize_override(
    principle: Principle,
    override: dict[str, Any],
    *,
    explicit_agent_hints: dict[str, str],
    recalc_quick_ref: bool,
    recalc_merge_topics: bool,
    recalc_agent_hints: bool,
) -> None:
    """Provide focused helper behavior for the split module."""
    if recalc_quick_ref and "quick_ref" not in override:
        principle.quick_ref = build_quick_ref(
            summary=principle.summary,
            directive=principle.directive,
            section_summaries=[section.summary for section in principle.sections],
        )
    if recalc_merge_topics and "merge_topics" not in override:
        principle.merge_topics = build_merge_topics(
            title=principle.title, tags=principle.tags
        )
    if recalc_agent_hints and "agent_hints" not in override:
        principle.agent_hints = build_agent_hints(tags=principle.tags)
    elif recalc_agent_hints and explicit_agent_hints:
        derived_hints = build_agent_hints(tags=principle.tags)
        derived_hints.update(explicit_agent_hints)
        principle.agent_hints = derived_hints
