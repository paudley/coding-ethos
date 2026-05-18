# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""CLI orchestration for generating ethos outputs and derived artifacts.

Responsibility is narrow.
Public imports stay aligned.
"""

import argparse

from coding_ethos.cli_generation import (
    load_bundle,
    print_written_paths,
    render_contents,
    write_outputs,
)
from coding_ethos.cli_options import (
    MergeSettings,
    build_parser,
    has_repo_root,
    maybe_seed_primary,
    repo_root_from_args,
    require_primary_path,
    resolve_merge_settings,
    resolve_primary_path,
    resolve_repo_ethos,
    resolve_seed_primary_path,
)


def generate_outputs(args: argparse.Namespace, merge_settings: MergeSettings) -> int:
    """Provide focused helper behavior for the split module."""
    repo_root = repo_root_from_args(args)
    primary_path = (
        resolve_seed_primary_path(args.primary)
        if args.seed_from_markdown
        else resolve_primary_path(args.primary)
    )
    maybe_seed_primary(args, primary_path)
    require_primary_path(build_parser(), primary_path)
    repo_root.mkdir(parents=True, exist_ok=True)
    repo_ethos_path = resolve_repo_ethos(repo_root, args.repo_ethos)
    bundle = load_bundle(primary_path, repo_ethos_path)
    rendered = render_contents(bundle, repo_root)
    written = write_outputs(
        bundle,
        repo_root,
        rendered,
        merge_settings=merge_settings,
    )
    print_written_paths(written)
    return 0


def main(argv: list[str] | None = None) -> int:
    """Run the coding-ethos command-line interface."""
    parser = build_parser()
    args = parser.parse_args(argv)
    merge_settings = resolve_merge_settings(args)

    if not has_repo_root(args):
        return 0
    return generate_outputs(args, merge_settings)
