# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

import os
import re
import shutil
import signal
import subprocess
import tempfile
from pathlib import Path

MERGEABLE_FILES = {"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
SUPPORTED_MERGE_ENGINES = ("codex", "gemini", "claude")
SUPPORTED_MERGE_STRATEGIES = ("inject", "llm")


def should_merge_existing(relative_path: str) -> bool:
    return relative_path in MERGEABLE_FILES


def build_merge_prompt(target_name: str, merge_topics: list[str] | None = None) -> str:
    topic_lines = ""
    if merge_topics:
        topic_lines = "\nPreserve repo-specific content related to these topics when it is still relevant:\n"
        topic_lines += "\n".join(f"- {topic}" for topic in merge_topics)
        topic_lines += "\n"

    return f"""You are merging two versions of `{target_name}`.

Inputs in the current directory:
- `existing.md`: the current file from the repo
- `generated.md`: the newly generated ethos-aware candidate

Your task:
1. Read both files completely.
2. Produce a merged result at `merged.md`.

Merge requirements:
- Preserve important repo-specific operational content from `existing.md`.
- Integrate all important ethos and agent-guidance content from `generated.md`.
- Prefer preserving concrete repo instructions, commands, paths, caveats, imports, and process notes from `existing.md` when they still apply.
- Prefer preserving structure that makes the file usable by the target agent.
{topic_lines}
- Keep imports, references, commands, paths, workflow notes, and local conventions if they are still relevant.
- Remove obvious duplication and resolve contradictions in favor of the newer generated ethos guidance where the old file is generic or redundant.
- Do not collapse the file into a tiny summary if `existing.md` contains important concrete repo instructions.
- Keep the result as valid Markdown.
- Only write `merged.md`. Do not create any other files.

Output contract:
- `merged.md` must contain the final merged file content for `{target_name}`.
"""


def _default_binary_name(engine: str) -> str:
    return {
        "codex": "codex",
        "gemini": "gemini",
        "claude": "claude",
    }[engine]


def resolve_merge_bin(engine: str, explicit_bin: str | None = None) -> str:
    if engine not in SUPPORTED_MERGE_ENGINES:
        raise ValueError(f"Unsupported merge engine: {engine}")
    if explicit_bin:
        return explicit_bin
    return shutil.which(_default_binary_name(engine)) or _default_binary_name(engine)


def resolve_codex_bin(explicit_bin: str | None = None) -> str:
    return resolve_merge_bin("codex", explicit_bin)


def _build_codex_command(
    *,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str | None,
) -> list[str]:
    command = [
        binary,
        "exec",
        "--skip-git-repo-check",
        "--sandbox",
        "workspace-write",
        "--full-auto",
        "-C",
        str(temp_root),
        prompt,
    ]
    if model:
        command[2:2] = ["--model", model]
    return command


def _build_gemini_command(
    *,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str | None,
) -> list[str]:
    command = [
        binary,
        "--prompt",
        prompt,
        "--sandbox",
        "--yolo",
        "--output-format",
        "text",
    ]
    if model:
        command[1:1] = ["--model", model]
    return command


def _build_claude_command(
    *,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str | None,
) -> list[str]:
    command = [
        binary,
        "--print",
        prompt,
        "--output-format",
        "text",
        "--add-dir",
        str(temp_root),
        "--permission-mode",
        "bypassPermissions",
        "--dangerously-skip-permissions",
    ]
    if model:
        command[1:1] = ["--model", model]
    return command


def _build_merge_command(
    *,
    engine: str,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str | None,
) -> list[str]:
    if engine == "codex":
        return _build_codex_command(binary=binary, temp_root=temp_root, prompt=prompt, model=model)
    if engine == "gemini":
        return _build_gemini_command(binary=binary, temp_root=temp_root, prompt=prompt, model=model)
    if engine == "claude":
        return _build_claude_command(binary=binary, temp_root=temp_root, prompt=prompt, model=model)
    raise ValueError(f"Unsupported merge engine: {engine}")


def _format_process_output(stdout: str, stderr: str) -> str:
    parts: list[str] = []
    if stdout.strip():
        parts.append(f"stdout:\n{stdout.strip()}")
    if stderr.strip():
        parts.append(f"stderr:\n{stderr.strip()}")
    return "\n\n".join(parts).strip()


def _run_command_with_timeout(
    *,
    command: list[str],
    cwd: Path,
    timeout_seconds: int,
    target_name: str,
    engine: str,
) -> tuple[int, str, str]:
    process = subprocess.Popen(
        command,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        start_new_session=True,
    )
    try:
        stdout, stderr = process.communicate(timeout=timeout_seconds)
    except subprocess.TimeoutExpired as exc:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            stdout, stderr = process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            stdout, stderr = process.communicate()
        output = _format_process_output(stdout or "", stderr or "")
        details = f"\n\n{output}" if output else ""
        raise RuntimeError(
            f"{engine.title()} merge timed out for {target_name} after {timeout_seconds} seconds.{details}"
        ) from exc
    return process.returncode, stdout, stderr


def merge_with_engine(
    *,
    engine: str,
    binary: str,
    target_name: str,
    existing_content: str,
    generated_content: str,
    model: str | None = None,
    merge_topics: list[str] | None = None,
    timeout_seconds: int = 300,
) -> str:
    if engine not in SUPPORTED_MERGE_ENGINES:
        raise ValueError(f"Unsupported merge engine: {engine}")

    with tempfile.TemporaryDirectory(prefix="coding-ethos-merge-") as tmp_dir:
        temp_root = Path(tmp_dir)
        (temp_root / "existing.md").write_text(existing_content, encoding="utf-8")
        (temp_root / "generated.md").write_text(generated_content, encoding="utf-8")
        prompt = build_merge_prompt(target_name, merge_topics)
        command = _build_merge_command(
            engine=engine,
            binary=binary,
            temp_root=temp_root,
            prompt=prompt,
            model=model,
        )

        return_code, stdout, stderr = _run_command_with_timeout(
            command=command,
            cwd=temp_root,
            timeout_seconds=timeout_seconds,
            target_name=target_name,
            engine=engine,
        )
        if return_code != 0:
            output = _format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise RuntimeError(
                f"{engine.title()} merge failed for {target_name} with exit code {return_code}.{details}"
            )

        merged_path = temp_root / "merged.md"
        if not merged_path.exists():
            output = _format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise RuntimeError(f"{engine.title()} merge did not produce merged.md for {target_name}.{details}")
        return merged_path.read_text(encoding="utf-8")


def _block_markers(target_name: str, block_name: str) -> tuple[str, str]:
    token = f"{block_name} {target_name}"
    return (f"<!-- coding-ethos:begin {token} -->", f"<!-- coding-ethos:end {token} -->")


def _remove_managed_block(content: str, begin_marker: str, end_marker: str) -> tuple[str, bool]:
    start = content.find(begin_marker)
    if start == -1:
        return content, False

    end = content.find(end_marker, start)
    if end == -1:
        return content, False

    end += len(end_marker)
    before = content[:start].rstrip("\n")
    after = content[end:].lstrip("\n")

    if before and after:
        merged = f"{before}\n\n{after}"
    else:
        merged = before + after

    if merged and not merged.endswith("\n"):
        merged += "\n"
    return merged, True


def _build_managed_block(begin_marker: str, end_marker: str, body: str) -> str:
    return f"{begin_marker}\n{body.rstrip()}\n{end_marker}"


def _append_managed_block(content: str, *, target_name: str, block_name: str, body: str) -> str:
    begin_marker, end_marker = _block_markers(target_name, block_name)
    base_content, _ = _remove_managed_block(content, begin_marker, end_marker)
    block = _build_managed_block(begin_marker, end_marker, body)

    if not base_content.strip():
        return block + "\n"
    return base_content.rstrip() + "\n\n" + block + "\n"


def _prepend_managed_block(content: str, *, target_name: str, block_name: str, body: str) -> str:
    begin_marker, end_marker = _block_markers(target_name, block_name)
    base_content, _ = _remove_managed_block(content, begin_marker, end_marker)
    block = _build_managed_block(begin_marker, end_marker, body)

    if not base_content.strip():
        return block + "\n"
    return block + "\n\n" + base_content.lstrip("\n")


def inject_import_block(
    *,
    target_name: str,
    existing_content: str,
    import_lines: list[str],
) -> str:
    if not import_lines:
        return existing_content

    begin_marker, end_marker = _block_markers(target_name, "imports")
    content_without_block, _ = _remove_managed_block(existing_content, begin_marker, end_marker)
    present_lines = {line.strip() for line in content_without_block.splitlines()}
    missing_imports = [line for line in import_lines if line not in present_lines]
    if not missing_imports:
        return content_without_block
    return _prepend_managed_block(
        content_without_block,
        target_name=target_name,
        block_name="imports",
        body="\n".join(missing_imports),
    )


def inject_addendum_block(
    *,
    target_name: str,
    existing_content: str,
    addendum_content: str,
) -> str:
    return _append_managed_block(
        existing_content,
        target_name=target_name,
        block_name="managed",
        body=addendum_content,
    )


def strip_managed_blocks(content: str) -> str:
    stripped = content
    pattern = re.compile(r"<!-- coding-ethos:begin .*?<!-- coding-ethos:end .*?-->", re.DOTALL)
    while True:
        updated = pattern.sub("", stripped)
        if updated == stripped:
            break
        stripped = updated
    stripped = re.sub(r"\n{3,}", "\n\n", stripped).strip("\n")
    return stripped + "\n" if stripped else ""


def merge_with_codex(
    *,
    codex_bin: str,
    target_name: str,
    existing_content: str,
    generated_content: str,
    model: str | None = None,
    merge_topics: list[str] | None = None,
    timeout_seconds: int = 300,
) -> str:
    return merge_with_engine(
        engine="codex",
        binary=codex_bin,
        target_name=target_name,
        existing_content=existing_content,
        generated_content=generated_content,
        model=model,
        merge_topics=merge_topics,
        timeout_seconds=timeout_seconds,
    )
