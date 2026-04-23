# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

import argparse
from pathlib import Path

from coding_ethos.loaders import load_primary_bundle, merge_repo_ethos
from coding_ethos.markdown_seed import seed_primary_from_markdown
from coding_ethos.merging import (
    SUPPORTED_MERGE_ENGINES,
    SUPPORTED_MERGE_STRATEGIES,
    inject_addendum_block,
    inject_import_block,
    merge_with_engine,
    resolve_merge_bin,
    should_merge_existing,
)
from coding_ethos.models import SUPPORTED_AGENTS
from coding_ethos.renderers import (
    render_agents_md,
    render_agents_addendum,
    render_claude_md,
    render_claude_addendum,
    render_claude_memory,
    render_ethos_md,
    render_gemini_md,
    render_gemini_addendum,
    render_principle_detail,
    render_prompt_addon,
    required_root_imports,
    render_shared_ethos_index,
)
from coding_ethos.tool_configs import check_tool_configs, sync_tool_configs


def _write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        path.unlink()
    path.write_text(content, encoding="utf-8")


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Generate Codex, Claude Code, and Gemini instruction files from a shared ethos."
    )
    parser.add_argument(
        "--repo",
        type=Path,
        help="Repository directory where generated files should be written.",
    )
    parser.add_argument(
        "--primary",
        type=Path,
        help="Primary ethos YAML. Defaults to coding_ethos.yml, with .yaml aliases also supported.",
    )
    parser.add_argument(
        "--repo-ethos",
        type=Path,
        help="Optional repo-specific ethos YAML. Defaults to <repo>/repo_ethos.yml.",
    )
    parser.add_argument(
        "--repo-config",
        type=Path,
        help="Optional repo-specific enforcement config YAML. Defaults to <repo>/repo_config.yml or .yaml.",
    )
    parser.add_argument(
        "--seed-from-markdown",
        type=Path,
        help="Seed or refresh the primary YAML from a markdown source before rendering.",
    )
    parser.add_argument(
        "--merge-existing",
        action="store_true",
        help="Preserve existing AGENTS.md/CLAUDE.md/GEMINI.md and inject managed generated blocks instead of replacing them.",
    )
    parser.add_argument(
        "--merge-strategy",
        choices=SUPPORTED_MERGE_STRATEGIES,
        default="inject",
        help="Merge strategy for existing root files. Defaults to inject; use llm only for full-document AI merges.",
    )
    parser.add_argument(
        "--merge-engine",
        choices=SUPPORTED_MERGE_ENGINES,
        default="codex",
        help="LLM CLI to use when --merge-strategy=llm. Defaults to codex.",
    )
    parser.add_argument(
        "--merge-bin",
        help="Path to the selected merge engine CLI binary. Defaults to the engine name from PATH.",
    )
    parser.add_argument(
        "--merge-model",
        help="Optional model override for merge mode.",
    )
    parser.add_argument(
        "--merge-timeout-seconds",
        type=int,
        default=300,
        help="Timeout for each root file when --merge-strategy=llm. Defaults to 300 seconds.",
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
        help="Generate pyrightconfig.json, mypy.ini, ruff.toml, and .yamllint.yml into --repo.",
    )
    parser.add_argument(
        "--check-tool-configs",
        action="store_true",
        help="Fail if generated tool config files in --repo are missing or out of sync.",
    )
    return parser


def _resolve_repo_ethos(repo_root: Path, explicit_repo_ethos: Path | None) -> Path | None:
    if explicit_repo_ethos is not None:
        return explicit_repo_ethos
    for name in ("repo_ethos.yml", "repo_ethos.yaml"):
        candidate = repo_root / name
        if candidate.exists():
            return candidate
    return None


def _resolve_primary_path(explicit_primary: Path | None) -> Path:
    if explicit_primary is not None:
        return explicit_primary.resolve()
    for name in ("coding_ethos.yml", "coding_ethos.yaml", "code_ethos.yml", "code_ethos.yaml"):
        candidate = Path(name)
        if candidate.exists():
            return candidate.resolve()
    return Path("coding_ethos.yml").resolve()


def _load_bundle(primary_path: Path, repo_ethos_path: Path | None):
    return merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)


