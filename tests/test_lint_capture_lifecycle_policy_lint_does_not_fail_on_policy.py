# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture runtime tests.

Responsibility is narrow.
Public imports stay aligned."""

import os
import time
from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    _prepare_consumer_repo,
    _run,
)


def test_lifecycle_policy_lint_does_not_fail_on_policy_mtime_drift(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_source = REPO_ROOT / "coding_ethos.yml"
    original_times = policy_source.stat()
    future = time.time() + 10
    try:
        os.utime(policy_source, (future, future))
        result = _run(
            [
                str(REPO_ROOT / "bin" / "coding-ethos-run"),
                "policy-lint",
                "--help",
            ],
            cwd=consumer,
            timeout=180,
        )
    finally:
        os.utime(policy_source, (original_times.st_atime, original_times.st_mtime))

    output = result.stdout + result.stderr
    assert result.returncode == 0, output
    assert "compiled policy bundle is older" not in output
    assert "hook runtime is not installed or is stale" not in output
