# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Smoke coverage for the public CLI entry points.

Responsibility is narrow.
Public imports stay aligned."""

import tempfile
import unittest
from pathlib import Path

import yaml

from coding_ethos import (
    parse_ethos_markdown,
    seed_primary_from_markdown,
)


def _write_yaml_file(path: Path, payload: object) -> None:
    path.write_text(
        yaml.safe_dump(payload, sort_keys=False),
        encoding="utf-8",
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
