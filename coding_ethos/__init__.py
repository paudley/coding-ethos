# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Public package API for coding-ethos generation and enforcement helpers.

This package exposes the supported CLI entrypoint and the small set of helper
functions that other modules and tests are expected to import directly.

See Also:
    CODING_ETHOS.md: Package-level workflow and source-of-truth guidance.

"""

from coding_ethos.cli import main
from coding_ethos.gemini_prompt_pack import (
    check_gemini_prompt_pack,
    render_gemini_prompt_pack,
    sync_gemini_prompt_pack,
)
from coding_ethos.loaders import load_primary_bundle
from coding_ethos.markdown_seed import parse_ethos_markdown, seed_primary_from_markdown
from coding_ethos.yaml_utils import format_yaml_file, render_yaml

__all__ = [
    "__version__",
    "check_gemini_prompt_pack",
    "format_yaml_file",
    "load_primary_bundle",
    "main",
    "parse_ethos_markdown",
    "render_gemini_prompt_pack",
    "render_yaml",
    "seed_primary_from_markdown",
    "sync_gemini_prompt_pack",
]

__version__ = "0.1.0"
