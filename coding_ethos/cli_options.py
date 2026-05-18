# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Argument parsing and path resolution for the coding-ethos CLI.

Responsibility is narrow.
Public imports stay aligned.
"""

import argparse
from dataclasses import dataclass
from pathlib import Path

from coding_ethos.cli_parser import build_parser
from coding_ethos.markdown_seed import seed_primary_from_markdown
from coding_ethos.resources import resource_path

__all__ = [
    "MergeSettings",
    "build_parser",
    "has_repo_root",
    "maybe_seed_primary",
    "repo_root_from_args",
    "require_primary_path",
    "resolve_merge_settings",
    "resolve_primary_path",
    "resolve_repo_ethos",
    "resolve_seed_primary_path",
]


@dataclass(frozen=True, slots=True)
class MergeSettings:
    """Resolved merge behavior for writing root agent files."""

    existing: bool
    strategy: str
    engine: str
    binary: str
    model: str
    timeout_seconds: int


def resolve_repo_ethos(repo_root: Path, explicit_repo_ethos: object = "") -> Path:
    """Provide focused helper behavior for the split module."""
    if isinstance(explicit_repo_ethos, Path):
        return explicit_repo_ethos.resolve()
    for name in ("repo_ethos.yml", "repo_ethos.yaml"):
        candidate = repo_root / name
        if candidate.exists():
            return candidate.resolve()
    return (repo_root / "repo_ethos.yml").resolve()


def resolve_primary_path(explicit_primary: object = "") -> Path:
    """Provide focused helper behavior for the split module."""
    if isinstance(explicit_primary, Path):
        return explicit_primary.resolve()
    for name in (
        "coding_ethos.yml",
        "coding_ethos.yaml",
        "code_ethos.yml",
        "code_ethos.yaml",
    ):
        candidate = Path(name)
        if candidate.exists():
            return candidate.resolve()
    return resource_path("coding_ethos.yml")


def resolve_seed_primary_path(explicit_primary: object = "") -> Path:
    """Provide focused helper behavior for the split module."""
    if isinstance(explicit_primary, Path):
        return explicit_primary.resolve()
    return Path("coding_ethos.yml").resolve()


def resolve_merge_settings(args: argparse.Namespace) -> MergeSettings:
    """Provide focused helper behavior for the split module."""
    merge_bin = args.merge_bin or ""
    merge_model = args.merge_model or ""
    if args.merge_engine == "codex":
        merge_bin = merge_bin or args.codex_bin or ""
        merge_model = merge_model or args.codex_model or ""
    return MergeSettings(
        existing=args.merge_existing,
        strategy=args.merge_strategy,
        engine=args.merge_engine,
        binary=merge_bin,
        model=merge_model,
        timeout_seconds=args.merge_timeout_seconds,
    )


def has_repo_root(args: argparse.Namespace) -> bool:
    """Provide focused helper behavior for the split module."""
    return isinstance(args.repo, Path)


def repo_root_from_args(args: argparse.Namespace) -> Path:
    """Provide focused helper behavior for the split module."""
    if isinstance(args.repo, Path):
        return args.repo.expanduser().resolve()
    return Path.cwd().resolve()


def require_primary_path(parser: argparse.ArgumentParser, primary_path: Path) -> None:
    """Provide focused helper behavior for the split module."""
    if primary_path.exists():
        return
    parser.error(
        f"Primary YAML not found at {primary_path}. "
        "Use --seed-from-markdown to generate it first."
    )


def maybe_seed_primary(args: argparse.Namespace, primary_path: Path) -> None:
    """Provide focused helper behavior for the split module."""
    if not args.seed_from_markdown:
        return
    source_path = args.seed_from_markdown.expanduser()
    primary_path.parent.mkdir(parents=True, exist_ok=True)
    seed_primary_from_markdown(source_path, primary_path)
