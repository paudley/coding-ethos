# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Argument parser construction for the coding-ethos CLI.

Responsibility is narrow.
Public imports stay aligned.
"""

import argparse
from pathlib import Path

from coding_ethos.merging import SUPPORTED_MERGE_ENGINES, SUPPORTED_MERGE_STRATEGIES


def build_parser() -> argparse.ArgumentParser:
    """Provide focused helper behavior for the split module."""
    parser = argparse.ArgumentParser(
        description=(
            "Generate Codex, Claude Code, and Gemini instruction files from a "
            "shared ethos."
        )
    )
    parser.add_argument(
        "--repo",
        type=Path,
        help="Repository directory where generated files should be written.",
    )
    parser.add_argument(
        "--primary",
        type=Path,
        help=(
            "Primary ethos YAML. Defaults to coding_ethos.yml, with .yaml "
            "aliases also supported."
        ),
    )
    parser.add_argument(
        "--repo-ethos",
        type=Path,
        help="Optional repo-specific ethos YAML. Defaults to <repo>/repo_ethos.yml.",
    )
    parser.add_argument(
        "--seed-from-markdown",
        type=Path,
        help=(
            "Seed or refresh the primary YAML from a markdown source before rendering."
        ),
    )
    parser.add_argument(
        "--merge-existing",
        action="store_true",
        help=(
            "Preserve existing AGENTS.md/CLAUDE.md/GEMINI.md and inject "
            "managed generated blocks instead of replacing them."
        ),
    )
    parser.add_argument(
        "--merge-strategy",
        choices=SUPPORTED_MERGE_STRATEGIES,
        default="inject",
        help=(
            "Merge strategy for existing root files. Defaults to inject; use "
            "llm only for full-document AI merges."
        ),
    )
    parser.add_argument(
        "--merge-engine",
        choices=SUPPORTED_MERGE_ENGINES,
        default="codex",
        help="LLM CLI to use when --merge-strategy=llm. Defaults to codex.",
    )
    parser.add_argument(
        "--merge-bin",
        help=(
            "Path to the selected merge engine CLI binary. Defaults to the "
            "engine name from PATH."
        ),
    )
    parser.add_argument(
        "--merge-model",
        help="Optional model override for merge mode.",
    )
    parser.add_argument(
        "--merge-timeout-seconds",
        type=int,
        default=300,
        help=(
            "Timeout for each root file when --merge-strategy=llm. Defaults "
            "to 300 seconds."
        ),
    )
    parser.add_argument(
        "--codex-bin",
        help="Deprecated alias for --merge-bin when --merge-engine=codex.",
    )
    parser.add_argument(
        "--codex-model",
        help="Deprecated alias for --merge-model when --merge-engine=codex.",
    )
    return parser
