# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

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
    consumer = tmp_path / "consumer"
    consumer.mkdir()
    _run(["git", "init"], cwd=consumer)
    (consumer / ".gitignore").write_text(".coding-ethos/\n", encoding="utf-8")
    _run(["git", "add", ".gitignore"], cwd=consumer)
    _run(
        [
            "uv",
            "run",
            "python",
            "main.py",
            "--repo",
            str(consumer),
            "--sync-tool-configs",
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )
    _run(
        [
            "make",
            "go-tools-install",
            "policy-bundle-install",
            f"HOOK_CONSUMER_ROOT={consumer}",
        ],
        cwd=REPO_ROOT,
        timeout=180,
    )
    return consumer


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
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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
    _run(
        [
            "uv",
            "run",
            "python",
            "main.py",
            "--repo",
            str(consumer),
            "--sync-tool-configs",
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )
    nested = consumer / "lbox-platform" / "lib" / "python"
    package = nested / "lbox" / "corpus"
    package.mkdir(parents=True)
    target = package / "inline_migration.py"
    target.write_text("import os\n\nVALUE = 1\n", encoding="utf-8")

    env = os.environ.copy()
    env["CODE_ETHOS_HOOK_OUTPUT_FORMAT"] = "toon"
    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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
    _run(
        [
            "uv",
            "run",
            "python",
            "main.py",
            "--repo",
            str(consumer),
            "--sync-tool-configs",
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )
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
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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
    _run(
        [
            "uv",
            "run",
            "python",
            "main.py",
            "--repo",
            str(consumer),
            "--sync-tool-configs",
        ],
        cwd=REPO_ROOT,
        timeout=120,
    )
    (consumer / "pkg").mkdir()
    (consumer / "pkg" / "app.py").write_text("VALUE = 1\n", encoding="utf-8")

    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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
    helper = (REPO_ROOT / "coding_ethos" / "lint_source_roots.py").read_text(
        encoding="utf-8"
    )
    script = (REPO_ROOT / "pre-commit" / "hooks" / "tool-capture.sh").read_text(
        encoding="utf-8"
    )

    assert "-m coding_ethos.lint_source_roots" in script
    assert "resolve_lint_source_roots" in helper
    assert "ConfiguredLintRootError" in helper
    policy_source = (REPO_ROOT / "coding_ethos" / "tool_configs.py").read_text(
        encoding="utf-8"
    )
    assert "load_enforcement_config" in policy_source
    assert "python.source_paths" in policy_source
    assert "python.extra_paths" in policy_source
    assert "relative_to(repo_root)" in policy_source
    assert "pyrightconfig" not in helper


def test_lint_source_roots_helper_rejects_repo_escape(tmp_path: Path) -> None:
    consumer = tmp_path / "consumer"
    consumer.mkdir()
    (consumer / "repo_config.yml").write_text(
        """
version: 1
python:
  extra_paths:
    - ..
""".lstrip(),
        encoding="utf-8",
    )

    result = _run(
        [
            "uv",
            "run",
            "python",
            "-m",
            "coding_ethos.lint_source_roots",
            str(consumer),
        ],
        cwd=REPO_ROOT,
        check=False,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert "configured lint source root escapes repo: .." in output


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
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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


def test_runtime_bootstrap_repairs_missing_checkout_local_binary(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_tool = REPO_ROOT / "bin" / "coding-ethos-policy"
    policy_tool.unlink(missing_ok=True)

    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
            "policy",
            "validate",
            "--bundle",
            str(REPO_ROOT / "build" / "policy" / "policy-bundle.json"),
        ],
        cwd=consumer,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert policy_tool.exists()
    assert ".git/coding-ethos-hooks" not in output
    assert "stale" not in output.lower()


def test_runtime_bootstrap_repairs_missing_policy_bundle(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_bundle = REPO_ROOT / "build" / "policy" / "policy-bundle.json"
    policy_bundle.unlink(missing_ok=True)

    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
            "policy-lint",
            "--help",
        ],
        cwd=consumer,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert policy_bundle.exists()
    assert "Usage of coding-ethos-lint" in output
    assert ".git/coding-ethos-hooks" not in output
    assert "stale" not in output.lower()


def test_runtime_bootstrap_repairs_missing_managed_shfmt(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    shfmt = REPO_ROOT / "build" / "toolchain" / "go-bin" / "shfmt"
    shfmt.unlink(missing_ok=True)
    env = os.environ.copy()
    env["PATH"] = ":".join(
        part for part in env["PATH"].split(":") if not part.endswith("/go/bin")
    )

    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
            "policy-lint",
            "--help",
        ],
        cwd=consumer,
        env=env,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert shfmt.exists()
    assert "Usage of coding-ethos-lint" in output
    assert ".git/coding-ethos-hooks" not in output
    assert "stale" not in output.lower()


def test_concurrent_runtime_bootstrap_uses_checkout_local_lock(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    lint_tool = REPO_ROOT / "bin" / "coding-ethos-lint"
    lint_tool.unlink(missing_ok=True)
    command = [
        str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
        "policy-lint",
        "--help",
    ]

    first = subprocess.Popen(
        command,
        cwd=consumer,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    second = subprocess.Popen(
        command,
        cwd=consumer,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    first_stdout, first_stderr = first.communicate(timeout=180)
    second_stdout, second_stderr = second.communicate(timeout=180)

    first_output = first_stdout + first_stderr
    second_output = second_stdout + second_stderr
    assert first.returncode == 0, first_output
    assert second.returncode == 0, second_output
    assert lint_tool.exists()
    assert "Usage of coding-ethos-lint" in first_output
    assert "Usage of coding-ethos-lint" in second_output
    assert "stale" not in (first_output + second_output).lower()


def test_failed_runtime_bootstrap_reports_exact_make_command(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    lint_tool = REPO_ROOT / "bin" / "coding-ethos-lint"
    lint_tool.unlink(missing_ok=True)
    fake_bin = tmp_path / "fake-bin"
    fake_bin.mkdir()
    fake_make = fake_bin / "make"
    fake_make.write_text(
        "#!/usr/bin/env bash\necho 'fake make failure' >&2\nexit 73\n",
        encoding="utf-8",
    )
    fake_make.chmod(0o700)

    env = os.environ.copy()
    env["PATH"] = f"{fake_bin}:{env['PATH']}"
    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
            "policy-lint",
            "--help",
        ],
        cwd=consumer,
        env=env,
        check=False,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 73, output
    assert "coding-ethos runtime bootstrap failed" in output
    assert "make -C" in output
    assert "build output:" in output
    assert "fake make failure" in output
    assert "stale" not in output.lower()


def test_lifecycle_policy_lint_does_not_fail_on_policy_mtime_drift(
    tmp_path: Path,
) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    policy_source = REPO_ROOT / "coding_ethos.yml"
    future = time.time() + 10
    os.utime(policy_source, (future, future))

    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
            "policy-lint",
            "--help",
        ],
        cwd=consumer,
        timeout=180,
    )

    output = result.stdout + result.stderr
    assert "Usage of coding-ethos-lint" in output
    assert "compiled policy bundle is older" not in output
    assert "hook runtime is not installed or is stale" not in output


def test_validate_uses_policy_source_hashes_not_mtime(tmp_path: Path) -> None:
    _prepare_consumer_repo(tmp_path)
    policy_source = REPO_ROOT / "coding_ethos.yml"
    future = time.time() + 10
    os.utime(policy_source, (future, future))

    result = _run(["make", "validate"], cwd=REPO_ROOT, timeout=180)

    output = result.stdout + result.stderr
    assert "Validating bundled hook runtime" in output
    assert "compiled policy bundle is older" not in output
    assert "hook runtime is not installed or is stale" not in output


def test_cutover_verify_resolves_consumer_without_env(tmp_path: Path) -> None:
    consumer = _prepare_consumer_repo(tmp_path)
    env = os.environ.copy()
    env.pop("CODE_ETHOS_CONSUMER_ROOT", None)
    result = _run(
        [
            str(REPO_ROOT / "pre-commit" / "hooks" / "run-go-hook.sh"),
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


def test_installed_git_shim_contains_no_policy_specific_logic() -> None:
    shim = (REPO_ROOT / "pre-commit" / "hooks" / "run-git-hook.sh").read_text(
        encoding="utf-8"
    )

    assert "policy-bundle" not in shim
    assert "coding-ethos-policy" not in shim
    assert "coding_ethos.yml" not in shim
    assert "config.yaml" not in shim


def test_git_shim_missing_checkout_error_names_submodule_repair(tmp_path: Path) -> None:
    consumer = tmp_path / "consumer-without-bundle"
    consumer.mkdir()
    _run(["git", "init"], cwd=consumer)

    result = _run(
        [str(REPO_ROOT / "pre-commit" / "hooks" / "run-git-hook.sh")],
        cwd=consumer,
        check=False,
    )

    output = result.stdout + result.stderr
    assert result.returncode == 127, output
    assert "git submodule update --init coding-ethos" in output
    assert "policy-bundle" not in output
