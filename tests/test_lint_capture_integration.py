# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Regression tests for managed lint capture in hostile consumer repos.

These tests build temporary consumer repositories with poisoned PATH entries,
drifted generated configs, and missing checkout-local runtime artifacts. They
verify that coding-ethos remains the policy and tool source of truth.
"""

import os
import subprocess
import sys
import time
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
    assert "format: toon" in output
    assert "tool: ruff" in output
    assert "F401" in output
    assert "lbox-platform/lib/python/lbox/parsing/analyzer_base.py" in output
    assert str(consumer) not in output
    assert "findings[0]" not in output


def test_policy_tool_resolves_package_paths_from_repo_root(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    (consumer / "repo_config.yml").write_text(
        """
version: 1
python:
  source_paths:
    - lbox-platform/lib/python/lbox
  extra_paths:
    - lbox-platform/lib/python
""".lstrip(),
        encoding="utf-8",
    )
    _sync_consumer_tool_configs(consumer)
    nested = consumer / "lbox-platform" / "lib" / "python"
    package = nested / "lbox" / "corpus"
    package.mkdir(parents=True)
    target = package / "inline_migration.py"
    target.write_text("import os\n\nVALUE = 1\n", encoding="utf-8")

    env = os.environ.copy()
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "ruff",
            "check",
            "lbox/corpus/inline_migration.py",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "F401" in output
    assert "lbox-platform/lib/python/lbox/corpus/inline_migration.py" in output
    assert "coding-ethos/lbox/corpus/inline_migration.py" not in output
    assert str(consumer) not in output


def test_policy_tool_resolves_package_globs_from_policy_roots(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    (consumer / "repo_config.yml").write_text(
        """
version: 1
python:
  source_paths:
    - lbox-platform/lib/python/lbox
  extra_paths:
    - lbox-platform/lib/python
""".lstrip(),
        encoding="utf-8",
    )
    _sync_consumer_tool_configs(consumer)
    package = consumer / "lbox-platform" / "lib" / "python" / "lbox" / "corpus"
    package.mkdir(parents=True)
    (package / "inline_migration.py").write_text(
        "import os\n\nVALUE = 1\n", encoding="utf-8"
    )
    (package / "audit.py").write_text("import sys\n\nVALUE = 2\n", encoding="utf-8")

    env = os.environ.copy()
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "bin" / "coding-ethos-run"),
            "policy-tool",
            "ruff",
            "check",
            "lbox/corpus/*.py",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "F401" in output
    assert "lbox-platform/lib/python/lbox/corpus/audit.py" in output
    assert "lbox-platform/lib/python/lbox/corpus/inline_migration.py" in output
    assert "coding-ethos/lbox/corpus" not in output
    assert str(consumer) not in output


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


def test_lint_target_source_roots_come_from_policy_config() -> None:
    runtime_config = (REPO_ROOT / "go" / "lintcapture" / "config.go").read_text(
        encoding="utf-8"
    )
    resolver = (REPO_ROOT / "go" / "lintcapture" / "targets.go").read_text(
        encoding="utf-8"
    )

    assert "LoadRuntimeConfig" in runtime_config
    assert '"python", "source_paths"' in runtime_config
    assert '"python", "extra_paths"' in runtime_config
    assert "containedSourceRoots" in runtime_config
    assert "errLintSourceRootEscapesRepo" in resolver
    assert "pyrightconfig" not in runtime_config


def test_lint_tool_shim_inventory_comes_from_go_catalog() -> None:
    shims = (REPO_ROOT / "go" / "internal" / "lintcli" / "shims.go").read_text(
        encoding="utf-8"
    )

    assert not (REPO_ROOT / "pre-commit" / "hooks" / "tool-capture.sh").exists()
    assert "CapturedLintTools()" in shims
    assert "CAPTURED_LINT_TOOLS" not in shims
    assert "CODING_ETHOS_POLICY_TOOL_SHIM=1" in shims


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
    assert result.returncode == 127, output
    assert "missing compiled policy bundle" in output
    assert "run make build" in output
    assert ".git/coding-ethos-hooks" not in output


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
