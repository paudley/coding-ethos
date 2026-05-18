# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""External merge-engine command contracts and execution.

Responsibility is narrow.
Public imports stay aligned.
"""

import os
import signal
import subprocess
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

from coding_ethos.merge_commands import (
    SUPPORTED_MERGE_ENGINES,
    build_merge_command,
    merge_engine_spec,
    resolve_codex_bin,
    resolve_merge_bin,
)
from coding_ethos.merge_errors import (
    MergeCommandFailedError,
    MergeTimeoutError,
    MissingMergedOutputError,
    UnsupportedMergeEngineError,
)
from coding_ethos.merge_prompt import build_merge_prompt

MERGEABLE_FILES = {"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
SUPPORTED_MERGE_STRATEGIES = ("inject", "llm")

__all__ = [
    "MERGEABLE_FILES",
    "SUPPORTED_MERGE_ENGINES",
    "SUPPORTED_MERGE_STRATEGIES",
    "MergeCommandFailedError",
    "MergeRequest",
    "MergeTimeoutError",
    "MissingMergedOutputError",
    "UnsupportedMergeEngineError",
    "build_merge_prompt",
    "merge_with_engine",
    "resolve_codex_bin",
    "resolve_merge_bin",
    "should_merge_existing",
]


def empty_merge_topics() -> list[str]:
    """Provide focused helper behavior for the split module."""
    return []


@dataclass(frozen=True, slots=True)
class MergeRequest:
    """One root-file merge request passed to an external merge engine."""

    target_name: str
    existing_content: str
    generated_content: str
    model: str = ""
    merge_topics: list[str] = field(default_factory=empty_merge_topics)
    timeout_seconds: int = 300


def should_merge_existing(relative_path: str) -> bool:
    """Return whether a generated file supports merge-preserving writes."""
    return relative_path in MERGEABLE_FILES


def format_process_output(stdout: str, stderr: str) -> str:
    """Provide focused helper behavior for the split module."""
    parts: list[str] = []
    if stdout.strip():
        parts.append(f"stdout:\n{stdout.strip()}")
    if stderr.strip():
        parts.append(f"stderr:\n{stderr.strip()}")
    return "\n\n".join(parts).strip()


def run_command_with_timeout(
    *,
    command: list[str],
    cwd: Path,
    timeout_seconds: int,
    target_name: str,
    engine: str,
) -> tuple[int, str, str]:
    """Provide focused helper behavior for the split module."""
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
        output = format_process_output(stdout or "", stderr or "")
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
    merge_engine_spec(engine)
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
        command = build_merge_command(
            engine=engine,
            binary=binary,
            temp_root=temp_root,
            prompt=prompt,
            model=request.model,
        )

        return_code, stdout, stderr = run_command_with_timeout(
            command=command,
            cwd=temp_root,
            timeout_seconds=request.timeout_seconds,
            target_name=request.target_name,
            engine=engine,
        )
        if return_code != 0:
            output = format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise MergeCommandFailedError(
                engine,
                request.target_name,
                return_code,
                details,
            )

        merged_path = temp_root / "merged.md"
        if not merged_path.exists():
            output = format_process_output(stdout, stderr)
            details = f"\n\n{output}" if output else ""
            raise MissingMergedOutputError(
                engine,
                request.target_name,
                details,
            )
        return merged_path.read_text(encoding="utf-8")
