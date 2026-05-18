# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Render and write generated ethos output files.

Responsibility is narrow.
Public imports stay aligned.
"""

from pathlib import Path

from coding_ethos.cli_options import MergeSettings
from coding_ethos.loaders import load_primary_bundle, merge_repo_ethos
from coding_ethos.merging import (
    MergeRequest,
    inject_addendum_block,
    inject_import_block,
    merge_with_engine,
    resolve_merge_bin,
    should_merge_existing,
)
from coding_ethos.models import SUPPORTED_AGENTS, EthosBundle
from coding_ethos.renderers import (
    render_agent_addendum,
    render_agent_root_outputs,
    render_claude_memory,
    render_ethos_md,
    render_principle_detail,
    render_prompt_addon,
    render_shared_ethos_index,
    required_root_imports,
    root_merge_topics,
)

MAX_MERGE_TOPICS = 12


def write_file(path: Path, content: str) -> None:
    """Provide focused helper behavior for the split module."""
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        path.unlink()
    path.write_text(content, encoding="utf-8")


def load_bundle(primary_path: Path, repo_ethos_path: Path) -> EthosBundle:
    """Provide focused helper behavior for the split module."""
    return merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)


def render_contents(bundle: EthosBundle, repo_root: Path) -> dict[str, str]:
    """Provide focused helper behavior for the split module."""
    rendered: dict[str, str] = {
        "ETHOS.md": render_ethos_md(bundle, repo_root),
        ".claude/ethos/MEMORY.md": render_claude_memory(bundle, repo_root),
        ".agents/ethos/README.md": render_shared_ethos_index(bundle, repo_root),
    }
    rendered.update(render_agent_root_outputs(bundle, repo_root))

    for principle in bundle.principles:
        rendered[f".agents/ethos/{principle.id}.md"] = render_principle_detail(
            bundle, principle, repo_root
        )

    for agent in SUPPORTED_AGENTS:
        rendered[f".agent-context/prompt-addons/{agent}.md"] = render_prompt_addon(
            bundle, agent, repo_root
        )

    return rendered


def merge_topics_for_target(bundle: EthosBundle, relative_path: str) -> list[str]:
    """Provide focused helper behavior for the split module."""
    topics = root_merge_topics(relative_path)

    for principle in bundle.principles:
        for topic in principle.merge_topics:
            if topic not in topics:
                topics.append(topic)
            if len(topics) >= MAX_MERGE_TOPICS:
                return topics
    return topics


def write_outputs(
    bundle: EthosBundle,
    repo_root: Path,
    rendered: dict[str, str],
    *,
    merge_settings: MergeSettings,
) -> list[Path]:
    """Provide focused helper behavior for the split module."""
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
                final_content = inject_import_block(
                    target_name=relative_path,
                    existing_content=existing_content,
                    import_lines=required_root_imports(relative_path),
                )
                final_content = inject_addendum_block(
                    target_name=relative_path,
                    existing_content=final_content,
                    addendum_content=render_agent_addendum(
                        bundle,
                        repo_root,
                        relative_path,
                        content,
                    ),
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
                        merge_topics=merge_topics_for_target(bundle, relative_path),
                        timeout_seconds=merge_settings.timeout_seconds,
                    ),
                )
        write_file(absolute_path, final_content)
        written.append(absolute_path)

    return written


def print_written_paths(paths: list[Path]) -> None:
    """Provide focused helper behavior for the split module."""
    print("\n".join(str(path) for path in paths))
