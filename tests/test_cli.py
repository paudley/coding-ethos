# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

from coding_ethos import (
    load_primary_bundle,
    main,
    parse_ethos_markdown,
    seed_primary_from_markdown,
)


class MarkdownSeedTests(unittest.TestCase):
    def test_parse_ethos_markdown_extracts_principles_and_subsections(self) -> None:
        markdown = """# **Sample Ethos**

Intro text.

## Table of Contents

## **1. First Rule**

Do the first thing. Always.

### **Why**

Because correctness matters.

## **2. Second Rule**

**Never** do the wrong thing.
"""
        payload = parse_ethos_markdown(markdown)
        self.assertEqual(payload["metadata"]["title"], "Sample Ethos")
        self.assertEqual(len(payload["principles"]), 2)
        self.assertEqual(payload["principles"][0]["id"], "first-rule")
        self.assertEqual(payload["principles"][0]["sections"][0]["title"], "Overview")
        self.assertEqual(payload["principles"][0]["sections"][0]["kind"], "overview")
        self.assertEqual(payload["principles"][0]["sections"][1]["title"], "Why")
        self.assertEqual(payload["principles"][0]["sections"][1]["kind"], "rationale")
        self.assertTrue(payload["principles"][0]["quick_ref"])
        self.assertTrue(payload["principles"][0]["merge_topics"])
        self.assertIn("codex", payload["principles"][0]["agent_hints"])
        self.assertEqual(
            payload["principles"][1]["summary"], "Never do the wrong thing."
        )

    def test_seed_primary_keeps_section_bodies_inline(self) -> None:
        markdown = """# **Sample Ethos**

Intro text.

## Table of Contents

## **1. First Rule**

Do the first thing. Always.
"""
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            source_path = tmp_path / "ETHOS.md"
            primary_path = tmp_path / "coding_ethos.yml"
            source_path.write_text(markdown, encoding="utf-8")

            seed_primary_from_markdown(source_path, primary_path)
            payload = yaml.safe_load(primary_path.read_text(encoding="utf-8"))

            self.assertEqual(payload["version"], 2)
            self.assertIn("agents", payload)
            self.assertEqual(payload["metadata"]["source_markdown"], "ETHOS.md")
            self.assertEqual(
                payload["principles"][0]["sections"][0]["kind"], "overview"
            )
            self.assertEqual(
                payload["principles"][0]["sections"][0]["body"],
                "Do the first thing. Always.",
            )
            self.assertFalse((tmp_path / "ethos").exists())


class LoaderValidationTests(unittest.TestCase):
    def test_load_primary_bundle_rejects_duplicate_orders(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
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
                                "id": "first",
                                "order": 1,
                                "title": "First",
                                "summary": "First summary.",
                                "directive": "First directive.",
                                "sections": [
                                    {
                                        "id": "overview",
                                        "kind": "overview",
                                        "title": "Overview",
                                        "summary": "First summary.",
                                        "body": "First body.",
                                    }
                                ],
                            },
                            {
                                "id": "second",
                                "order": 1,
                                "title": "Second",
                                "summary": "Second summary.",
                                "directive": "Second directive.",
                                "sections": [
                                    {
                                        "id": "overview",
                                        "kind": "overview",
                                        "title": "Overview",
                                        "summary": "Second summary.",
                                        "body": "Second body.",
                                    }
                                ],
                            },
                        ],
                    },
                    sort_keys=False,
                ),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "duplicate principle order"):
                load_primary_bundle(primary_path)


