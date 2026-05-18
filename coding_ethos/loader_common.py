# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Low-level YAML normalization helpers for ethos loaders.

Responsibility is narrow.
Public imports stay aligned.
"""

from collections.abc import Sequence
from pathlib import Path
from typing import Any, NoReturn, cast

import yaml

ETHOS_SCHEMA_VERSION = 2


def load_yaml(path: Path) -> dict[str, Any]:
    """Provide focused helper behavior for the split module."""
    payload = cast(object, yaml.safe_load(path.read_text(encoding="utf-8")))
    if payload is None:
        return {}
    if not isinstance(payload, dict):
        msg = f"Invalid ethos YAML at {path}: expected a mapping at the document root."
        raise TypeError(msg)
    return cast(dict[str, Any], payload)


def as_mapping(value: object, label: str) -> dict[str, Any]:
    """Provide focused helper behavior for the split module."""
    if not isinstance(value, dict):
        msg = f"{label} must be a mapping."
        raise TypeError(msg)
    return cast(dict[str, Any], value)


def empty_mapping() -> dict[str, Any]:
    """Provide focused helper behavior for the split module."""
    return {}


def as_sequence(value: object, label: str) -> Sequence[object]:
    """Provide focused helper behavior for the split module."""
    if not isinstance(value, list):
        msg = f"{label} must be a list."
        raise TypeError(msg)
    return cast(Sequence[object], value)


def error(source: str, message: str) -> NoReturn:
    """Provide focused helper behavior for the split module."""
    msg = f"Invalid ethos YAML at {source}: {message}"
    raise ValueError(msg)


def normalize_lines(value: object) -> list[str]:
    """Provide focused helper behavior for the split module."""
    if value is None:
        return []
    if isinstance(value, list):
        return [
            str(item).strip()
            for item in cast(Sequence[object], value)
            if str(item).strip()
        ]
    stripped = str(value).strip()
    return [stripped] if stripped else []


def normalize_commands(raw: object) -> dict[str, list[str]]:
    """Provide focused helper behavior for the split module."""
    if not raw:
        return {}
    if not isinstance(raw, dict):
        msg = "commands must be a mapping."
        raise TypeError(msg)
    normalized: dict[str, list[str]] = {}
    for name, commands in as_mapping(cast(object, raw), "commands").items():
        normalized[str(name)] = normalize_lines(commands)
    return normalized
