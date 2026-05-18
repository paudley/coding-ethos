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
