#!/usr/bin/env python3
"""Pre-commit hook to enforce docstring coverage on Python code.

This enforces ETHOS §18 (Documentation as Contract): public APIs must have
docstrings. Uses interrogate to measure coverage and fails if below threshold.

Usage:
    Pre-commit: python pre-commit/hooks/check_docstring_coverage.py

Exit codes:
    0: Docstring coverage meets threshold
    1: Coverage below threshold or interrogate failed
"""

import shutil
import subprocess
import sys

from hook_config import get_bool, get_list


def _coverage_threshold() -> int:
    return int(get_list("python.docstring_coverage.threshold", [90])[0])


def _check_paths() -> list[str]:
    return [
        str(item).strip()
        for item in get_list(
            "python.docstring_coverage.check_paths",
            ["coding_ethos", "pre-commit/hooks"],
        )
        if str(item).strip()
    ]


def _exclude_patterns() -> list[str]:
    return [
        str(item).strip()
        for item in get_list(
            "python.docstring_coverage.exclude_patterns",
            [r"__pycache__", r"\.venv", r"tests", r".*_test\.py$", r"test_.*\.py$"],
        )
        if str(item).strip()
    ]


def run_interrogate() -> int:
    """Run interrogate and check docstring coverage.

    Returns:
        Exit code (0 for success, 1 for failure).

    """
    interrogate_path = shutil.which("interrogate")
    if not interrogate_path:
        print("ERROR: interrogate not found. Install the hook environment.")
        return 1

    # Build interrogate command
    cmd = [
        interrogate_path,
        "--fail-under",
        str(_coverage_threshold()),
        "--verbose",
        "--ignore-init-method",  # __init__ docstrings optional
        "--ignore-init-module",  # __init__.py module docstrings optional
        "--ignore-magic",  # Magic methods like __str__ optional
        "--ignore-private",  # Private methods (_foo) optional
        "--ignore-semiprivate",  # Semi-private (__foo) optional
        "--ignore-property-decorators",  # Properties optional
        "--ignore-nested-functions",  # Nested functions optional
        "--ignore-nested-classes",  # Nested classes optional
    ]

    # Add exclude patterns
    for pattern in _exclude_patterns():
        cmd.extend(["--ignore-regex", pattern])

    # Add paths to check
    cmd.extend(_check_paths())

    try:
        # S603: Safe - hardcoded command with all arguments from constants
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=False,
        )
    except FileNotFoundError:
        print("ERROR: interrogate not found. Install the hook environment.")
        return 1

    if result.returncode != 0:
        # Only print output on failure — silence is golden on success
        print("=" * 60)
        print("DOCSTRING COVERAGE CHECK FAILED (ETHOS §18)")
        print("=" * 60)
        print(f"Threshold: {_coverage_threshold()}%")
        print(f"Paths: {', '.join(_check_paths())}")
        print()
        if result.stdout:
            print(result.stdout)
        if result.stderr:
            print(result.stderr, file=sys.stderr)
        print()
        print("Per ETHOS §18 (Documentation as Contract):")
        print("  - Every public function must have a Google-style docstring")
        print("  - Docstrings document the contract between function and caller")
        print("  - If you change behavior, update the docstring")
        print()
        print(f"Add docstrings to reach {_coverage_threshold()}% coverage.")
        return 1

    return 0


def main() -> int:
    """Enforce docstring coverage threshold via interrogate."""
    if not get_bool("python.docstring_coverage.enabled", True):
        return 0
    return run_interrogate()


if __name__ == "__main__":
    sys.exit(main())
