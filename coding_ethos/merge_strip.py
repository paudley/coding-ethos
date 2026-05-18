# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed Markdown block stripping helpers.

Responsibility is narrow.
Public imports stay aligned.
"""

import re


def strip_managed_blocks(content: str) -> str:
    """Remove managed coding-ethos blocks from a root file."""
    stripped = content
    pattern = re.compile(
        r"<!-- coding-ethos:begin .*?<!-- coding-ethos:end .*?-->", re.DOTALL
    )
    while True:
        updated = pattern.sub("", stripped)
        if updated == stripped:
            break
        stripped = updated
    stripped = re.sub(r"\n{3,}", "\n\n", stripped).strip("\n")
    return stripped + "\n" if stripped else ""
