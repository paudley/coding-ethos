# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Shared subprocess fixtures for managed lint capture tests.

The helpers build temporary consumer repositories with poisoned PATH entries, drifted generated configs, and checkout-local runtime artifacts.


Responsibility is narrow.
Public imports stay aligned."""

import os
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
REAL_GIT = "/usr/bin/git"
RUNNER = REPO_ROOT / "bin" / "coding-ethos-run"
POLICY = REPO_ROOT / "bin" / "coding-ethos-policy"


def _clean_subprocess_env(env: dict[str, str] | None) -> dict[str, str]:
    clean = dict(os.environ if env is None else env)
    for name in list(clean):
        if name.startswith("GIT_"):
            clean.pop(name, None)
    return clean


def _run(
    args: list[str],
    *,
    cwd: Path,
    env: dict[str, str] | None = None,
    timeout: int = 120,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    command = [REAL_GIT, *args[1:]] if args and args[0] == "git" else args
    result = subprocess.run(
        command,
        cwd=cwd,
        env=_clean_subprocess_env(env),
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    if check and result.returncode != 0:
        raise AssertionError(
            f"command failed with {result.returncode}: {args}\n"
            f"stdout:\n{result.stdout}\n"
            f"stderr:\n{result.stderr}"
        )
    return result


def _prepare_consumer_repo(tmp_path: Path) -> Path:
    _assert_managed_runtime_exists()
    consumer = tmp_path / "consumer"
    consumer.mkdir()
    _run(["git", "init"], cwd=consumer)
    (consumer / ".gitignore").write_text(".coding-ethos/\n", encoding="utf-8")
    _run(["git", "add", ".gitignore"], cwd=consumer)
    _sync_consumer_tool_configs(consumer)
    _sync_consumer_policy_bundle(consumer)
    return consumer


def _assert_managed_runtime_exists() -> None:
    missing = [
        str(path)
        for path in (
            RUNNER,
            POLICY,
            REPO_ROOT / "build" / "policy" / "policy-bundle.json",
        )
        if not path.exists()
    ]
    if missing:
        raise AssertionError(
            "managed runtime is missing; run `make build` before integration tests: "
            + ", ".join(missing)
        )


def _sync_consumer_tool_configs(consumer: Path) -> None:
    _run(
        [
            str(POLICY),
            "sync-tool-configs",
            "--ethos-root",
            str(REPO_ROOT),
            "--repo",
            str(consumer),
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )


def _sync_consumer_policy_bundle(consumer: Path) -> None:
    _run(
        [
            str(POLICY),
            "compile",
            "--primary",
            str(REPO_ROOT / "coding_ethos.yml"),
            "--repo-ethos",
            str(REPO_ROOT / "repo_ethos.yml"),
            "--config",
            str(REPO_ROOT / "config.yaml"),
            "--out-dir",
            str(consumer / ".git" / "coding-ethos-hooks" / "policy"),
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )


def _write_poisoned_bin(fake_bin: Path, tool: str) -> None:
    fake_bin.mkdir(parents=True, exist_ok=True)
    path = fake_bin / tool
    path.write_text(
        f"#!/usr/bin/env bash\necho 'PWNED {tool} from consumer PATH' >&2\nexit 66\n",
        encoding="utf-8",
    )
    path.chmod(0o700)
