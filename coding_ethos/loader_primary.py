# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Primary coding ethos bundle loading and validation.

Responsibility is narrow.
Public imports stay aligned.
"""

from collections.abc import Sequence
from pathlib import Path
from typing import Any, cast

from coding_ethos.bundle_validator import (
    validate_principle_collection as validate_bundle_principle_collection,
)
from coding_ethos.loader_agents import normalize_agent_hints
from coding_ethos.loader_common import (
    ETHOS_SCHEMA_VERSION,
    as_mapping,
    as_sequence,
    error,
    load_yaml,
)
from coding_ethos.loader_components import (
    axioms_from_payload,
    normalize_string_list,
    require_principle_id,
    require_principle_order,
    require_principle_sections,
    require_principle_title,
    resolve_principle_directive,
)
from coding_ethos.loader_primary_profiles import agent_profiles_from_payload
from coding_ethos.loader_primary_skills import (
    skills_from_payload,
    validate_skill_collection,
)
from coding_ethos.models import (
    EthosBundle,
    Principle,
)
from coding_ethos.presets import build_agent_hints, build_merge_topics, build_quick_ref


def principle_from_item(item: dict[str, Any], *, source: str) -> Principle:
    """Provide focused helper behavior for the split module."""
    principle_id = require_principle_id(item, source=source)
    title = require_principle_title(item, source=source, principle_id=principle_id)
    order = require_principle_order(item, source=source, principle_id=principle_id)
    sections = require_principle_sections(
        item, source=source, principle_id=principle_id
    )

    body = "\n\n".join(section.body for section in sections).rstrip()
    summary = str(item.get("summary", "")).strip() or sections[0].summary
    directive = resolve_principle_directive(
        item,
        source=source,
        principle_id=principle_id,
        summary=summary,
    )

    tags = [str(tag).strip() for tag in item.get("tags", []) if str(tag).strip()]
    related = [
        str(related).strip()
        for related in item.get("related", [])
        if str(related).strip()
    ]
    quick_ref = normalize_string_list(
        item.get("quick_ref"), source=source, field_name="quick_ref"
    )
    if not quick_ref:
        quick_ref = build_quick_ref(
            summary=summary,
            directive=directive,
            section_summaries=[section.summary for section in sections],
        )

    merge_topics = normalize_string_list(
        item.get("merge_topics"), source=source, field_name="merge_topics"
    )
    if not merge_topics:
        merge_topics = build_merge_topics(title=title, tags=tags)

    agent_hints = normalize_agent_hints(item.get("agent_hints"))
    if not agent_hints:
        agent_hints = build_agent_hints(tags=tags)

    return Principle(
        id=principle_id,
        order=order,
        title=title,
        summary=summary,
        body=body,
        sections=sections,
        axioms=axioms_from_payload(item, source=source),
        directive=directive,
        quick_ref=quick_ref,
        merge_topics=merge_topics,
        tags=tags,
        related=related,
        agent_hints=agent_hints,
    )


def validate_primary_payload(payload: dict[str, Any], primary_path: Path) -> None:
    """Provide focused helper behavior for the split module."""
    source = str(primary_path)
    version = payload.get("version")
    if version != ETHOS_SCHEMA_VERSION:
        error(source, "`version` must be set to `2`.")

    principles = payload.get("principles")
    if not isinstance(principles, list) or not principles:
        error(source, "`principles` must be a non-empty list.")

    normalized_principles: list[Principle] = []
    for index, item in enumerate(cast(Sequence[object], principles), start=1):
        if not isinstance(item, dict):
            error(source, f"principles[{index}] must be a mapping.")
        normalized_principles.append(
            principle_from_item(
                cast(dict[str, Any], item),
                source=f"{source} principles[{index}]",
            )
        )
    validate_principle_ordering(normalized_principles, source)
    validate_skill_collection(payload, normalized_principles, source)


def validate_principle_ordering(principles: list[Principle], source: str) -> None:
    """Provide focused helper behavior for the split module."""
    seen_orders: dict[int, str] = {}
    for principle in principles:
        existing = seen_orders.get(principle.order)
        if existing is not None:
            error(
                source,
                (
                    f"duplicate principle order `{principle.order}` for "
                    f"`{existing}` and `{principle.id}`."
                ),
            )
        seen_orders[principle.order] = principle.id
    validate_bundle_principle_collection(principles, source=source)


def principles_from_payload(payload: dict[str, Any], *, source: str) -> list[Principle]:
    """Provide focused helper behavior for the split module."""
    principles: list[Principle] = []
    raw_principles = payload.get("principles", [])
    for index, item in enumerate(as_sequence(raw_principles, "`principles`"), start=1):
        if not isinstance(item, dict):
            error(source, f"principles[{index}] must be a mapping.")
        principles.append(
            principle_from_item(
                cast(dict[str, Any], item),
                source=f"{source} principles[{index}]",
            )
        )
    return sorted(
        principles, key=lambda principle: (principle.order, principle.title.lower())
    )


def load_primary_bundle(primary_path: Path) -> EthosBundle:
    """Load and validate the primary structured ethos definition.

    Args:
        primary_path: Path to the canonical ethos YAML file.

    Returns:
        A validated :class:`EthosBundle` ready for rendering or repo overlays.

    Raises:
        ValueError: The YAML document is malformed or violates the expected
            ethos schema.

    """
    payload = load_yaml(primary_path)
    validate_primary_payload(payload, primary_path)
    metadata = as_mapping(payload.get("metadata", {}) or {}, "metadata")
    return EthosBundle(
        title=str(metadata.get("title", "Coding Ethos")).strip(),
        overview=str(metadata.get("overview", "")).strip(),
        source_markdown=str(metadata.get("source_markdown", "")).strip(),
        principles=principles_from_payload(payload, source=str(primary_path)),
        agent_profiles=agent_profiles_from_payload(payload),
        skills=skills_from_payload(payload, source=str(primary_path)),
    )
