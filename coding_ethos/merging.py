# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Public merge API for generated root-file preservation workflows.

Responsibility is narrow.
Public imports stay aligned.
"""

from coding_ethos.merge_blocks import inject_addendum_block, inject_import_block
from coding_ethos.merge_engines import (
    MERGEABLE_FILES,
    SUPPORTED_MERGE_ENGINES,
    SUPPORTED_MERGE_STRATEGIES,
    MergeCommandFailedError,
    MergeRequest,
    MergeTimeoutError,
    MissingMergedOutputError,
    UnsupportedMergeEngineError,
    build_merge_prompt,
    merge_with_engine,
    resolve_codex_bin,
    resolve_merge_bin,
)
from coding_ethos.merge_strip import strip_managed_blocks

__all__ = [
    "SUPPORTED_MERGE_ENGINES",
    "SUPPORTED_MERGE_STRATEGIES",
    "MergeCommandFailedError",
    "MergeRequest",
    "MergeTimeoutError",
    "MissingMergedOutputError",
    "UnsupportedMergeEngineError",
    "build_merge_prompt",
    "inject_addendum_block",
    "inject_import_block",
    "merge_with_codex",
    "merge_with_engine",
    "resolve_codex_bin",
    "resolve_merge_bin",
    "should_merge_existing",
    "strip_managed_blocks",
]


def should_merge_existing(relative_path: str) -> bool:
    """Return whether a generated file supports merge-preserving writes."""
    return relative_path in MERGEABLE_FILES


def merge_with_codex(*, codex_bin: str, request: MergeRequest) -> str:
    """Merge one root file through the Codex CLI."""
    return merge_with_engine(engine="codex", binary=codex_bin, request=request)
