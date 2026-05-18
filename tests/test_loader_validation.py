# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Smoke coverage for the public CLI entry points.

Responsibility is narrow.
Public imports stay aligned."""

import tempfile
import unittest
from pathlib import Path

import pytest
import yaml

from coding_ethos import (
    load_primary_bundle,
    merge_repo_ethos,
)


def _write_yaml_file(path: Path, payload: object) -> None:
    path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
    )


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
