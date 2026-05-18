# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Shared fixtures for CLI rendering workflow tests.

Responsibility is narrow.
Public imports stay aligned."""

import unittest
from pathlib import Path

import yaml

from tests.cli_render_assertions import _assert_rendered_targets
from tests.cli_render_injected_assertions import _assert_injected_root_files


class CliRenderSupport(unittest.TestCase):
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
        _assert_rendered_targets(repo_root)

    @staticmethod
    def _assert_injected_root_files(repo_root: Path) -> None:
        _assert_injected_root_files(repo_root)
