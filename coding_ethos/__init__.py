# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Generate agent instruction files from a shared coding ethos."""

from coding_ethos.cli import main
from coding_ethos.loaders import load_primary_bundle
from coding_ethos.markdown_seed import parse_ethos_markdown, seed_primary_from_markdown

__all__ = [
    "__version__",
    "load_primary_bundle",
    "main",
    "parse_ethos_markdown",
    "seed_primary_from_markdown",
]

__version__ = "0.1.0"
