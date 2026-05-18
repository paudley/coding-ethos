# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Validate normalized ethos bundles after loading and repo overlays.

This module owns invariants that must hold after primary YAML loading and
after repo overlays mutate a copied bundle. Keeping these checks outside the
loader makes post-overlay validation reusable without growing the loader's
schema parsing responsibilities. Callers should pass a human-readable source
path so validation failures point at the file or overlay that introduced the
invalid state.
"""

from typing import NoReturn

from coding_ethos.models import EthosBundle, Principle, PrincipleSection


def validate_bundle(bundle: EthosBundle, *, source: str) -> None:
    """Validate normalized bundle invariants.

    Args:
        bundle: Normalized bundle produced by the primary loader and optional
            repo overlay.
        source: Human-readable source path used in validation errors.

    Raises:
        ValueError: The bundle violates required model invariants.

    """
    validate_principles(bundle.principles, source=source)
    validate_skills(bundle, source=source)


def validate_principle_collection(principles: list[Principle], *, source: str) -> None:
    """Validate principle invariants without requiring full bundle context."""
    validate_principles(principles, source=source)


def validate_principles(principles: list[Principle], *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    if not principles:
        error(source, "bundle must contain at least one principle.")

    seen_ids: set[str] = set()
    seen_orders: set[int] = set()
    related_map: dict[str, list[str]] = {}
    for principle in principles:
        validate_principle(principle, source=source)
        if principle.id in seen_ids:
            error(source, f"duplicate principle id `{principle.id}`.")
        if principle.order in seen_orders:
            error(source, f"duplicate principle order `{principle.order}`.")
        seen_ids.add(principle.id)
        seen_orders.add(principle.order)
        related_map[principle.id] = principle.related

    all_ids = set(related_map)
    for principle_id, related in related_map.items():
        unknown_related = sorted(item for item in related if item not in all_ids)
        if unknown_related:
            error(
                source,
                (
                    f"principle `{principle_id}` references unknown related "
                    f"ids: {', '.join(unknown_related)}."
                ),
            )


def validate_principle(principle: Principle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    validate_principle_identity(principle, source=source)
    validate_principle_text(principle, source=source)
    validate_principle_sections(principle, source=source)
    validate_principle_guidance(principle, source=source)


def validate_principle_identity(principle: Principle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    if not principle.id.strip():
        error(source, "each principle must define a non-empty `id`.")
    if principle.order < 1:
        error(source, f"principle `{principle.id}` must define a positive `order`.")
    if not principle.title.strip():
        error(source, f"principle `{principle.id}` must define a non-empty `title`.")


def validate_principle_text(principle: Principle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    if not principle.summary.strip():
        error(source, f"principle `{principle.id}` must define a non-empty `summary`.")
    if not principle.directive.strip():
        error(
            source,
            f"principle `{principle.id}` must define a non-empty `directive`.",
        )
    if not principle.body.strip():
        error(source, f"principle `{principle.id}` must define non-empty body text.")


def validate_principle_sections(principle: Principle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    if not principle.sections:
        error(source, f"principle `{principle.id}` must include at least one section.")
    for section in principle.sections:
        validate_section(section, principle_id=principle.id, source=source)


def validate_principle_guidance(principle: Principle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    if not principle.quick_ref:
        error(source, f"principle `{principle.id}` must define quick_ref guidance.")
    if not principle.merge_topics:
        error(source, f"principle `{principle.id}` must define merge topics.")


def validate_section(
    section: PrincipleSection, *, principle_id: str, source: str
) -> None:
    """Provide focused helper behavior for the module."""
    if not section.id.strip():
        error(source, f"principle `{principle_id}` has a section without an id.")
    if not section.title.strip():
        error(source, f"principle `{principle_id}` has a section without a title.")
    if not section.summary.strip():
        error(source, f"principle `{principle_id}` has a section without a summary.")
    if not section.body.strip():
        error(source, f"principle `{principle_id}` has a section without body text.")


def validate_skills(bundle: EthosBundle, *, source: str) -> None:
    """Provide focused helper behavior for the module."""
    principle_ids = {principle.id for principle in bundle.principles}
    seen_skill_ids: set[str] = set()
    for skill in bundle.skills:
        if not skill.id.strip():
            error(source, "each skill must define a non-empty `id`.")
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


def error(source: str, message: str) -> NoReturn:
    """Provide focused helper behavior for the module."""
    msg = f"{source}: {message}"
    raise ValueError(msg)
