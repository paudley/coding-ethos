# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Principle section and axiom builders for ethos loader payloads.

Responsibility is narrow.
Public imports stay aligned.
"""

from typing import Any, cast

from coding_ethos.loader_common import as_sequence, error
from coding_ethos.models import SECTION_KINDS, PrincipleAxiom, PrincipleSection


def body_from_item(item: dict[str, Any]) -> str:
    """Provide focused helper behavior for the split module."""
    return str(item.get("body", "")).rstrip()


def normalize_section_kind(raw_kind: object) -> str:
    """Provide focused helper behavior for the split module."""
    kind = str(raw_kind or "guidance").strip()
    if kind not in SECTION_KINDS:
        msg = f"section kind must be one of: {', '.join(SECTION_KINDS)}"
        raise ValueError(msg)
    return kind


def section_from_raw(
    raw_section: object,
    *,
    source: str,
    seen_section_ids: set[str],
) -> PrincipleSection:
    """Provide focused helper behavior for the split module."""
    if not isinstance(raw_section, dict):
        error(source, "each section must be a mapping.")
    section = cast(dict[str, Any], raw_section)
    body = body_from_item(section)
    section_id = str(section.get("id", "")).strip()
    if not section_id:
        error(source, "each section must define a non-empty `id`.")
    if section_id in seen_section_ids:
        error(source, f"duplicate section id `{section_id}`.")
    seen_section_ids.add(section_id)
    title = str(section.get("title", "")).strip()
    if not title:
        error(source, f"section `{section_id}` must define a non-empty `title`.")
    if not body:
        error(source, f"section `{section_id}` must define a non-empty `body`.")
    try:
        section_kind = normalize_section_kind(section.get("kind"))
    except ValueError as exc:
        error(source, f"section `{section_id}` {exc}")
    return PrincipleSection(
        id=section_id,
        title=title,
        summary=str(section.get("summary", "")).strip() or body.splitlines()[0].strip(),
        body=body,
        kind=section_kind,
    )


def sections_from_payload(
    item: dict[str, Any], *, source: str
) -> list[PrincipleSection]:
    """Provide focused helper behavior for the split module."""
    raw_sections = item.get("sections", [])
    sections: list[PrincipleSection] = []
    if not raw_sections:
        body = body_from_item(item)
        if body:
            sections.append(
                PrincipleSection(
                    id="overview",
                    title="Overview",
                    summary=str(item.get("summary", "")).strip()
                    or body.splitlines()[0].strip(),
                    body=body,
                    kind="overview",
                )
            )
        return sections

    if not isinstance(raw_sections, list):
        error(source, "`sections` must be a list.")

    seen_section_ids: set[str] = set()
    sections.extend(
        section_from_raw(
            raw_section,
            source=source,
            seen_section_ids=seen_section_ids,
        )
        for raw_section in as_sequence(cast(object, raw_sections), "`sections`")
    )
    return sections


def axioms_from_payload(item: dict[str, Any], *, source: str) -> list[PrincipleAxiom]:
    """Provide focused helper behavior for the split module."""
    raw_axioms = item.get("axioms", [])
    if not raw_axioms:
        return []
    if not isinstance(raw_axioms, list):
        error(source, "`axioms` must be a list.")

    axioms: list[PrincipleAxiom] = []
    for raw_axiom in as_sequence(cast(object, raw_axioms), "`axioms`"):
        if not isinstance(raw_axiom, dict):
            error(source, "each axiom must be a mapping.")
        axiom = cast(dict[str, Any], raw_axiom)
        text = str(axiom.get("axiom", "")).strip()
        if not text:
            error(source, "each axiom must define a non-empty `axiom`.")
        axioms.append(
            PrincipleAxiom(
                axiom=text,
                action=str(axiom.get("action", "")).strip(),
            )
        )

    return axioms
