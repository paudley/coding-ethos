#!/usr/bin/env python3
"""Check cyclomatic complexity of Python code.

Pre-commit hook for complexity enforcement.
Fails if any function exceeds complexity threshold (15).

Cyclomatic complexity measures the number of independent paths through code.
A threshold of 15 is industry-standard; functions above this become difficult
to test comprehensively and maintain reliably.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_complexity.py

Exit codes:
    0: All functions within complexity threshold
    1: One or more functions exceed threshold
"""

import sys
from typing import Final

from radon_utils import run_radon

# Configuration
COMPLEXITY_THRESHOLD: Final[int] = 15
TARGET_PATH: Final[str] = "."
EXCLUDE_PATTERN: Final[str] = ".venv/*,node_modules/*"


def main() -> int:
    """Check cyclomatic complexity and fail if threshold exceeded.

    When invoked as a pre-commit hook with ``pass_filenames: true``,
    consumes the staged file list from ``sys.argv[1:]`` and scans
    only those paths. When invoked with no arguments (standalone
    debugging / manual runs), falls back to a whole-tree scan of
    :data:`TARGET_PATH` so the script remains usable outside
    pre-commit.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    targets = sys.argv[1:] or [TARGET_PATH]
    try:
        data = run_radon(["cc", *targets, "-j", "-e", EXCLUDE_PATTERN])
    except RuntimeError:
        return 1

    violations: list[str] = []

    for filename, functions in data.items():
        violations.extend(
            f"  {filename}:{func['lineno']} {func['name']}"
            f" (complexity: {func['complexity']})"
            for func in functions
            if func.get("complexity", 0) > COMPLEXITY_THRESHOLD
        )

    if violations:
        sys.stderr.write(
            f"Functions exceeding complexity threshold ({COMPLEXITY_THRESHOLD}):\n"
        )
        for v in violations:
            sys.stderr.write(f"{v}\n")
        sys.stderr.write("\n")
        sys.stderr.write("=" * 60 + "\n")
        sys.stderr.write("Complexity check FAILED\n")
        sys.stderr.write("=" * 60 + "\n")
        sys.stderr.write("\nRefactoring suggestions:\n")
        sys.stderr.write("  - Extract helper functions for conditional branches\n")
        sys.stderr.write("  - Use early returns to reduce nesting\n")
        sys.stderr.write("  - Replace complex conditionals with strategy patterns\n")
        sys.stderr.write("  - Consider dictionary dispatch for switch-like logic\n")
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
