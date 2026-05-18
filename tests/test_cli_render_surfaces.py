# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""CLI rendering and managed-block integration tests.

Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path

import pytest
import yaml

from coding_ethos import (
    SUPPORTED_MERGE_ENGINES,
    UnsupportedMergeEngineError,
    load_primary_bundle,
    render_agent_root_outputs,
    required_root_imports,
    resolve_merge_bin,
    root_merge_topics,
)
from tests.cli_render_support import CliRenderSupport


def _write_yaml_file(path: Path, payload: object) -> None:
    path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )


def test_registered_agent_root_surfaces_render_expected_outputs(tmp_path: Path) -> None:
    primary_path = tmp_path / "coding_ethos.yml"
    _write_yaml_file(
        primary_path,
        CliRenderSupport._primary_payload(include_testing_principle=False),
    )

    bundle = load_primary_bundle(primary_path)
    rendered = render_agent_root_outputs(bundle, tmp_path)

    assert set(rendered) == {"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
    assert required_root_imports("CLAUDE.md") == [
        "@AGENTS.md",
        "@.claude/ethos/MEMORY.md",
    ]
    assert required_root_imports("GEMINI.md") == ["@AGENTS.md"]
    assert root_merge_topics("AGENTS.md") == [
        "repo commands",
        "key paths",
        "repo operating notes",
    ]


def test_merge_engine_registry_resolves_supported_engines() -> None:
    assert SUPPORTED_MERGE_ENGINES == ("codex", "gemini", "claude")
    assert resolve_merge_bin("gemini", "/custom/gemini") == "/custom/gemini"
    with pytest.raises(UnsupportedMergeEngineError):
        resolve_merge_bin("unknown")
