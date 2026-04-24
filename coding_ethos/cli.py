"""CLI orchestration for generating ethos outputs and derived hook artifacts.

This module keeps argument parsing and top-level workflow coordination in one
place while delegating rendering, loading, and merge logic to narrower helpers.
It is the only supported command-line entrypoint for generation and sync flows.
"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

import argparse
from dataclasses import dataclass
from pathlib import Path

from coding_ethos.gemini_prompt_pack import (
    check_gemini_prompt_pack,
    sync_gemini_prompt_pack,
)
from coding_ethos.loaders import load_primary_bundle, merge_repo_ethos
from coding_ethos.markdown_seed import seed_primary_from_markdown
from coding_ethos.merging import (
    SUPPORTED_MERGE_ENGINES,
    SUPPORTED_MERGE_STRATEGIES,
    MergeRequest,
    inject_addendum_block,
    inject_import_block,
    merge_with_engine,
    resolve_merge_bin,
    should_merge_existing,
)
from coding_ethos.models import SUPPORTED_AGENTS, EthosBundle
from coding_ethos.renderers import (
    render_agents_addendum,
    render_agents_md,
    render_claude_addendum,
    render_claude_md,
    render_claude_memory,
    render_ethos_md,
    render_gemini_addendum,
    render_gemini_md,
    render_principle_detail,
    render_prompt_addon,
    render_shared_ethos_index,
    required_root_imports,
)
from coding_ethos.tool_configs import check_tool_configs, sync_tool_configs

MAX_MERGE_TOPICS = 12


@dataclass(frozen=True, slots=True)
class MergeSettings:
    """Resolved merge behavior for writing root agent files."""

    existing: bool
    strategy: str
    engine: str
    binary: str
    model: str
    timeout_seconds: int


def _write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        path.unlink()
    path.write_text(content, encoding="utf-8")


def _build_parser() -> argparse.ArgumentParser:
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
        "--repo-config",
        type=Path,
        help=(
            "Optional repo-specific enforcement config YAML. Defaults to "
            "<repo>/repo_config.yml or .yaml."
        ),
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
    parser.add_argument(
        "--sync-tool-configs",
        action="store_true",
        help=(
            "Generate pyrightconfig.json, mypy.ini, ruff.toml, and "
            ".yamllint.yml into --repo."
        ),
    )
    parser.add_argument(
        "--check-tool-configs",
        action="store_true",
        help=(
            "Fail if generated tool config files in --repo are missing or out of sync."
        ),
    )
    parser.add_argument(
        "--sync-gemini-prompts",
        action="store_true",
        help=(
            "Generate the grounded Gemini prompt pack into --repo/.code-ethos/gemini/."
        ),
    )
    parser.add_argument(
        "--check-gemini-prompts",
        action="store_true",
        help=(
            "Fail if the generated Gemini prompt pack in --repo is missing or "
            "out of sync."
        ),
    )
    return parser


def _resolve_repo_ethos(repo_root: Path, explicit_repo_ethos: object = "") -> Path:
    if isinstance(explicit_repo_ethos, Path):
        return explicit_repo_ethos.resolve()
    for name in ("repo_ethos.yml", "repo_ethos.yaml"):
        candidate = repo_root / name
        if candidate.exists():
            return candidate.resolve()
    return (repo_root / "repo_ethos.yml").resolve()


def _resolve_primary_path(explicit_primary: object = "") -> Path:
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
    return Path("coding_ethos.yml").resolve()


def _load_bundle(primary_path: Path, repo_ethos_path: Path) -> EthosBundle:
    return merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)


def _render_contents(bundle: EthosBundle, repo_root: Path) -> dict[str, str]:
    rendered: dict[str, str] = {
        "ETHOS.md": render_ethos_md(bundle, repo_root),
        "AGENTS.md": render_agents_md(bundle, repo_root),
        "CLAUDE.md": render_claude_md(bundle),
        ".claude/ethos/MEMORY.md": render_claude_memory(bundle, repo_root),
        "GEMINI.md": render_gemini_md(bundle, repo_root),
        ".agents/ethos/README.md": render_shared_ethos_index(bundle, repo_root),
    }

    for principle in bundle.principles:
        rendered[f".agents/ethos/{principle.id}.md"] = render_principle_detail(
            bundle, principle, repo_root
        )

    for agent in SUPPORTED_AGENTS:
        rendered[f".agent-context/prompt-addons/{agent}.md"] = render_prompt_addon(
            bundle, agent, repo_root
        )

    return rendered


def _merge_topics_for_target(bundle: EthosBundle, relative_path: str) -> list[str]:
    topics: list[str] = []

    if relative_path == "AGENTS.md":
        topics.extend(["repo commands", "key paths", "repo operating notes"])
    elif relative_path == "CLAUDE.md":
        topics.extend(
            ["Claude imports", "memory links", "Claude-specific workflow notes"]
        )
    elif relative_path == "GEMINI.md":
        topics.extend(
            ["Gemini root guidance", "linked detail docs", "repo operating notes"]
        )

    for principle in bundle.principles:
        for topic in principle.merge_topics:
            if topic not in topics:
                topics.append(topic)
            if len(topics) >= MAX_MERGE_TOPICS:
                return topics
    return topics


def _write_outputs(
    bundle: EthosBundle,
    repo_root: Path,
    rendered: dict[str, str],
    *,
    merge_settings: MergeSettings,
) -> list[Path]:
    written: list[Path] = []

    for relative_path, content in rendered.items():
        absolute_path = repo_root / relative_path
        final_content = content
        if (
            merge_settings.existing
            and should_merge_existing(relative_path)
            and absolute_path.exists()
        ):
            existing_content = absolute_path.read_text(encoding="utf-8")
            if merge_settings.strategy == "inject":
                if relative_path == "AGENTS.md":
                    addendum_content = render_agents_addendum(bundle, repo_root)
                elif relative_path == "CLAUDE.md":
                    addendum_content = render_claude_addendum(bundle, repo_root)
                elif relative_path == "GEMINI.md":
                    addendum_content = render_gemini_addendum(bundle, repo_root)
                else:
                    addendum_content = content

                final_content = inject_import_block(
                    target_name=relative_path,
                    existing_content=existing_content,
                    import_lines=required_root_imports(relative_path),
                )
                final_content = inject_addendum_block(
                    target_name=relative_path,
                    existing_content=final_content,
                    addendum_content=addendum_content,
                )
            else:
                final_content = merge_with_engine(
                    engine=merge_settings.engine,
                    binary=resolve_merge_bin(
                        merge_settings.engine, merge_settings.binary
                    ),
                    request=MergeRequest(
                        target_name=relative_path,
                        existing_content=existing_content,
                        generated_content=content,
                        model=merge_settings.model,
                        merge_topics=_merge_topics_for_target(bundle, relative_path),
                        timeout_seconds=merge_settings.timeout_seconds,
                    ),
                )
        _write_file(absolute_path, final_content)
        written.append(absolute_path)

    return written


def _print_written_paths(paths: list[Path]) -> None:
    print("\n".join(str(path) for path in paths))


def _resolve_merge_settings(args: argparse.Namespace) -> MergeSettings:
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


def _has_repo_root(args: argparse.Namespace) -> bool:
    return isinstance(args.repo, Path)


def _repo_root_from_args(args: argparse.Namespace) -> Path:
    if isinstance(args.repo, Path):
        return args.repo.expanduser().resolve()
    return Path.cwd().resolve()


def _tool_actions_requested(args: argparse.Namespace) -> bool:
    return (
        args.sync_tool_configs
        or args.check_tool_configs
        or args.sync_gemini_prompts
        or args.check_gemini_prompts
    )


def _require_repo_root(
    parser: argparse.ArgumentParser, args: argparse.Namespace
) -> Path:
    if not _has_repo_root(args):
        parser.error(
            "--sync-tool-configs, --check-tool-configs, --sync-gemini-prompts, "
            "and --check-gemini-prompts require --repo."
        )
    return _repo_root_from_args(args)


def _require_primary_path(parser: argparse.ArgumentParser, primary_path: Path) -> None:
    if primary_path.exists():
        return
    parser.error(
        f"Primary YAML not found at {primary_path}. "
        "Use --seed-from-markdown to generate it first."
    )


def _run_tool_config_actions(args: argparse.Namespace) -> int:
    if not args.sync_tool_configs and not args.check_tool_configs:
        return 0
    resolved_repo_root = _require_repo_root(_build_parser(), args)
    resolved_repo_root.mkdir(parents=True, exist_ok=True)
    if args.sync_tool_configs:
        _print_written_paths(sync_tool_configs(resolved_repo_root, args.repo_config))
    if args.check_tool_configs:
        mismatched = check_tool_configs(resolved_repo_root, args.repo_config)
        if mismatched:
            _print_written_paths(mismatched)
            return 1
    return 0


def _run_gemini_prompt_actions(
    args: argparse.Namespace,
    parser: argparse.ArgumentParser,
) -> int:
    if not args.sync_gemini_prompts and not args.check_gemini_prompts:
        return 0
    resolved_repo_root = _require_repo_root(parser, args)
    primary_path = _resolve_primary_path(args.primary)
    _require_primary_path(parser, primary_path)
    repo_ethos_path = _resolve_repo_ethos(resolved_repo_root, args.repo_ethos)
    resolved_repo_root.mkdir(parents=True, exist_ok=True)
    if args.sync_gemini_prompts:
        written = sync_gemini_prompt_pack(
            repo_root=resolved_repo_root,
            primary_path=primary_path,
            repo_ethos_path=repo_ethos_path,
            repo_config_path=args.repo_config,
        )
        _print_written_paths(written)
    if args.check_gemini_prompts:
        mismatched = check_gemini_prompt_pack(
            repo_root=resolved_repo_root,
            primary_path=primary_path,
            repo_ethos_path=repo_ethos_path,
            repo_config_path=args.repo_config,
        )
        if mismatched:
            _print_written_paths(mismatched)
            return 1
    return 0


def _maybe_seed_primary(args: argparse.Namespace, primary_path: Path) -> None:
    if not args.seed_from_markdown:
        return
    source_path = args.seed_from_markdown.expanduser()
    primary_path.parent.mkdir(parents=True, exist_ok=True)
    seed_primary_from_markdown(source_path, primary_path)


def _generate_outputs(
    args: argparse.Namespace,
    merge_settings: MergeSettings,
) -> int:
    repo_root = _repo_root_from_args(args)
    primary_path = _resolve_primary_path(args.primary)
    _maybe_seed_primary(args, primary_path)
    _require_primary_path(_build_parser(), primary_path)
    repo_root.mkdir(parents=True, exist_ok=True)
    repo_ethos_path = _resolve_repo_ethos(repo_root, args.repo_ethos)
    bundle = _load_bundle(primary_path, repo_ethos_path)
    rendered = _render_contents(bundle, repo_root)
    written = _write_outputs(
        bundle,
        repo_root,
        rendered,
        merge_settings=merge_settings,
    )
    _print_written_paths(written)
    return 0


def main(argv: list[str] | None = None) -> int:
    """Run the coding-ethos command-line interface.

    Args:
        argv: Optional argument vector for tests or embedding. When ``None``,
            arguments are read from ``sys.argv`` through ``argparse``.

    Returns:
        Process-style exit code. ``0`` indicates success and ``1`` indicates a
        validation, sync, or merge failure that should stop the caller.

    """
    parser = _build_parser()
    args = parser.parse_args(argv)
    merge_settings = _resolve_merge_settings(args)

    tool_result = _run_tool_config_actions(args)
    if tool_result != 0:
        return tool_result

    gemini_result = _run_gemini_prompt_actions(args, parser)
    if gemini_result != 0:
        return gemini_result

    if _tool_actions_requested(args):
        return 0

    if not _has_repo_root(args):
        return 0
    return _generate_outputs(args, merge_settings)