def _render_contents(bundle, repo_root: Path) -> dict[str, str]:
    rendered: dict[str, str] = {
        "ETHOS.md": render_ethos_md(bundle, repo_root),
        "AGENTS.md": render_agents_md(bundle, repo_root),
        "CLAUDE.md": render_claude_md(bundle),
        ".claude/ethos/MEMORY.md": render_claude_memory(bundle, repo_root),
        "GEMINI.md": render_gemini_md(bundle, repo_root),
        ".agents/ethos/README.md": render_shared_ethos_index(bundle, repo_root),
    }

    for principle in bundle.principles:
        rendered[f".agents/ethos/{principle.id}.md"] = render_principle_detail(bundle, principle, repo_root)

    for agent in SUPPORTED_AGENTS:
        rendered[f".agent-context/prompt-addons/{agent}.md"] = render_prompt_addon(bundle, agent, repo_root)

    return rendered


def _merge_topics_for_target(bundle, relative_path: str) -> list[str]:
    topics: list[str] = []

    if relative_path == "AGENTS.md":
        topics.extend(["repo commands", "key paths", "repo operating notes"])
    elif relative_path == "CLAUDE.md":
        topics.extend(["Claude imports", "memory links", "Claude-specific workflow notes"])
    elif relative_path == "GEMINI.md":
        topics.extend(["Gemini root guidance", "linked detail docs", "repo operating notes"])

    for principle in bundle.principles:
        for topic in principle.merge_topics:
            if topic not in topics:
                topics.append(topic)
            if len(topics) >= 12:
                return topics
    return topics


def _write_outputs(
    bundle,
    repo_root: Path,
    rendered: dict[str, str],
    *,
    merge_existing: bool,
    merge_strategy: str,
    merge_engine: str,
    merge_bin: str | None,
    merge_model: str | None,
    merge_timeout_seconds: int,
) -> list[Path]:
    written: list[Path] = []

    for relative_path, content in rendered.items():
        absolute_path = repo_root / relative_path
        final_content = content
        if merge_existing and should_merge_existing(relative_path) and absolute_path.exists():
            existing_content = absolute_path.read_text(encoding="utf-8")
            if merge_strategy == "inject":
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
                    engine=merge_engine,
                    binary=resolve_merge_bin(merge_engine, merge_bin),
                    target_name=relative_path,
                    existing_content=existing_content,
                    generated_content=content,
                    model=merge_model,
                    merge_topics=_merge_topics_for_target(bundle, relative_path),
                    timeout_seconds=merge_timeout_seconds,
                )
        _write_file(absolute_path, final_content)
        written.append(absolute_path)

    return written


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)

    merge_bin = args.merge_bin
    merge_model = args.merge_model
    if args.merge_engine == "codex":
        if merge_bin is None:
            merge_bin = args.codex_bin
        if merge_model is None:
            merge_model = args.codex_model

    repo_root = args.repo.expanduser().resolve() if args.repo is not None else None

    if (args.sync_tool_configs or args.check_tool_configs) and repo_root is None:
        parser.error("--sync-tool-configs and --check-tool-configs require --repo.")

    if args.sync_tool_configs and repo_root is not None:
        repo_root.mkdir(parents=True, exist_ok=True)
        written = sync_tool_configs(repo_root, args.repo_config)
        print("\n".join(str(path) for path in written))

    if args.check_tool_configs and repo_root is not None:
        mismatched = check_tool_configs(repo_root, args.repo_config)
        if mismatched:
            print("\n".join(str(path) for path in mismatched))
            return 1

    if args.sync_tool_configs or args.check_tool_configs:
        return 0

    primary_path = _resolve_primary_path(args.primary)
    if args.seed_from_markdown:
        source_path = args.seed_from_markdown.expanduser()
        primary_path.parent.mkdir(parents=True, exist_ok=True)
        seed_primary_from_markdown(source_path, primary_path)

    if not primary_path.exists():
        parser.error(
            f"Primary YAML not found at {primary_path}. "
            "Use --seed-from-markdown to generate it first."
        )

    if repo_root is None:
        return 0

    repo_root.mkdir(parents=True, exist_ok=True)
    repo_ethos_path = _resolve_repo_ethos(repo_root, args.repo_ethos)
    bundle = _load_bundle(primary_path, repo_ethos_path)
    rendered = _render_contents(bundle, repo_root)
    written = _write_outputs(
        bundle,
        repo_root,
        rendered,
        merge_existing=args.merge_existing,
        merge_strategy=args.merge_strategy,
        merge_engine=args.merge_engine,
        merge_bin=merge_bin,
        merge_model=merge_model,
        merge_timeout_seconds=args.merge_timeout_seconds,
    )
    print("\n".join(str(path) for path in written))
    return 0
