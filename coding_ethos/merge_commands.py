# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""External merge engine command builders and registry.

Responsibility is narrow.
Public imports stay aligned.
"""

import shutil
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from coding_ethos.merge_errors import UnsupportedMergeEngineError


@dataclass(frozen=True, slots=True)
class MergeEngineSpec:
    """One supported external merge engine command contract."""

    name: str
    default_binary: str
    build_command: Callable[[str, Path, str, str], list[str]]


def build_codex_command(
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str,
) -> list[str]:
    """Provide focused helper behavior for the split module."""
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


def build_gemini_command(
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str,
) -> list[str]:
    """Provide focused helper behavior for the split module."""
    del temp_root
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


def build_claude_command(
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str,
) -> list[str]:
    """Provide focused helper behavior for the split module."""
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


_MERGE_ENGINE_SPECS: tuple[MergeEngineSpec, ...] = (
    MergeEngineSpec("codex", "codex", build_codex_command),
    MergeEngineSpec("gemini", "gemini", build_gemini_command),
    MergeEngineSpec("claude", "claude", build_claude_command),
)
SUPPORTED_MERGE_ENGINES = tuple(spec.name for spec in _MERGE_ENGINE_SPECS)


def merge_engine_spec(engine: str) -> MergeEngineSpec:
    """Provide focused helper behavior for the split module."""
    for spec in _MERGE_ENGINE_SPECS:
        if spec.name == engine:
            return spec
    raise UnsupportedMergeEngineError(engine)


def build_merge_command(
    *,
    engine: str,
    binary: str,
    temp_root: Path,
    prompt: str,
    model: str,
) -> list[str]:
    """Provide focused helper behavior for the split module."""
    spec = merge_engine_spec(engine)
    return spec.build_command(binary, temp_root, prompt, model)


def resolve_merge_bin(engine: str, explicit_bin: str = "") -> str:
    """Resolve the CLI binary used for a selected merge engine."""
    spec = merge_engine_spec(engine)
    if explicit_bin:
        return explicit_bin
    return shutil.which(spec.default_binary) or spec.default_binary


def resolve_codex_bin(explicit_bin: str = "") -> str:
    """Resolve the Codex CLI binary, preserving the legacy helper name."""
    return resolve_merge_bin("codex", explicit_bin)
