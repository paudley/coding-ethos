# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Public package API for coding-ethos generation and enforcement helpers.

This package exposes the supported CLI entrypoint and the small set of helper
functions that other modules and tests are expected to import directly.

See Also:
    CODING_ETHOS.md: Package-level workflow and source-of-truth guidance.

"""

from coding_ethos.cli import main
from coding_ethos.loaders import load_primary_bundle, merge_repo_ethos
from coding_ethos.markdown_seed import parse_ethos_markdown, seed_primary_from_markdown
from coding_ethos.merging import (
    SUPPORTED_MERGE_ENGINES,
    UnsupportedMergeEngineError,
    resolve_merge_bin,
)
from coding_ethos.renderers import (
    render_agent_root_outputs,
    required_root_imports,
    root_merge_topics,
)
from coding_ethos.yaml_utils import format_yaml_file, render_yaml

__all__ = [
    "SUPPORTED_MERGE_ENGINES",
    "UnsupportedMergeEngineError",
    "__version__",
    "format_yaml_file",
    "load_primary_bundle",
    "main",
    "merge_repo_ethos",
    "parse_ethos_markdown",
    "render_agent_root_outputs",
    "render_yaml",
    "required_root_imports",
    "resolve_merge_bin",
    "root_merge_topics",
    "seed_primary_from_markdown",
]

__version__ = "0.2.1"
