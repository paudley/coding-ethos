# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Repo overlay merge support for ethos bundles.

Responsibility is narrow.
Public imports stay aligned.
"""

from collections.abc import Sequence
from copy import deepcopy
from pathlib import Path
from typing import Any, cast

from coding_ethos.bundle_validator import validate_bundle
from coding_ethos.loader_common import load_yaml
from coding_ethos.loader_overlay_apply import apply_principle_override
from coding_ethos.loader_overlay_context import (
    load_repo_context,
    overlay_error,
    overlay_overrides,
    overlay_principle_section,
)
from coding_ethos.loader_primary import principle_from_item
from coding_ethos.models import EthosBundle


def apply_overrides(
    merged: EthosBundle,
    overrides: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    """Provide focused helper behavior for the split module."""
    principles_by_id = {principle.id: principle for principle in merged.principles}
    for principle_id, override in overrides.items():
        principle = principles_by_id.get(str(principle_id))
        if principle is None:
            continue
        if not isinstance(override, dict):
            overlay_error(
                repo_ethos_path,
                f"override `{principle_id}` must be a mapping.",
            )
        apply_principle_override(
            principle,
            override=cast(dict[str, Any], override),
            repo_ethos_path=repo_ethos_path,
        )


def append_additional_principles(
    merged: EthosBundle,
    principle_section: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    """Provide focused helper behavior for the split module."""
    principles_by_id = {principle.id for principle in merged.principles}
    additional_ids: set[str] = set()
    additional = cast(object, principle_section.get("additional", []) or [])
    if additional and not isinstance(additional, list):
        overlay_error(repo_ethos_path, "`principles.additional` must be a list.")
    for item in cast(Sequence[object], additional):
        if not isinstance(item, dict):
            overlay_error(
                repo_ethos_path,
                "each additional principle must be a mapping.",
            )
        principle = principle_from_item(
            cast(dict[str, Any], item),
            source=f"{repo_ethos_path} additional[{len(additional_ids) + 1}]",
        )
        if principle.id in principles_by_id or principle.id in additional_ids:
            overlay_error(
                repo_ethos_path,
                f"duplicate additional principle id `{principle.id}`.",
            )
        additional_ids.add(principle.id)
        merged.principles.append(principle)


def merge_repo_ethos(bundle: EthosBundle, repo_ethos_path: Path) -> EthosBundle:
    """Apply a repo-specific overlay on top of a shared ethos bundle.

    Args:
        bundle: The already validated shared ethos bundle.
        repo_ethos_path: Optional path to the repo-local overlay YAML.

    Returns:
        A new bundle containing repo context, agent notes, and principle
        overrides from the overlay when one is present.

    Raises:
        ValueError: The overlay payload is structurally invalid or references
            unknown principles.

    """
    if not repo_ethos_path.exists():
        return bundle

    merged = deepcopy(bundle)
    payload = load_yaml(repo_ethos_path)
    merged.repo = load_repo_context(payload, repo_ethos_path)
    principles_by_id = {principle.id: principle for principle in merged.principles}
    principle_section = overlay_principle_section(payload, repo_ethos_path)
    overrides = overlay_overrides(
        principle_section,
        principles_by_id,
        repo_ethos_path,
    )
    apply_overrides(merged, overrides, repo_ethos_path)
    append_additional_principles(merged, principle_section, repo_ethos_path)

    merged.principles.sort(
        key=lambda principle: (principle.order, principle.title.lower())
    )
    validate_bundle(merged, source=str(repo_ethos_path))
    return merged
