# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture lifecycle tests.

Responsibility is narrow.
Public imports stay aligned."""

import os
import time
from pathlib import Path

from tests.lint_capture_support import (
    REPO_ROOT,
    RUNNER,
    _prepare_consumer_repo,
    _run,
    _sync_consumer_tool_configs,
)


def test_policy_tool_blocks_configured_lint_roots_that_escape_repo(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    (consumer / "repo_config.yml").write_text(
        """
version: 1
python:
  extra_paths:
    - ..
""".lstrip(),
        encoding="utf-8",
    )
    _sync_consumer_tool_configs(consumer)
    (consumer / "pkg").mkdir()
    (consumer / "pkg" / "app.py").write_text("VALUE = 1\n", encoding="utf-8")

    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "ruff",
            "check",
            "pkg/app.py",
        ],
        cwd=consumer,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 2, output
    assert "configured lint source root escapes repo: .." in output
    assert "tool: ruff" not in output


def test_validate_uses_policy_source_hashes_not_mtime(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_source = REPO_ROOT / "coding_ethos.yml"
    original_times = policy_source.stat()
    future = time.time() + 10
    try:
        os.utime(policy_source, (future, future))

        env = os.environ.copy()
        env["CODE_ETHOS_CONSUMER_ROOT"] = str(consumer)
        result = _run(
            [str(RUNNER), "git-hook", "validate"],
            cwd=REPO_ROOT,
            env=env,
            timeout=180,
        )
    finally:
        os.utime(policy_source, (original_times.st_atime, original_times.st_mtime))

    output = result.stdout + result.stderr
    assert "compiled policy bundle is older" not in output
    assert "hook runtime is not installed or is stale" not in output


def test_cutover_verify_resolves_consumer_without_env(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    env = os.environ.copy()
    env.pop("CODE_ETHOS_CONSUMER_ROOT", None)
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "cutover",
            "verify",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert f"repo: {consumer}" in output
    assert f"repo: {REPO_ROOT}" not in output


def test_git_hook_shell_shims_are_removed() -> None:
    assert not (REPO_ROOT / "pre-commit" / "hooks" / "run-git-hook.sh").exists()
    assert not (REPO_ROOT / "pre-commit" / "hooks" / "run-lfs-hook.sh").exists()