class CliRenderTests(unittest.TestCase):
    def test_cli_renders_all_supported_targets(self) -> None:
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
                        "agents": {
                            "claude": {
                                "notes": ["Use CLAUDE.md as a short import hub."]
                            },
                        },
                        "principles": [
                            {
                                "id": "solid-is-law",
                                "order": 1,
                                "title": "SOLID is Law",
                                "summary": "Structure wins over convenience.",
                                "directive": "Enforce simple SOLID designs.",
                                "quick_ref": [
                                    "Favor simple, explicit designs.",
                                    "Remove speculative abstractions.",
                                ],
                                "merge_topics": [
                                    "architecture decisions",
                                    "design constraints",
                                ],
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
                            },
                            {
                                "id": "testing-as-specification",
                                "order": 2,
                                "title": "Testing as Specification",
                                "summary": "Tests define expected behavior.",
                                "directive": "Treat tests as the behavioral contract.",
                                "quick_ref": ["Update tests with code changes."],
                                "merge_topics": [
                                    "test requirements",
                                    "behavioral specification",
                                ],
                                "tags": ["testing"],
                                "related": [],
                                "agent_hints": {"codex": "Keep tests aligned."},
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

            repo_root.mkdir()
            (repo_root / "repo_ethos.yaml").write_text(
                yaml.safe_dump(
                    {
                        "repo": {
                            "name": "Widget Service",
                            "overview": "Processes widgets.",
                            "commands": {"test": ["uv run pytest"]},
                            "paths": {"source": "src/", "tests": "tests/"},
                            "notes": ["Widget IDs are immutable."],
                        },
                        "agent_notes": {
                            "claude": [
                                "Open the matching ethos doc before changing API contracts."
                            ],
                            "gemini": [
                                "Prefer targeted reads when the task is narrow."
                            ],
                        },
                        "principles": {
                            "overrides": {
                                "solid-is-law": {
                                    "append": "Repo addendum: prefer service objects for integrations."
                                }
                            }
                        },
                    },
                    sort_keys=False,
                ),
                encoding="utf-8",
            )

            exit_code = main(["--repo", str(repo_root), "--primary", str(primary_path)])
            self.assertEqual(exit_code, 0)

            agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
            claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
            ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
            gemini_md = (repo_root / "GEMINI.md").read_text(encoding="utf-8")
            detail_doc = (repo_root / ".agents/ethos/solid-is-law.md").read_text(
                encoding="utf-8"
            )
            memory_index = (repo_root / ".claude/ethos/MEMORY.md").read_text(
                encoding="utf-8"
            )

            self.assertIn("Widget Service", agents_md)
            self.assertIn("Processes widgets.", agents_md)
            self.assertIn("Enforce simple SOLID designs.", agents_md)
            self.assertIn("Quick ref:", agents_md)
            self.assertIn("# Test Ethos", ethos_md)
            self.assertIn("## Repo Context", ethos_md)
            self.assertIn("## 01. SOLID is Law", ethos_md)
            self.assertIn("### Directive", ethos_md)
            self.assertIn("@AGENTS.md", claude_md)
            self.assertIn("Open the matching ethos doc", claude_md)
            self.assertIn("@AGENTS.md", gemini_md)
            self.assertIn("## Overview", detail_doc)
            self.assertIn("## Quick Ref", detail_doc)
            self.assertIn("## Merge Topics", detail_doc)
            self.assertIn("## Agent Hints", detail_doc)
            self.assertIn("## Repo Addendum", detail_doc)
            self.assertIn("../../.agents/ethos/solid-is-law.md", memory_index)
            self.assertTrue(
                (repo_root / ".agent-context/prompt-addons/codex.md").exists()
            )

    def test_cli_merge_existing_injects_managed_blocks_for_root_files(self) -> None:
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
            (repo_root / "ETHOS.md").write_text(
                "# Old ethos\n\nStale content.\n", encoding="utf-8"
            )
            (repo_root / "AGENTS.md").write_text(
                "# Existing agents\n\nKeep this.\n", encoding="utf-8"
            )
            (repo_root / "CLAUDE.md").write_text(
                "# Existing claude\n\nLocal workflow notes.\n", encoding="utf-8"
            )

            exit_code = main(
                [
                    "--repo",
                    str(repo_root),
                    "--primary",
                    str(primary_path),
                    "--merge-existing",
                ]
            )

            self.assertEqual(exit_code, 0)
            agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
            claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
            ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
            self.assertIn("# Existing agents", agents_md)
            self.assertIn("Keep this.", agents_md)
            self.assertIn("<!-- coding-ethos:begin managed AGENTS.md -->", agents_md)
            self.assertIn("## Coding Ethos", agents_md)
            self.assertIn(".agents/ethos/README.md", agents_md)
            self.assertIn("# Existing claude", claude_md)
            self.assertIn("Local workflow notes.", claude_md)
            self.assertIn("<!-- coding-ethos:begin imports CLAUDE.md -->", claude_md)
            self.assertIn("@AGENTS.md", claude_md)
            self.assertIn("@.claude/ethos/MEMORY.md", claude_md)
            self.assertIn("<!-- coding-ethos:begin managed CLAUDE.md -->", claude_md)
            self.assertNotIn("Stale content.", ethos_md)
            self.assertIn("# Test Ethos", ethos_md)
            self.assertIn("## 01. SOLID is Law", ethos_md)
            self.assertTrue((repo_root / ".agents/ethos/solid-is-law.md").exists())

            exit_code = main(
                [
                    "--repo",
                    str(repo_root),
                    "--primary",
                    str(primary_path),
                    "--merge-existing",
                ]
            )

            self.assertEqual(exit_code, 0)
            rerun_agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
            rerun_claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
            self.assertEqual(
                rerun_agents_md.count("<!-- coding-ethos:begin managed AGENTS.md -->"),
                1,
            )
            self.assertEqual(
                rerun_claude_md.count("<!-- coding-ethos:begin imports CLAUDE.md -->"),
                1,
            )
            self.assertEqual(
                rerun_claude_md.count("<!-- coding-ethos:begin managed CLAUDE.md -->"),
                1,
            )

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

            self.assertEqual(exit_code, 0)
            ethos_path = repo_root / "ETHOS.md"
            self.assertFalse(ethos_path.is_symlink())
            self.assertIn("# Test Ethos", ethos_path.read_text(encoding="utf-8"))
            self.assertEqual(legacy_target.read_text(encoding="utf-8"), "legacy\n")

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
                "coding_ethos.cli.merge_with_engine",
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

            self.assertEqual(exit_code, 0)
            self.assertEqual(
                (repo_root / "AGENTS.md").read_text(encoding="utf-8"),
                "# Merged agents\n\nKeep this and add ethos.\n",
            )
            merge_mock.assert_called_once()
            self.assertEqual(merge_mock.call_args.kwargs["engine"], "gemini")
            self.assertEqual(merge_mock.call_args.kwargs["target_name"], "AGENTS.md")
            self.assertEqual(merge_mock.call_args.kwargs["binary"], "/fake/gemini")
            self.assertEqual(merge_mock.call_args.kwargs["timeout_seconds"], 42)
            self.assertEqual(
                merge_mock.call_args.kwargs["merge_topics"][:3],
                ["repo commands", "key paths", "repo operating notes"],
            )

    def test_cli_sync_tool_configs_generates_repo_root_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            repo_root = Path(tmp_dir)
            (repo_root / "repo_config.yaml").write_text(
                yaml.safe_dump(
                    {
                        "style": {"line_length": 100},
                        "python": {
                            "source_paths": ["lib/python/lbox", "pre-commit/hooks"],
                            "test_paths": ["lib/python/tests", "integration/tests"],
                            "stub_paths": ["lib/python/stubs"],
                            "extra_paths": [
                                "lib/python",
                                "scripts",
                                "pre-commit/hooks",
                            ],
                            "venv_path": "..",
                            "venv": ".venv",
                            "sql_centralization": {
                                "enabled": True,
                                "central_paths": ["lib/python/lbox/sql.py"],
                            },
                        },
                    },
                    sort_keys=False,
                ),
                encoding="utf-8",
            )

            exit_code = main(["--repo", str(repo_root), "--sync-tool-configs"])
            self.assertEqual(exit_code, 0)

            pyright = yaml.safe_load(
                (repo_root / "pyrightconfig.json").read_text(encoding="utf-8")
            )
            mypy_ini = (repo_root / "mypy.ini").read_text(encoding="utf-8")
            ruff_toml = (repo_root / "ruff.toml").read_text(encoding="utf-8")
            yamllint = yaml.safe_load(
                (repo_root / ".yamllint.yml").read_text(encoding="utf-8")
            )

            self.assertEqual(
                pyright["include"], ["lib/python/lbox", "pre-commit/hooks"]
            )
            self.assertEqual(pyright["stubPath"], "lib/python/stubs")
            self.assertEqual(
                pyright["extraPaths"], ["lib/python", "scripts", "pre-commit/hooks"]
            )
            self.assertEqual(pyright["venvPath"], "..")
            self.assertIn("files = lib/python/lbox, pre-commit/hooks", mypy_ini)
            self.assertIn("mypy_path = lib/python/stubs", mypy_ini)
            self.assertIn("line-length = 100", ruff_toml)
            self.assertIn('"lib/python/tests/**"', ruff_toml)
            self.assertIn('"integration/tests/**"', ruff_toml)
            self.assertIn('"lib/python/lbox/sql.py" = ["S608"]', ruff_toml)
            self.assertEqual(yamllint["rules"]["line-length"]["max"], 100)

    def test_cli_check_tool_configs_detects_out_of_sync_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            repo_root = Path(tmp_dir)
            exit_code = main(["--repo", str(repo_root), "--sync-tool-configs"])
            self.assertEqual(exit_code, 0)

            (repo_root / "ruff.toml").write_text(
                "line-length = 120\n", encoding="utf-8"
            )

            drift_exit_code = main(["--repo", str(repo_root), "--check-tool-configs"])
            self.assertEqual(drift_exit_code, 1)


if __name__ == "__main__":
    unittest.main()
