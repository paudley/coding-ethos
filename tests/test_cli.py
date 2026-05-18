# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Smoke coverage for the public CLI entry points.

Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path

import pytest
import yaml

from coding_ethos import (
    main,
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
