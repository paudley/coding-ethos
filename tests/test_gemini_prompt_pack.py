# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Tests for generated Gemini prompt packs."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

import yaml

from coding_ethos.cli import main
from coding_ethos.gemini_prompt_pack import (
    check_gemini_prompt_pack,
    render_gemini_prompt_pack,
    sync_gemini_prompt_pack,
)


def _write_primary(path: Path) -> None:
    path.write_text(
        yaml.safe_dump(
            {
                "version": 2,
                "metadata": {"title": "Test Ethos", "overview": "Shared overview."},
                "agents": {
                    "gemini": {
                        "notes": [
                            "Prefer targeted reads of the matching ethos detail docs."
                        ]
                    },
                },
                "principles": [
                    {
                        "id": "solid-is-law",
                        "order": 1,
                        "title": "SOLID is Law",
                        "summary": "Structure wins over convenience.",
                        "directive": "Enforce simple SOLID designs.",
                        "quick_ref": ["Favor explicit, non-speculative structure."],
                        "merge_topics": ["architecture"],
                        "tags": ["architecture"],
                        "related": [],
                        "agent_hints": {
                            "gemini": "Keep review output architectural and concrete."
                        },
                        "sections": [
                            {
                                "id": "overview",
                                "kind": "overview",
                                "title": "Overview",
                                "summary": "Structure wins over convenience.",
                                "body": "Long-form guidance.",
                            }
                        ],
                    },
                    {
                        "id": "testing-as-specification",
                        "order": 2,
                        "title": "Testing as Specification",
                        "summary": "Tests define expected behavior.",
                        "directive": "Treat tests as the behavioral contract.",
                        "quick_ref": ["Update tests when behavior changes."],
                        "merge_topics": ["testing"],
                        "tags": ["testing"],
                        "related": [],
                        "agent_hints": {
                            "gemini": "Anchor findings in observable behavior."
                        },
                        "sections": [
                            {
                                "id": "overview",
                                "kind": "overview",
                                "title": "Overview",
                                "summary": "Tests define expected behavior.",
                                "body": "More guidance.",
                            }
                        ],
                    },
                ],
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )


def _write_repo_ethos(path: Path) -> None:
    path.write_text(
        yaml.safe_dump(
            {
                "repo": {
                    "name": "Widget Service",
                    "overview": "Processes widgets.",
                    "commands": {
                        "test": ["uv run pytest"],
                        "lint": ["uv run ruff check ."],
                    },
                    "paths": {
                        "source": "src/widget_service",
                        "tests": "tests",
                    },
                    "notes": ["Widget IDs are immutable."],
                },
                "agent_notes": {
                    "gemini": [
                        "Prefer narrow, file-level reads before broad summaries."
                    ],
                },
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )


def _write_repo_config(path: Path) -> None:
    path.write_text(
        yaml.safe_dump(
            {
                "style": {"line_length": 100},
                "python": {
                    "source_paths": ["src/widget_service"],
                    "test_paths": ["tests", "integration/tests"],
                },
                "gemini": {
                    "modal_allowlist_files": [
                        "scripts/bootstrap.sh",
                        "scripts/legacy/*.sh",
                    ]
                },
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )


class GeminiPromptPackTests(unittest.TestCase):
    def test_render_prompt_pack_grounds_repo_identity_and_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "repo"
            repo_ethos_path = repo_root / "repo_ethos.yml"
            repo_config_path = repo_root / "repo_config.yaml"

            repo_root.mkdir()
            _write_primary(primary_path)
            _write_repo_ethos(repo_ethos_path)
            _write_repo_config(repo_config_path)

            payload = json.loads(
                render_gemini_prompt_pack(
                    repo_root=repo_root,
                    primary_path=primary_path,
                    repo_ethos_path=repo_ethos_path,
                    repo_config_path=repo_config_path,
                )
            )

            assert payload["project"]["name"] == "Widget Service"
            assert payload["project"]["context"] == "Processes widgets."
            assert payload["checks"]["code_ethos"]["file_scope"] == "code"
            assert payload["checks"]["code_ethos"]["batch_size"] == 3
            assert payload["checks"]["code_ethos"]["max_file_size_kb"] == 50
            assert payload["checks"]["code_ethos"]["selector"] == {
                "include_extensions": [
                    ".py",
                    ".pyi",
                    ".sh",
                    ".bash",
                    ".go",
                    ".rs",
                    ".ts",
                    ".js",
                ],
                "exclude_substrings": [
                    "test_",
                    "_test.",
                    ".test.",
                    "/tests/",
                    "/test/",
                    "/__pycache__/",
                    "/node_modules/",
                    "/vendor/",
                    "/.venv/",
                    "/venv/",
                    "/migrations/",
                ],
                "exclude_prefixes": [
                    ".venv/",
                    "venv/",
                    "__pycache__/",
                    "node_modules/",
                ],
                "allow_extensionless_in_scripts": False,
                "shebang_markers": ["python", "bash", "sh"],
            }
            assert (
                "Shared line length is 100 characters."
                in payload["grounding"]["enforcement_notes"]
            )
            assert (
                "Gemini modal-path allowlist: scripts/bootstrap.sh, scripts/legacy/*.sh."
                in payload["grounding"]["enforcement_notes"]
            )
            assert "Widget Service" in payload["prompts"]["code_ethos"]
            assert "Processes widgets." in payload["prompts"]["shell_review"]
            assert "Command `test`: uv run pytest" in payload["prompts"]["shell_review"]
            assert (
                "01. SOLID is Law: Enforce simple SOLID designs."
                in payload["prompts"]["code_ethos"]
            )
            assert "{code_content}" in payload["prompts"]["code_ethos"]

    def test_sync_and_check_prompt_pack(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "repo"
            repo_ethos_path = repo_root / "repo_ethos.yml"
            repo_config_path = repo_root / "repo_config.yaml"

            repo_root.mkdir()
            _write_primary(primary_path)
            _write_repo_ethos(repo_ethos_path)
            _write_repo_config(repo_config_path)

            written = sync_gemini_prompt_pack(
                repo_root=repo_root,
                primary_path=primary_path,
                repo_ethos_path=repo_ethos_path,
                repo_config_path=repo_config_path,
            )
            prompt_pack_path = repo_root / ".code-ethos/gemini/prompt-pack.json"

            assert written == [prompt_pack_path]
            assert (
                check_gemini_prompt_pack(
                    repo_root=repo_root,
                    primary_path=primary_path,
                    repo_ethos_path=repo_ethos_path,
                    repo_config_path=repo_config_path,
                )
                == []
            )

            prompt_pack_path.write_text('{"broken": true}\n', encoding="utf-8")

            assert check_gemini_prompt_pack(
                repo_root=repo_root,
                primary_path=primary_path,
                repo_ethos_path=repo_ethos_path,
                repo_config_path=repo_config_path,
            ) == [prompt_pack_path]


class GeminiPromptPackCliTests(unittest.TestCase):
    def test_cli_syncs_and_checks_prompt_pack(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "repo"
            repo_ethos_path = repo_root / "repo_ethos.yml"
            repo_config_path = repo_root / "repo_config.yaml"

            repo_root.mkdir()
            _write_primary(primary_path)
            _write_repo_ethos(repo_ethos_path)
            _write_repo_config(repo_config_path)

            exit_code = main(
                [
                    "--repo",
                    str(repo_root),
                    "--primary",
                    str(primary_path),
                    "--repo-ethos",
                    str(repo_ethos_path),
                    "--repo-config",
                    str(repo_config_path),
                    "--sync-gemini-prompts",
                ]
            )
            assert exit_code == 0
            assert (repo_root / ".code-ethos/gemini/prompt-pack.json").exists()

            exit_code = main(
                [
                    "--repo",
                    str(repo_root),
                    "--primary",
                    str(primary_path),
                    "--repo-ethos",
                    str(repo_ethos_path),
                    "--repo-config",
                    str(repo_config_path),
                    "--check-gemini-prompts",
                ]
            )
            assert exit_code == 0
