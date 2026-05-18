# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture runtime tests.

Responsibility is narrow.
Public imports stay aligned."""

import sys
from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    _prepare_consumer_repo,
    _run,
)


def test_policy_tool_mypy_uses_consumer_python_environment(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    package = consumer / "pkg"
    package.mkdir()
    (package / "app.py").write_text("x: int = 'bad'\n", encoding="utf-8")
    venv_bin = consumer / ".venv" / "bin"
    venv_bin.mkdir(parents=True)
    (venv_bin / "python").symlink_to(Path(sys.executable))

    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "mypy",
            "pkg/app.py",
        ],
        cwd=consumer,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "Incompatible types in assignment" in output

    trace_files = sorted((consumer / ".coding-ethos" / "lint-runs").glob("*.json"))
    assert trace_files
    trace_content = trace_files[-1].read_text(encoding="utf-8")
    assert "--python-executable" in trace_content
    assert str(venv_bin / "python") in trace_content
