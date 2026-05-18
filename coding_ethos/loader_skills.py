# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Skill entry builders for ethos loader payloads.

Responsibility is narrow.
Public imports stay aligned.
"""

from typing import Any

from coding_ethos.loader_common import error, normalize_lines
from coding_ethos.loader_principle_fields import normalize_string_list
from coding_ethos.models import EthosSkill


def normalize_skill_id(raw: object, *, source: str, field_name: str) -> str:
    """Provide focused helper behavior for the split module."""
    value = str(raw or "").strip()
    if not value:
        error(source, f"skill `{field_name}` must be non-empty.")
    if value.startswith("-") or value.endswith("-") or "--" in value:
        error(source, f"skill `{field_name}` must be a valid skill slug.")
    if any(not (char.islower() or char.isdigit() or char == "-") for char in value):
        error(
            source,
            (
                f"skill `{field_name}` may only contain lowercase letters, "
                "digits, and hyphens."
            ),
        )
    return value


def skill_from_item(item: dict[str, Any], *, source: str) -> EthosSkill:
    """Provide focused helper behavior for the split module."""
    skill_id = normalize_skill_id(item.get("id"), source=source, field_name="id")
    title = str(item.get("title", "")).strip()
    if not title:
        error(source, f"skill `{skill_id}` must define a non-empty `title`.")
    description = str(item.get("description", "")).strip()
    if not description:
        error(source, f"skill `{skill_id}` must define a non-empty `description`.")
    principle_ids = normalize_string_list(
        item.get("principle_ids"),
        source=source,
        field_name="principle_ids",
    )
    if not principle_ids:
        error(source, f"skill `{skill_id}` must reference at least one principle.")
    return EthosSkill(
        id=skill_id,
        title=title,
        description=description,
        principle_ids=principle_ids,
        trigger_terms=normalize_lines(item.get("trigger_terms")),
        short_hint=str(item.get("short_hint", "")).strip(),
        focus=str(item.get("focus", "")).strip(),
        remediation_steps=normalize_lines(item.get("remediation_steps")),
    )
