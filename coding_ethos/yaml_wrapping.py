# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""String wrapping rules for deterministic YAML rendering.

Responsibility is narrow.
Public imports stay aligned.
"""

import re
import textwrap

from ruamel.yaml.scalarstring import FoldedScalarString, LiteralScalarString

_WRAP_WIDTH = 70
_YAML_WIDTH = 88
_LIST_PREFIX_RE = re.compile(r"^(\s*(?:[-*+]|\d+\.)\s+)(.*)$")


def normalize_yaml_string(value: str) -> str:
    """Provide focused helper behavior for the split module."""
    if "\n" in value:
        return LiteralScalarString(wrap_multiline_string(value))
    if should_fold_single_line_string(value):
        return FoldedScalarString(wrap_single_line_string(value))
    return value


def should_fold_single_line_string(value: str) -> bool:
    """Provide focused helper behavior for the split module."""
    stripped = value.strip()
    return (
        len(value) > _YAML_WIDTH
        and bool(stripped)
        and not stripped.startswith(("http://", "https://", "#", "```"))
        and any(character.isspace() for character in stripped)
    )


def wrap_single_line_string(value: str) -> str:
    """Provide focused helper behavior for the split module."""
    return textwrap.fill(
        value.strip(),
        width=_WRAP_WIDTH,
        break_long_words=False,
        break_on_hyphens=False,
    )


def wrap_text_line(line: str) -> str:
    """Provide focused helper behavior for the split module."""
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


def wrap_multiline_string(value: str) -> str:
    """Provide focused helper behavior for the split module."""
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
        wrapped_lines.append(wrap_text_line(line))
    return "\n".join(wrapped_lines)
