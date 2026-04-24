#!/usr/bin/env python3
"""Parallel type checker runner for pre-commit.

Runs pyright and mypy concurrently on staged Python files only.

Usage:
    Pre-commit: python pre-commit/hooks/parallel_type_check.py
    Pre-push: python pre-commit/hooks/parallel_type_check.py

Behavior:
    - Gets list of staged Python files from git
    - Runs all checkers in parallel using asyncio subprocesses
    - Waits for all to complete before reporting results
    - Exit code is 0 only if ALL checkers pass
"""

import asyncio
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

from hook_config import bundle_root, get_bool, get_list


@dataclass
class TypeCheckerConfig:
    """Configuration for a type checker."""

    name: str
    base_command: list[str]
    pass_files_as_args: bool = True


# Registered checkers - files will be appended to base_command
# pyright may need --project to find repo-local configuration files.
def _configured_checkers() -> list[TypeCheckerConfig]:
    checkers: list[TypeCheckerConfig] = []
    for raw in get_list("python.type_check.checkers", []):
        if not isinstance(raw, dict):
            continue
        name = str(raw.get("name", "")).strip()
        command = [
            str(item).strip() for item in raw.get("command", []) if str(item).strip()
        ]
        if name and command:
            checkers.append(
                TypeCheckerConfig(
                    name=name,
                    base_command=command,
                    pass_files_as_args=bool(raw.get("pass_files_as_args", True)),
                )
            )
    return checkers


def _hooks_pyproject() -> Path:
    return bundle_root() / "hooks" / "pyproject.toml"


def _repo_root() -> Path:
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True,
            text=True,
            check=True,
        )
    except (FileNotFoundError, subprocess.SubprocessError):
        return Path.cwd()
    return Path(result.stdout.strip())


def _repo_config(name: str) -> Path | None:
    candidate = _repo_root() / name
    return candidate if candidate.exists() else None


def _has_option(command: list[str], *names: str) -> bool:
    return any(
        token in names or any(token.startswith(f"{name}=") for name in names)
        for token in command
    )


def _resolved_command(config: TypeCheckerConfig) -> list[str]:
    command = list(config.base_command)
    hooks_pyproject = _hooks_pyproject()
    pyright_config = _repo_config("pyrightconfig.json")
    mypy_config = _repo_config("mypy.ini")

    if config.name == "pyright" and not _has_option(command, "--project", "-p"):
        project_path = pyright_config or (
            hooks_pyproject if hooks_pyproject.exists() else None
        )
        if project_path is not None:
            command.extend(["--project", str(project_path)])

    if config.name == "mypy" and not _has_option(command, "--config-file"):
        config_path = mypy_config or (
            hooks_pyproject if hooks_pyproject.exists() else None
        )
        if config_path is not None:
            command.extend(["--config-file", str(config_path)])

    return command


def _is_checkable_python_file(path: str) -> bool:
    """Return whether a path should be sent to the type checkers.

    Args:
        path: Repository-relative path.

    Returns:
        True when the file is a Python source file in the host environment.

    """
    return (
        path.endswith(".py")
        and not path.startswith(".venv/")
        # Docker scripts run in a different Python environment (3.12 + amd-quark)
        # and cannot be type-checked by the host toolchain (Python 3.13)
        and "/docker/" not in path
        # Vulture whitelist uses bare names as expressions — not valid Python
        and "vulture_whitelist" not in path
    )


def _normalize_python_files(paths: list[str]) -> list[str]:
    """Filter and deduplicate Python file paths.

    Args:
        paths: Raw file paths.

    Returns:
        Existing checkable Python files, preserving first-seen order.

    """
    seen: set[str] = set()
    files: list[str] = []
    for raw in paths:
        path = raw.strip()
        if (
            path
            and path not in seen
            and Path(path).exists()
            and _is_checkable_python_file(path)
        ):
            seen.add(path)
            files.append(path)
    return files


