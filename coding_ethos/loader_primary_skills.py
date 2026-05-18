# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Skill collection loading and validation for primary ethos payloads.

Responsibility is narrow.
Public imports stay aligned.
"""

from typing import Any, cast

from coding_ethos.loader_common import as_sequence, error
from coding_ethos.loader_components import skill_from_item
from coding_ethos.models import EthosSkill, Principle


def skills_from_payload(payload: dict[str, Any], *, source: str) -> list[EthosSkill]:
    """Provide focused helper behavior for the split module."""
    raw_skills = payload.get("skills")
    if raw_skills is None:
        return []

    skill_items = as_sequence(raw_skills, "`skills`")
    if not skill_items:
        return []

    skills: list[EthosSkill] = []
    for index, item in enumerate(skill_items, start=1):
        if not isinstance(item, dict):
            error(source, f"skills[{index}] must be a mapping.")
        skills.append(
            skill_from_item(
                cast(dict[str, Any], item),
                source=f"{source} skills[{index}]",
            )
        )
    return skills


def validate_skill_collection(
    payload: dict[str, Any], principles: list[Principle], source: str
) -> None:
    """Provide focused helper behavior for the split module."""
    raw_skills = payload.get("skills")
    if raw_skills is None:
        return

    skill_items = as_sequence(raw_skills, "`skills`")
    if not skill_items:
        return

    principle_ids = {principle.id for principle in principles}
    seen_skill_ids: set[str] = set()
    for index, item in enumerate(skill_items, start=1):
        if not isinstance(item, dict):
            error(source, f"skills[{index}] must be a mapping.")
        skill = skill_from_item(
            cast(dict[str, Any], item),
            source=f"{source} skills[{index}]",
        )
        if skill.id in seen_skill_ids:
            error(source, f"duplicate skill id `{skill.id}`.")
        seen_skill_ids.add(skill.id)
        unknown_principles = sorted(
            principle_id
            for principle_id in skill.principle_ids
            if principle_id not in principle_ids
        )
        if unknown_principles:
            error(
                source,
                (
                    f"skill `{skill.id}` references unknown principle ids: "
                    f"{', '.join(unknown_principles)}."
                ),
            )
