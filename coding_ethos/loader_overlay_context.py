# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Repo overlay payload parsing helpers.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path
from typing import Any, cast

from coding_ethos.loader_agents import normalize_agent_notes
from coding_ethos.loader_common import (
    empty_mapping,
    error,
    normalize_commands,
    normalize_lines,
)
from coding_ethos.models import Principle, RepoContext


def overlay_error(repo_ethos_path: Path, message: str) -> None:
    """Provide focused helper behavior for the split module."""
    error(str(repo_ethos_path), message)


def load_repo_context(payload: dict[str, Any], repo_ethos_path: Path) -> RepoContext:
    """Provide focused helper behavior for the split module."""
    repo_payload = cast(object, payload.get("repo", {}))
    if repo_payload and not isinstance(repo_payload, dict):
        overlay_error(repo_ethos_path, "`repo` must be a mapping.")
    repo = cast(dict[str, Any], repo_payload) if repo_payload else empty_mapping()
    raw_paths = cast(object, repo.get("paths") or {})
    if raw_paths and not isinstance(raw_paths, dict):
        overlay_error(repo_ethos_path, "`repo.paths` must be a mapping.")
    paths = cast(dict[str, Any], raw_paths) if raw_paths else empty_mapping()
    return RepoContext(
        name=str(repo.get("name", "")).strip(),
        overview=str(repo.get("overview", "")).strip(),
        commands=normalize_commands(repo.get("commands")),
        paths={str(key): str(value) for key, value in paths.items()},
        notes=normalize_lines(repo.get("notes")),
        agent_notes=normalize_agent_notes(payload.get("agent_notes")),
    )


def overlay_principle_section(
    payload: dict[str, Any], repo_ethos_path: Path
) -> dict[str, Any]:
    """Provide focused helper behavior for the split module."""
    principle_section = payload.get("principles", {})
    if principle_section and not isinstance(principle_section, dict):
        overlay_error(repo_ethos_path, "`principles` must be a mapping.")
    return cast(dict[str, Any], principle_section)


def overlay_overrides(
    principle_section: dict[str, Any],
    principles_by_id: dict[str, Principle],
    repo_ethos_path: Path,
) -> dict[str, Any]:
    """Provide focused helper behavior for the split module."""
    overrides = cast(object, principle_section.get("overrides", {}) or {})
    if overrides and not isinstance(overrides, dict):
        overlay_error(repo_ethos_path, "`principles.overrides` must be a mapping.")
    override_map = cast(dict[str, Any], overrides) if overrides else empty_mapping()
    unknown_override_ids = sorted(
        principle_id
        for principle_id in override_map
        if str(principle_id) not in principles_by_id
    )
    if unknown_override_ids:
        unknown_ids = ", ".join(unknown_override_ids)
        overlay_error(
            repo_ethos_path,
            f"unknown override ids: {unknown_ids}.",
        )
    return override_map
