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


def test_runner_fails_hard_when_policy_bundle_is_missing(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_bundle = REPO_ROOT / "build" / "policy" / "policy-bundle.json"
    backup = policy_bundle.read_bytes()
    try:
        policy_bundle.unlink()
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
        policy_bundle.parent.mkdir(parents=True, exist_ok=True)
        policy_bundle.write_bytes(backup)

    output = result.stdout + result.stderr
    assert result.returncode != 0, output
    assert "missing compiled policy bundle" in output
    assert "run make build" in output
    assert ".git/coding-ethos-hooks" not in output
