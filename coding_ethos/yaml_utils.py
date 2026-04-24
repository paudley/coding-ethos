"""Shared YAML formatting helpers for repo-owned configuration artifacts.

This module centralizes deterministic YAML rendering so generated config files
and repo-maintained YAML sources follow the same indentation and wrapping
rules. It also preserves comments when reformatting existing YAML files.
"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

import re
import textwrap
from io import StringIO
from pathlib import Path

from ruamel.yaml import YAML
from ruamel.yaml.comments import CommentedBase, CommentedMap, CommentedSeq
from ruamel.yaml.scalarstring import FoldedScalarString, LiteralScalarString

_WRAP_WIDTH = 70
_YAML_WIDTH = 88
_LIST_PREFIX_RE = re.compile(r"^(\s*(?:[-*+]|\d+\.)\s+)(.*)$")


def _build_yaml() -> YAML:
    yaml = YAML(typ="rt")
    yaml.width = _YAML_WIDTH
    yaml.preserve_quotes = True
    yaml.indent(mapping=2, sequence=4, offset=2)
    return yaml


def _normalize_yaml_strings(value: object) -> object:
    normalized = value
    if isinstance(value, CommentedMap):
        for key in list(value):
            value[key] = _normalize_yaml_strings(value[key])
        normalized = value
    elif isinstance(value, CommentedSeq):
        for index, item in enumerate(list(value)):
            value[index] = _normalize_yaml_strings(item)
        normalized = value
    elif isinstance(value, dict):
        normalized = {key: _normalize_yaml_strings(item) for key, item in value.items()}
    elif isinstance(value, list):
        normalized = [_normalize_yaml_strings(item) for item in value]
    elif isinstance(value, tuple):
        normalized = tuple(_normalize_yaml_strings(item) for item in value)
    elif isinstance(value, str):
        normalized = _normalize_yaml_string(value)
    return normalized


def _normalize_yaml_string(value: str) -> str:
    if "\n" in value:
        return LiteralScalarString(_wrap_multiline_string(value))
    if _should_fold_single_line_string(value):
        return FoldedScalarString(_wrap_single_line_string(value))
    return value


def _should_fold_single_line_string(value: str) -> bool:
    stripped = value.strip()
    return (
        len(value) > _YAML_WIDTH
        and bool(stripped)
        and not stripped.startswith(("http://", "https://", "#", "```"))
        and any(character.isspace() for character in stripped)
    )


def _wrap_single_line_string(value: str) -> str:
    return textwrap.fill(
        value.strip(),
        width=_WRAP_WIDTH,
        break_long_words=False,
        break_on_hyphens=False,
    )


def _wrap_text_line(line: str) -> str:
    stripped = line.strip()
    if not stripped:
        return ""

    list_match = _LIST_PREFIX_RE.match(line)
    if list_match:
        prefix, remainder = list_match.groups()
        return textwrap.fill(
            remainder,
            width=_WRAP_WIDTH,
            initial_indent=prefix,
            subsequent_indent=" " * len(prefix),
            break_long_words=False,
            break_on_hyphens=False,
        )

    leading = len(line) - len(line.lstrip())
    indent = " " * leading
    return textwrap.fill(
        stripped,
        width=_WRAP_WIDTH,
        initial_indent=indent,
        subsequent_indent=indent,
        break_long_words=False,
        break_on_hyphens=False,
    )


def _wrap_multiline_string(value: str) -> str:
    wrapped_lines: list[str] = []
    in_fenced_block = False
    for line in value.splitlines():
        stripped = line.strip()
        if stripped.startswith("```"):
            in_fenced_block = not in_fenced_block
            wrapped_lines.append(line)
            continue
        if (
            in_fenced_block
            or not stripped
            or stripped.startswith("#")
            or line.startswith("    ")
        ):
            wrapped_lines.append(line)
            continue
        wrapped_lines.append(_wrap_text_line(line))
    return "\n".join(wrapped_lines)


def render_yaml(data: object) -> str:
    """Render YAML with repo-standard indentation and wrapping."""
    yaml = _build_yaml()
    stream = StringIO()
    yaml.dump(_normalize_yaml_strings(data), stream)
    return stream.getvalue()


def format_yaml_file(path: Path) -> Path:
    """Reformat one existing YAML file in place while preserving comments."""
    yaml = _build_yaml()
    payload = yaml.load(path.read_text(encoding="utf-8"))
    if payload is None:
        msg = f"Cannot format empty YAML file: {path}"
        raise ValueError(msg)
    if not isinstance(payload, CommentedBase | dict | list):
        msg = f"Cannot format non-collection YAML payload in {path}"
        raise TypeError(msg)
    stream = StringIO()
    yaml.dump(_normalize_yaml_strings(payload), stream)
    path.write_text(stream.getvalue(), encoding="utf-8")
    return path