def get_staged_python_files() -> list[str]:
    """Get list of staged Python files from git.

    Returns:
        List of staged .py file paths.

    Raises:
        RuntimeError: If git command fails.

    """
    try:
        # S607: Safe - hardcoded git command in pre-commit hook
        result = subprocess.run(
            ["git", "diff", "--cached", "--name-only", "--diff-filter=ACMR"],
            capture_output=True,
            text=True,
            check=True,
        )
        return _normalize_python_files(result.stdout.splitlines())
    except subprocess.CalledProcessError as exc:
        msg = f"Failed to get staged files from git: {exc.stderr}"
        raise RuntimeError(msg) from exc
    except FileNotFoundError as exc:
        msg = "git command not found"
        raise RuntimeError(msg) from exc


async def run_checker(
    config: TypeCheckerConfig, files: list[str]
) -> tuple[str, int, str, float]:
    """Run a single type checker on the given files.

    Args:
        config: Type checker configuration.
        files: List of Python files to check.

    Returns:
        Tuple of (name, exit_code, output, duration_ms).

    """
    start = time.perf_counter()

    resolved_command = _resolved_command(config)
    command = (
        [*resolved_command, *files] if config.pass_files_as_args else resolved_command
    )

    try:
        proc = await asyncio.create_subprocess_exec(
            *command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
        )
        stdout, _ = await proc.communicate()
        duration_ms = (time.perf_counter() - start) * 1000

        return (
            config.name,
            proc.returncode or 0,
            stdout.decode("utf-8", errors="replace"),
            duration_ms,
        )

    except FileNotFoundError:
        duration_ms = (time.perf_counter() - start) * 1000
        return (config.name, 0, f"Skipped: {config.name} not installed", duration_ms)

    except (OSError, subprocess.SubprocessError) as exc:
        duration_ms = (time.perf_counter() - start) * 1000
        return (config.name, 1, f"Error running {config.name}: {exc}", duration_ms)


def format_results(results: list[tuple[str, int, str, float]], file_count: int) -> str:
    """Format type checker results for terminal output.

    Args:
        results: List of (name, exit_code, output, duration_ms) tuples.
        file_count: Number of files checked.

    Returns:
        Formatted report string.

    """
    lines = [
        "",
        "=" * 70,
        f"TYPE CHECKING (PARALLEL) - {file_count} staged file(s)",
        "=" * 70,
        "",
    ]

    total_time_ms = sum(r[3] for r in results)
    passed = sum(1 for r in results if r[1] == 0)
    failed = len(results) - passed

    lines.append(f"Summary: {passed} passed, {failed} failed")
    lines.append(f"Total time: {total_time_ms:.0f}ms (parallel execution)")
    lines.append("")

    for name, exit_code, output, duration_ms in results:
        icon = "OK" if exit_code == 0 else "XX"
        status = "PASS" if exit_code == 0 else "FAIL"
        lines.append(f"{icon} {name}: {status} ({duration_ms:.0f}ms)")

        # Only show output for failures
        if exit_code != 0:
            lines.append("")
            lines.extend(f"   {line}" for line in output.strip().split("\n"))
            lines.append("")

    lines.append("=" * 70)

    return "\n".join(lines)


async def run_all_checkers(files: list[str]) -> int:
    """Run all registered type checkers in parallel on staged files.

    Args:
        files: List of staged Python files to check.

    Returns:
        Exit code (0 = all passed, 1 = any failed).

    """
    checkers = _configured_checkers()
    if not checkers:
        sys.stderr.write("No type checkers registered\n")
        return 0

    # Run all checkers concurrently
    tasks = [run_checker(c, files) for c in checkers]
    results = await asyncio.gather(*tasks)

    # Return non-zero if any checker failed
    any_failed = any(r[1] != 0 for r in results)
    if any_failed:
        # Only print report on failure — silence is golden on success
        print(format_results(list(results), len(files)))
        sys.stderr.write(
            "\nXX Type checking failed\n   Fix the errors above and try again.\n\n"
        )
        return 1

    return 0


def main() -> int:
    """Run pyright and mypy in parallel on staged files.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if not get_bool("python.type_check.enabled", False):
        return 0

    files = (
        _normalize_python_files(sys.argv[1:])
        if len(sys.argv) > 1
        else get_staged_python_files()
    )

    if not files:
        sys.stderr.write("No staged Python files to check\n")
        return 0

    return asyncio.run(run_all_checkers(files))


if __name__ == "__main__":
    sys.exit(main())
