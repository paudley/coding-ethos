# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture runtime tests.

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


def test_policy_tool_blocks_generated_config_drift_before_linter_runs(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    nested = consumer / "pkg"
    nested.mkdir()
    (nested / "app.py").write_text("x: int = 'bad'\n", encoding="utf-8")
    (consumer / "mypy.ini").write_text(
        "[mypy]\nignore_errors = True\n", encoding="utf-8"
    )

    fake_bin = tmp_path / "fake-bin"
    _write_poisoned_bin(fake_bin, "mypy")

    env = os.environ.copy()
    env["PATH"] = f"{fake_bin}:{env['PATH']}"
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "mypy",
            "pkg/app.py",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 2, output
    assert "PWNED" not in output
    assert "GENERATED TOOL CONFIG DRIFT" in output
    assert "mypy.ini" in output
    assert str(consumer) not in output
    assert "make -C coding-ethos fix-configs" in output
