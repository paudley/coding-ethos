# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture policy tool tests.

Responsibility is narrow.
Public imports stay aligned."""

import os
from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    _prepare_consumer_repo,
    _run,
    _write_poisoned_bin,
)


def test_policy_tool_ruff_uses_managed_tool_and_normalizes_paths(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    nested = consumer / "lbox-platform" / "lib" / "python"
    package = nested / "lbox" / "parsing"
    package.mkdir(parents=True)
    target = package / "analyzer_base.py"
    target.write_text("import os\n\nVALUE = 1\n", encoding="utf-8")

    (nested / "pyproject.toml").write_text(
        "[tool.ruff.lint]\nignore = ['F401']\n",
        encoding="utf-8",
    )
    fake_bin = tmp_path / "fake-bin"
    _write_poisoned_bin(fake_bin, "ruff")

    env = os.environ.copy()
    env["PATH"] = f"{fake_bin}:{env['PATH']}"
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "ruff",
            "check",
            "lbox/parsing/analyzer_base.py",
        ],
        cwd=nested,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "PWNED" not in output
    assert "tool: ruff" in output
    assert "status: FAIL" in output
    assert "F401" in output
    assert "lbox-platform/lib/python/lbox/parsing/analyzer_base.py" in output
    assert str(consumer) not in output
    assert "findings[0]" not in output
