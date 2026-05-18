# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Scalar/list field updates for repo principle overrides.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path
from typing import Any

from coding_ethos.loader_agents import normalize_agent_hints
from coding_ethos.loader_common import as_sequence
from coding_ethos.loader_components import normalize_string_list
from coding_ethos.models import Principle


def apply_override_fields(
    principle: Principle,
    override: dict[str, Any],
    repo_ethos_path: Path,
) -> tuple[dict[str, str], bool, bool, bool]:
    """Provide focused helper behavior for the split module."""
    explicit_agent_hints: dict[str, str] = {}
    recalc_quick_ref = False
    recalc_merge_topics = False
    recalc_agent_hints = False
    if "summary" in override:
        principle.summary = str(override["summary"]).strip()
        recalc_quick_ref = True
    if "directive" in override:
        principle.directive = str(override["directive"]).strip()
        recalc_quick_ref = True
    if "tags" in override:
        raw_tags = as_sequence(override["tags"], "override.tags")
        principle.tags = [str(tag).strip() for tag in raw_tags if str(tag).strip()]
        recalc_merge_topics = True
        recalc_agent_hints = True
    if "related" in override:
        raw_related = as_sequence(override["related"], "override.related")
        principle.related = [
            str(item).strip() for item in raw_related if str(item).strip()
        ]
    if "quick_ref" in override:
        principle.quick_ref = normalize_string_list(
            override["quick_ref"],
            source=f"{repo_ethos_path} override `{principle.id}`",
            field_name="quick_ref",
        )
    if "merge_topics" in override:
        principle.merge_topics = normalize_string_list(
            override["merge_topics"],
            source=f"{repo_ethos_path} override `{principle.id}`",
            field_name="merge_topics",
        )
    if "agent_hints" in override:
        explicit_agent_hints = normalize_agent_hints(override["agent_hints"])
        recalc_agent_hints = True
    return (
        explicit_agent_hints,
        recalc_quick_ref,
        recalc_merge_topics,
        recalc_agent_hints,
    )
