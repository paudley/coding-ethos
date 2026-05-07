# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""CLI integration tests for coding-ethos generation workflows.

These tests exercise the public command surface rather than private helpers.
They verify that generated files, tool configs, and merge behavior stay aligned.
"""

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import pytest
import yaml

from coding_ethos import (
    SUPPORTED_MERGE_ENGINES,
    UnsupportedMergeEngineError,
    load_primary_bundle,
    main,
    merge_repo_ethos,
    parse_ethos_markdown,
    render_agent_root_outputs,
    required_root_imports,
    resolve_merge_bin,
    root_merge_topics,
    seed_primary_from_markdown,
)


def _write_yaml_file(path: Path, payload: object) -> None:
    path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )


def test_package_defaults_do_not_require_source_checkout(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.chdir(tmp_path)

    generated_repo = tmp_path / "generated"
    assert main(["--repo", str(generated_repo)]) == 0
    assert "SOLID is Law" in (generated_repo / "ETHOS.md").read_text(encoding="utf-8")


def test_seed_from_markdown_defaults_to_working_directory(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    markdown = """# **Seeded Ethos**

Intro text.

## **1. First Rule**

Do the first thing.
"""
    monkeypatch.chdir(tmp_path)
    source_path = tmp_path / "ETHOS.md"
    source_path.write_text(markdown, encoding="utf-8")

    generated_repo = tmp_path / "generated"
    assert (
        main(
            [
                "--repo",
                str(generated_repo),
                "--seed-from-markdown",
                str(source_path),
            ]
        )
        == 0
    )

    primary_path = tmp_path / "coding_ethos.yml"
    assert primary_path.exists()
    assert "Seeded Ethos" in primary_path.read_text(encoding="utf-8")
    assert "Seeded Ethos" in (generated_repo / "ETHOS.md").read_text(encoding="utf-8")


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
        assert payload["metadata"]["title"] == "Sample Ethos"
        assert len(payload["principles"]) == 2
        assert payload["principles"][0]["id"] == "first-rule"
        assert payload["principles"][0]["sections"][0]["title"] == "Overview"
        assert payload["principles"][0]["sections"][0]["kind"] == "overview"
        assert payload["principles"][0]["sections"][1]["title"] == "Why"
        assert payload["principles"][0]["sections"][1]["kind"] == "rationale"
        assert payload["principles"][0]["quick_ref"]
        assert payload["principles"][0]["merge_topics"]
        assert "codex" in payload["principles"][0]["agent_hints"]
        assert payload["principles"][1]["summary"] == "Never do the wrong thing."

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

            assert payload["version"] == 2
            assert "agents" in payload
            assert payload["metadata"]["source_markdown"] == "ETHOS.md"
            assert payload["principles"][0]["sections"][0]["kind"] == "overview"
            assert (
                payload["principles"][0]["sections"][0]["body"]
                == "Do the first thing. Always."
            )
            assert not (tmp_path / "ethos").exists()


class LoaderValidationTests(unittest.TestCase):
    @staticmethod
    def _valid_primary_payload() -> dict[str, object]:
        return {
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
                    "quick_ref": ["Favor simple, explicit designs."],
                    "merge_topics": ["architecture"],
                    "sections": [
                        {
                            "id": "overview",
                            "kind": "overview",
                            "title": "Overview",
                            "summary": "Structure wins.",
                            "body": "Keep designs simple and explicit.",
                        }
                    ],
                }
            ],
        }

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

            with pytest.raises(ValueError, match="duplicate principle order"):
                load_primary_bundle(primary_path)

    def test_repo_overlay_revalidates_principle_summary(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_ethos_path = tmp_path / "repo_ethos.yml"
            _write_yaml_file(primary_path, self._valid_primary_payload())
            _write_yaml_file(
                repo_ethos_path,
                {
                    "repo": {"name": "test-repo"},
                    "principles": {
                        "overrides": {
                            "solid-is-law": {
                                "summary": "   ",
                            }
                        }
                    },
                },
            )

            with pytest.raises(
                ValueError,
                match="principle `solid-is-law` must define a non-empty `summary`",
            ):
                merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)

    def test_repo_overlay_revalidates_principle_directive(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_ethos_path = tmp_path / "repo_ethos.yml"
            _write_yaml_file(primary_path, self._valid_primary_payload())
            _write_yaml_file(
                repo_ethos_path,
                {
                    "repo": {"name": "test-repo"},
                    "principles": {
                        "overrides": {
                            "solid-is-law": {
                                "directive": "",
                            }
                        }
                    },
                },
            )

            with pytest.raises(
                ValueError,
                match="principle `solid-is-law` must define a non-empty `directive`",
            ):
                merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)


def test_registered_agent_root_surfaces_render_expected_outputs(tmp_path: Path) -> None:
    primary_path = tmp_path / "coding_ethos.yml"
    _write_yaml_file(primary_path, LoaderValidationTests._valid_primary_payload())

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


class CliRenderTests(unittest.TestCase):
    @staticmethod
    def _write_yaml(path: Path, payload: dict[str, object]) -> None:
        path.write_text(
            yaml.safe_dump(payload, sort_keys=False),
            encoding="utf-8",
        )

    @staticmethod
    def _primary_payload(*, include_testing_principle: bool) -> dict[str, object]:
        principles: list[dict[str, object]] = [
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
                "agent_hints": {"codex": "Prefer structural refactors."},
                "sections": [
                    {
                        "id": "overview",
                        "kind": "overview",
                        "title": "Overview",
                        "summary": "Structure wins over convenience.",
                        "body": (
                            "Long form guidance. See [Section 2: Testing as "
                            "Specification](#2-testing-as-specification) and "
                            "Section 99: Missing Principle."
                        ),
                    }
                ],
            }
        ]
        if include_testing_principle:
            principles.append(
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
                }
            )

        payload: dict[str, object] = {
            "version": 2,
            "metadata": {
                "title": "Test Ethos",
                "overview": "Shared overview.",
            },
            "principles": principles,
        }
        if include_testing_principle:
            payload["agents"] = {
                "claude": {"notes": ["Use CLAUDE.md as a short import hub."]}
            }
        return payload

    @staticmethod
    def _repo_ethos_payload() -> dict[str, object]:
        return {
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
                "gemini": ["Prefer targeted reads when the task is narrow."],
            },
            "principles": {
                "overrides": {
                    "solid-is-law": {
                        "append": (
                            "Repo addendum: prefer service objects for integrations."
                        )
                    }
                }
            },
        }

    @staticmethod
    def _assert_rendered_targets(repo_root: Path) -> None:
        CliRenderTests._assert_rendered_root_files(repo_root)
        CliRenderTests._assert_rendered_detail_docs(repo_root)
        assert (repo_root / ".agent-context/prompt-addons/codex.md").exists()

    @staticmethod
    def _assert_rendered_root_files(repo_root: Path) -> None:
        agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
        claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
        ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
        gemini_md = (repo_root / "GEMINI.md").read_text(encoding="utf-8")
        assert "Widget Service" in agents_md
        assert "Processes widgets." in agents_md
        assert "Enforce simple SOLID designs." in agents_md
        assert "Quick ref:" in agents_md
        assert "# Test Ethos" in ethos_md
        assert "## Repo Context" in ethos_md
        assert "## 01. SOLID is Law" in ethos_md
        assert "### Directive" in ethos_md
        assert "@AGENTS.md" in claude_md
        assert "Open the matching ethos doc" in claude_md
        assert "@AGENTS.md" in gemini_md

    @staticmethod
    def _assert_rendered_detail_docs(repo_root: Path) -> None:
        detail_doc = (repo_root / ".agents/ethos/solid-is-law.md").read_text(
            encoding="utf-8"
        )
        memory_index = (repo_root / ".claude/ethos/MEMORY.md").read_text(
            encoding="utf-8"
        )
        assert "## Overview" in detail_doc
        assert "## Quick Ref" in detail_doc
        assert "## Merge Topics" in detail_doc
        assert "## Agent Hints" in detail_doc
        assert "## Repo Addendum" in detail_doc
        assert "../../.agents/ethos/solid-is-law.md" in memory_index

    @staticmethod
    def _assert_injected_root_files(repo_root: Path) -> None:
        CliRenderTests._assert_injected_agents_md(repo_root)
        CliRenderTests._assert_injected_claude_md(repo_root)
        CliRenderTests._assert_injected_ethos_md(repo_root)
        assert (repo_root / ".agents/ethos/solid-is-law.md").exists()

    @staticmethod
    def _assert_injected_agents_md(repo_root: Path) -> None:
        agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
        assert "# Existing agents" in agents_md
        assert "Keep this." in agents_md
        assert "<!-- coding-ethos:begin managed AGENTS.md -->" in agents_md
        assert "## Coding Ethos" in agents_md
        assert ".agents/ethos/README.md" in agents_md

    @staticmethod
    def _assert_injected_claude_md(repo_root: Path) -> None:
        claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
        assert "# Existing claude" in claude_md
        assert "Local workflow notes." in claude_md
        assert "<!-- coding-ethos:begin imports CLAUDE.md -->" in claude_md
        assert "@AGENTS.md" in claude_md
        assert "@.claude/ethos/MEMORY.md" in claude_md
        assert "<!-- coding-ethos:begin managed CLAUDE.md -->" in claude_md

    @staticmethod
    def _assert_injected_ethos_md(repo_root: Path) -> None:
        ethos_md = (repo_root / "ETHOS.md").read_text(encoding="utf-8")
        assert "Stale content." not in ethos_md
        assert "# Test Ethos" in ethos_md
        assert "## 01. SOLID is Law" in ethos_md

    def test_cli_renders_all_supported_targets(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "target"
            self._write_yaml(
                primary_path,
                self._primary_payload(include_testing_principle=True),
            )

            repo_root.mkdir()
            self._write_yaml(
                repo_root / "repo_ethos.yaml",
                self._repo_ethos_payload(),
            )

            exit_code = main(["--repo", str(repo_root), "--primary", str(primary_path)])
            assert exit_code == 0
            self._assert_rendered_targets(repo_root)

    def test_cli_does_not_render_go_owned_skill_surfaces(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "target"
            payload = self._primary_payload(include_testing_principle=True)
            payload["skills"] = [
                {
                    "id": "lint-remediation",
                    "title": "Lint Remediation",
                    "description": "Use when lint findings need structural fixes.",
                    "principle_ids": ["solid-is-law", "testing-as-specification"],
                    "trigger_terms": ["ruff", "mypy"],
                    "short_hint": "Fix structurally.",
                    "focus": "Use this skill for lint failures.",
                    "remediation_steps": ["Classify the finding.", "Fix the code."],
                }
            ]
            self._write_yaml(primary_path, payload)

            repo_root.mkdir()
            self._write_yaml(
                repo_root / "repo_ethos.yml",
                {
                    "repo": {
                        "name": 'Widget "Service" \\ Alpha',
                        "overview": "Processes widgets.",
                    }
                },
            )
            exit_code = main(["--repo", str(repo_root), "--primary", str(primary_path)])

            assert exit_code == 0
            assert (repo_root / "AGENTS.md").exists()
            for relative_path in [
                ".agents/skills/lint-remediation/SKILL.md",
                ".claude/skills/lint-remediation/SKILL.md",
                ".codex/skills/lint-remediation/SKILL.md",
                ".gemini/extensions/coding-ethos/skills/lint-remediation/SKILL.md",
                ".gemini/extensions/coding-ethos/gemini-extension.json",
            ]:
                assert not (repo_root / relative_path).exists(), relative_path

    def test_cli_merge_existing_injects_managed_blocks_for_root_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_dir:
            tmp_path = Path(tmp_dir)
            primary_path = tmp_path / "coding_ethos.yml"
            repo_root = tmp_path / "target"
            self._write_yaml(
                primary_path,
                self._primary_payload(include_testing_principle=False),
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

            assert exit_code == 0
            self._assert_injected_root_files(repo_root)

            exit_code = main(
                [
                    "--repo",
                    str(repo_root),
                    "--primary",
                    str(primary_path),
                    "--merge-existing",
                ]
            )

            assert exit_code == 0
            rerun_agents_md = (repo_root / "AGENTS.md").read_text(encoding="utf-8")
            rerun_claude_md = (repo_root / "CLAUDE.md").read_text(encoding="utf-8")
            assert (
                rerun_agents_md.count("<!-- coding-ethos:begin managed AGENTS.md -->")
                == 1
            )
            assert (
                rerun_claude_md.count("<!-- coding-ethos:begin imports CLAUDE.md -->")
                == 1
            )
            assert (
                rerun_claude_md.count("<!-- coding-ethos:begin managed CLAUDE.md -->")
                == 1
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


if __name__ == "__main__":
    unittest.main()
