# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""CLI merge-engine behavior tests.

Responsibility is narrow.
Public imports stay aligned."""

import tempfile
from pathlib import Path
from unittest.mock import patch

import yaml

from coding_ethos import (
    main,
)
from tests.cli_render_support import CliRenderSupport


def _write_yaml_file(path: Path, payload: object) -> None:
    path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )


class CliRenderTests(CliRenderSupport):
    def test_cli_replaces_existing_ethos_symlink_with_generated_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "target"

            primary_path.write_text(
                yaml.safe_dump(
                    {
                        "version": 2,
                        "metadata": {
                            "title": "Test Ethos",
                            "overview": "Shared overview.",
                        },
                        "principles": [
                            {
                                "id": "solid-is-law",
                                "order": 1,
                                "title": "SOLID is Law",
                                "summary": "Structure wins over convenience.",
                                "directive": "Enforce simple SOLID designs.",
                                "quick_ref": ["Favor simple designs."],
                                "merge_topics": ["architecture decisions"],
                                "tags": ["architecture"],
                                "related": [],
                                "agent_hints": {
                                    "codex": "Prefer structural refactors."
                                },
                                "sections": [
                                    {
                                        "id": "overview",
                                        "kind": "overview",
                                        "title": "Overview",
                                        "summary": "Structure wins over convenience.",
                                        "body": "Long form guidance.",
                                    }
                                ],
                            }
                        ],
                    },
                    sort_keys=False,
                ),
                encoding="utf-8",
            )

            repo_root.mkdir()
            legacy_target = repo_root / "legacy-ethos.md"
            legacy_target.write_text("legacy\n", encoding="utf-8")
            (repo_root / "ETHOS.md").symlink_to(legacy_target.name)

            exit_code = main(["--repo", str(repo_root), "--primary", str(primary_path)])

            assert exit_code == 0
            ethos_path = repo_root / "ETHOS.md"
            assert not ethos_path.is_symlink()
            assert "# Test Ethos" in ethos_path.read_text(encoding="utf-8")
            assert legacy_target.read_text(encoding="utf-8") == "legacy\n"

    def test_cli_merge_existing_llm_strategy_still_uses_merge_engine(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "target"

            primary_path.write_text(
                yaml.safe_dump(
                    {
                        "version": 2,
                        "metadata": {
                            "title": "Test Ethos",
                            "overview": "Shared overview.",
                        },
                        "principles": [
                            {
                                "id": "solid-is-law",
                                "order": 1,
                                "title": "SOLID is Law",
                                "summary": "Structure wins over convenience.",
                                "directive": "Enforce simple SOLID designs.",
                                "quick_ref": ["Favor simple designs."],
                                "merge_topics": ["architecture decisions"],
                                "tags": ["architecture"],
                                "related": [],
                                "agent_hints": {
                                    "codex": "Prefer structural refactors."
                                },
                                "sections": [
                                    {
                                        "id": "overview",
                                        "kind": "overview",
                                        "title": "Overview",
                                        "summary": "Structure wins over convenience.",
                                        "body": "Long form guidance.",
                                    }
                                ],
                            }
                        ],
                    },
                    sort_keys=False,
                ),
                encoding="utf-8",
            )

            repo_root.mkdir()
            (repo_root / "AGENTS.md").write_text(
                "# Existing agents\n\nKeep this.\n", encoding="utf-8"
            )

            with patch(
                "coding_ethos.cli_generation.merge_with_engine",
                return_value="# Merged agents\n\nKeep this and add ethos.\n",
            ) as merge_mock:
                exit_code = main(
                    [
                        "--repo",
                        str(repo_root),
                        "--primary",
                        str(primary_path),
                        "--merge-existing",
                        "--merge-strategy",
                        "llm",
                        "--merge-engine",
                        "gemini",
                        "--merge-bin",
                        "/fake/gemini",
                        "--merge-timeout-seconds",
                        "42",
                    ]
                )

            assert exit_code == 0
            assert (repo_root / "AGENTS.md").read_text(
                encoding="utf-8"
            ) == "# Merged agents\n\nKeep this and add ethos.\n"
            merge_mock.assert_called_once()
            assert merge_mock.call_args.kwargs["engine"] == "gemini"
            assert merge_mock.call_args.kwargs["binary"] == "/fake/gemini"
            request = merge_mock.call_args.kwargs["request"]
            assert request.target_name == "AGENTS.md"
            assert request.timeout_seconds == 42
            assert request.merge_topics[:3] == [
                "repo commands",
                "key paths",
                "repo operating notes",
            ]
