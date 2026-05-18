# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Shared YAML formatting helpers for repo-owned configuration artifacts.

This module centralizes deterministic YAML rendering so generated config files
and repo-maintained YAML sources follow the same indentation and wrapping
rules. It also preserves comments when reformatting existing YAML files.


Responsibility is narrow.
Public imports stay aligned.
"""

from io import StringIO
from pathlib import Path
from typing import Any, cast

from ruamel.yaml import YAML
from ruamel.yaml.comments import CommentedBase, CommentedMap, CommentedSeq

from coding_ethos.yaml_wrapping import normalize_yaml_string

_YAML_WIDTH = 88


def build_yaml() -> YAML:
    """Provide focused helper behavior for the split module."""
    yaml = YAML(typ="rt")
    yaml.width = _YAML_WIDTH
    yaml.preserve_quotes = True
    yaml.indent(mapping=2, sequence=4, offset=2)
    return yaml


def normalize_yaml_strings(value: object) -> object:
    """Provide focused helper behavior for the split module."""
    normalized = value
    if isinstance(value, CommentedMap):
        mapping = cast(Any, value)
        keys = cast(list[object], list(mapping))
        for key in keys:
            item = cast(object, mapping[key])
            mapping[key] = normalize_yaml_strings(item)
        normalized = value
    elif isinstance(value, CommentedSeq):
        sequence = cast(Any, value)
        items = cast(list[object], list(sequence))
        for index, item in enumerate(items):
            sequence[index] = normalize_yaml_strings(item)
        normalized = value
    elif isinstance(value, dict):
        mapping = cast(dict[object, object], value)
        normalized = {
            key: normalize_yaml_strings(item) for key, item in mapping.items()
        }
    elif isinstance(value, list):
        normalized = [
            normalize_yaml_strings(item) for item in cast(list[object], value)
        ]
    elif isinstance(value, tuple):
        normalized = tuple(
            normalize_yaml_strings(item) for item in cast(tuple[object, ...], value)
        )
    elif isinstance(value, str):
        normalized = normalize_yaml_string(value)
    return normalized


def render_yaml(data: object) -> str:
    """Render YAML with repo-standard indentation and wrapping."""
    yaml = build_yaml()
    stream = StringIO()
    yaml_rt = cast(Any, yaml)
    yaml_rt.dump(normalize_yaml_strings(data), stream)
    return stream.getvalue()


def format_yaml_file(path: Path) -> Path:
    """Reformat one existing YAML file in place while preserving comments."""
    yaml = build_yaml()
    yaml_rt = cast(Any, yaml)
    payload = cast(object, yaml_rt.load(path.read_text(encoding="utf-8")))
    if payload is None:
        msg = f"Cannot format empty YAML file: {path}"
        raise ValueError(msg)
    if not isinstance(payload, CommentedBase | dict | list):
        msg = f"Cannot format non-collection YAML payload in {path}"
        raise TypeError(msg)
    stream = StringIO()
    yaml_rt = cast(Any, yaml)
    yaml_rt.dump(normalize_yaml_strings(cast(object, payload)), stream)
    path.write_text(stream.getvalue(), encoding="utf-8")
    return path
