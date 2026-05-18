# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Required principle field validators for ethos loader payloads.

Responsibility is narrow.
Public imports stay aligned.
"""

from typing import Any

from coding_ethos.loader_common import error, normalize_lines
from coding_ethos.loader_sections import sections_from_payload
from coding_ethos.models import PrincipleSection


def normalize_string_list(raw: object, *, source: str, field_name: str) -> list[str]:
    """Provide focused helper behavior for the split module."""
    values = normalize_lines(raw)
    if raw is not None and not values:
        error(
            source,
            f"`{field_name}` must contain at least one non-empty string when provided.",
        )
    return values


def require_principle_id(item: dict[str, Any], *, source: str) -> str:
    """Provide focused helper behavior for the split module."""
    principle_id = str(item.get("id", "")).strip()
    if not principle_id:
        error(source, "each principle must define a non-empty `id`.")
    return principle_id


def require_principle_title(
    item: dict[str, Any], *, source: str, principle_id: str
) -> str:
    """Provide focused helper behavior for the split module."""
    title = str(item.get("title", "")).strip()
    if not title:
        error(source, f"principle `{principle_id}` must define a non-empty `title`.")
    return title


def require_principle_order(
    item: dict[str, Any], *, source: str, principle_id: str
) -> int:
    """Provide focused helper behavior for the split module."""
    try:
        return int(item["order"])
    except (KeyError, TypeError, ValueError):
        error(source, f"principle `{principle_id}` must define an integer `order`.")


def require_principle_sections(
    item: dict[str, Any], *, source: str, principle_id: str
) -> list[PrincipleSection]:
    """Provide focused helper behavior for the split module."""
    sections = sections_from_payload(item, source=source)
    if not sections:
        error(
            source,
            (
                f"principle `{principle_id}` must include at least one "
                "section or inline `body`."
            ),
        )
    return sections


def resolve_principle_directive(
    item: dict[str, Any],
    *,
    source: str,
    principle_id: str,
    summary: str,
) -> str:
    """Provide focused helper behavior for the split module."""
    directive = str(item.get("directive", summary)).strip()
    if not directive:
        error(
            source, f"principle `{principle_id}` must define a non-empty `directive`."
        )
    return directive
