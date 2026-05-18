# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture runtime tests.

Responsibility is narrow.
Public imports stay aligned."""

from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    _prepare_consumer_repo,
    _run,
)


def test_policy_lint_runs_without_legacy_lint_binary(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    lint_tool = REPO_ROOT / "bin" / "coding-ethos-lint"
    backup = lint_tool.read_bytes()
    mode = lint_tool.stat().st_mode
    try:
        lint_tool.unlink()
        result = _run(
            [
                str(REPO_ROOT / "bin" / "coding-ethos-run"),
                "policy-lint",
                "--help",
            ],
            cwd=consumer,
            check=False,
            timeout=180,
        )
    finally:
        lint_tool.write_bytes(backup)
        lint_tool.chmod(mode)

    output = result.stdout + result.stderr
    assert result.returncode == 0, output
    assert "missing or non-executable coding-ethos-lint" not in output
    assert ".git/coding-ethos-hooks" not in output
