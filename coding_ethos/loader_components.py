# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Compatibility imports for normalized ethos payload builders.

Responsibility is narrow.
Public imports stay aligned.
"""

from coding_ethos.loader_principle_fields import (
    normalize_string_list,
    require_principle_id,
    require_principle_order,
    require_principle_sections,
    require_principle_title,
    resolve_principle_directive,
)
from coding_ethos.loader_sections import axioms_from_payload, sections_from_payload
from coding_ethos.loader_skills import skill_from_item

__all__ = [
    "axioms_from_payload",
    "normalize_string_list",
    "require_principle_id",
    "require_principle_order",
    "require_principle_sections",
    "require_principle_title",
    "resolve_principle_directive",
    "sections_from_payload",
    "skill_from_item",
]
