# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""CLI rendering and managed-block integration tests.

Responsibility is narrow.
Public imports stay aligned."""

import tempfile
from pathlib import Path

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
