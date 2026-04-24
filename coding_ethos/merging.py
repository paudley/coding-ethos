"""Merge generated root files with existing repo-owned agent documents.

This module keeps merge policy, managed-block injection, and external merge
engine orchestration together so the CLI can remain a thin workflow shell.
It is responsible for preserving local guidance without forking the renderer.
"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

import os
import re
import shutil
import signal
import subprocess
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

MERGEABLE_FILES = {"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
SUPPORTED_MERGE_ENGINES = ("codex", "gemini", "claude")
SUPPORTED_MERGE_STRATEGIES = ("inject", "llm")


class UnsupportedMergeEngineError(ValueError):
    """Raised when a configured merge engine name is not supported."""

    def __init__(self, engine: str) -> None:
        """Initialize the error with the unsupported engine name."""
        super().__init__(f"Unsupported merge engine: {engine}")


class MergeTimeoutError(RuntimeError):
    """Raised when an external merge engine exceeds its timeout."""

    def __init__(
        self, engine: str, target_name: str, timeout_seconds: int, details: str
    ) -> None:
        """Initialize the error with timeout context and captured output."""
        super().__init__(
            f"{engine.title()} merge timed out for {target_name} after "
            f"{timeout_seconds} seconds.{details}"
        )


class MergeCommandFailedError(RuntimeError):
    """Raised when an external merge engine exits non-zero."""

    def __init__(
        self, engine: str, target_name: str, return_code: int, details: str
    ) -> None:
        """Initialize the error with exit-code context and captured output."""
        super().__init__(
            f"{engine.title()} merge failed for {target_name} with exit code "
            f"{return_code}.{details}"
        )


class MissingMergedOutputError(RuntimeError):
    """Raised when an external merge engine does not write `merged.md`."""

    def __init__(self, engine: str, target_name: str, details: str) -> None:
        """Initialize the error with the missing-output context."""
        super().__init__(
            f"{engine.title()} merge did not produce merged.md for "
            f"{target_name}.{details}"
        )


@dataclass(frozen=True, slots=True)
class MergeRequest:
    """One root-file merge request passed to an external merge engine."""

    target_name: str
    existing_content: str
    generated_content: str
    model: str = ""
    merge_topics: list[str] = field(default_factory=list)
    timeout_seconds: int = 300


def should_merge_existing(relative_path: str) -> bool:
    """Return whether a generated file supports merge-preserving writes."""
    return relative_path in MERGEABLE_FILES


def build_merge_prompt(target_name: str, merge_topics: list[str]) -> str:
    """Build the external LLM merge prompt for one root document."""
    topic_lines = ""
    if merge_topics:
        topic_lines = (
            "\nPreserve repo-specific content related to these topics when it "
            "is still relevant:\n"
        )
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
- Prefer preserving concrete repo instructions, commands, paths, caveats,
  imports, and process notes from `existing.md` when they still apply.
- Prefer preserving structure that makes the file usable by the target agent.
{topic_lines}
- Keep imports, references, commands, paths, workflow notes, and local
  conventions if they are still relevant.
- Remove obvious duplication and resolve contradictions in favor of the newer
  generated ethos guidance where the old file is generic or redundant.
- Do not collapse the file into a tiny summary if `existing.md` contains
  important concrete repo instructions.
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


def resolve_merge_bin(engine: str, explicit_bin: str = "") -> str:
    """Resolve the CLI binary used for a selected merge engine."""
    if engine not in SUPPORTED_MERGE_ENGINES:
        raise UnsupportedMergeEngineError(engine)
    if explicit_bin:
        return explicit_bin
    return shutil.which(_default_binary_name(engine)) or _default_binary_name(engine)


def resolve_codex_bin(explicit_bin: str = "") -> str:
    """Resolve the Codex CLI binary, preserving the legacy helper name."""
    return resolve_merge_bin("codex", explicit_bin)


def _build_codex_command(
    *,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str,
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
    prompt: str,
    model: str,
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
    model: str,
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
    model: str,
) -> list[str]:
    if engine == "codex":
        return _build_codex_command(
            binary=binary, temp_root=temp_root, prompt=prompt, model=model
        )
    if engine == "gemini":
        return _build_gemini_command(binary=binary, prompt=prompt, model=model)
    if engine == "claude":
        return _build_claude_command(
            binary=binary, temp_root=temp_root, prompt=prompt, model=model
        )
    raise UnsupportedMergeEngineError(engine)


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
        raise MergeTimeoutError(engine, target_name, timeout_seconds, details) from exc
    return process.returncode, stdout, stderr


def merge_with_engine(
    *,
    engine: str,
    binary: str,
    request: MergeRequest,
) -> str:
    """Merge one generated root file through an external agent CLI.

    Args:
        engine: Selected merge engine identifier.
        binary: Executable name or path for the engine.
        request: Merge payload and metadata for the root file.

    Returns:
        Final merged Markdown content read from ``merged.md``.

    Raises:
        RuntimeError: The external merge command fails, times out, or does not
            produce the required output file.

    """
    if engine not in SUPPORTED_MERGE_ENGINES:
        raise UnsupportedMergeEngineError(engine)

    with tempfile.TemporaryDirectory(prefix="coding-ethos-merge-") as tmp_dir:
        temp_root = Path(tmp_dir)
        (temp_root / "existing.md").write_text(
            request.existing_content,
            encoding="utf-8",
        )
        (temp_root / "generated.md").write_text(
            request.generated_content,
            encoding="utf-8",
        )
        prompt = build_merge_prompt(request.target_name, request.merge_topics)
        command = _build_merge_command(
            engine=engine,
            binary=binary,
            temp_root=temp_root,
            prompt=prompt,
            model=request.model,
        )

        return_code, stdout, stderr = _run_command_with_timeout(
            command=command,
            cwd=temp_root,
            timeout_seconds=request.timeout_seconds,
            target_name=request.target_name,
            engine=engine,
        )
        if return_code != 0:
            output = _format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise MergeCommandFailedError(
                engine,
                request.target_name,
                return_code,
                details,
            )

        merged_path = temp_root / "merged.md"
        if not merged_path.exists():
            output = _format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise MissingMergedOutputError(
                engine,
                request.target_name,
                details,
            )
        return merged_path.read_text(encoding="utf-8")


def _block_markers(target_name: str, block_name: str) -> tuple[str, str]:
    token = f"{block_name} {target_name}"
    return (
        f"<!-- coding-ethos:begin {token} -->",
        f"<!-- coding-ethos:end {token} -->",
    )


def _remove_managed_block(
    content: str, begin_marker: str, end_marker: str
) -> tuple[str, bool]:
    start = content.find(begin_marker)
    if start == -1:
        return content, False

    end = content.find(end_marker, start)
    if end == -1:
        return content, False

    end += len(end_marker)
    before = content[:start].rstrip("\n")
    after = content[end:].lstrip("\n")

    merged = f"{before}\n\n{after}" if before and after else before + after

    if merged and not merged.endswith("\n"):
        merged += "\n"
    return merged, True


def _build_managed_block(begin_marker: str, end_marker: str, body: str) -> str:
    return f"{begin_marker}\n{body.rstrip()}\n{end_marker}"


def _append_managed_block(
    content: str, *, target_name: str, block_name: str, body: str
) -> str:
    begin_marker, end_marker = _block_markers(target_name, block_name)
    base_content, _ = _remove_managed_block(content, begin_marker, end_marker)
    block = _build_managed_block(begin_marker, end_marker, body)

    if not base_content.strip():
        return block + "\n"
    return base_content.rstrip() + "\n\n" + block + "\n"


def _prepend_managed_block(
    content: str, *, target_name: str, block_name: str, body: str
) -> str:
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
    """Inject required managed import lines into an existing root file."""
    if not import_lines:
        return existing_content

    begin_marker, end_marker = _block_markers(target_name, "imports")
    content_without_block, _ = _remove_managed_block(
        existing_content, begin_marker, end_marker
    )
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
    """Append the managed additive ethos block to an existing root file."""
    return _append_managed_block(
        existing_content,
        target_name=target_name,
        block_name="managed",
        body=addendum_content,
    )


def strip_managed_blocks(content: str) -> str:
    """Remove managed coding-ethos blocks from a root file."""
    stripped = content
    pattern = re.compile(
        r"<!-- coding-ethos:begin .*?<!-- coding-ethos:end .*?-->", re.DOTALL
    )
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
    request: MergeRequest,
) -> str:
    """Merge one root file through the Codex CLI."""
    return merge_with_engine(
        engine="codex",
        binary=codex_bin,
        request=request,
    )
