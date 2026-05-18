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


def test_runner_fails_hard_when_checkout_local_binary_is_missing(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_tool = REPO_ROOT / "bin" / "coding-ethos-policy"
    backup = policy_tool.read_bytes()
    mode = policy_tool.stat().st_mode
    try:
        policy_tool.unlink()
        result = _run(
            [
                str(REPO_ROOT / "bin" / "coding-ethos-run"),
                "policy",
                "validate",
                "--bundle",
                str(REPO_ROOT / "build" / "policy" / "policy-bundle.json"),
            ],
            cwd=consumer,
            check=False,
            timeout=180,
        )
    finally:
        policy_tool.write_bytes(backup)
        policy_tool.chmod(mode)

    output = result.stdout + result.stderr
    assert result.returncode == 0, output
    assert "policy bundle valid" in output
    assert "missing or non-executable coding-ethos-policy" not in output
    assert ".git/coding-ethos-hooks" not in output
