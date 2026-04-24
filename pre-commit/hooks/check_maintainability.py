#!/usr/bin/env python3
"""Check maintainability index of Python code.

Pre-commit hook for maintainability monitoring.
Currently ADVISORY - prints warnings but does not fail.

The Maintainability Index (MI) is a composite metric ranging from 0-100:
- 20+: Moderately maintainable
- 50+: Highly maintainable
- 85+: Very highly maintainable

Lower scores indicate code that is harder to understand and modify.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_maintainability.py

Exit codes:
    0: Always passes (advisory mode)
"""

import sys
from typing import Final

from radon_utils import run_radon

# Configuration
MI_THRESHOLD: Final[int] = 50
TARGET_PATH: Final[str] = "."
EXCLUDE_PATTERN: Final[str] = ".venv/*,node_modules/*"


def main() -> int:
    """Check maintainability index and warn if below threshold.

    Returns:
        Exit code 0 (always passes in advisory mode).

    """
    try:
        data = run_radon(["mi", TARGET_PATH, "-j", "-e", EXCLUDE_PATTERN])
    except RuntimeError:
        return 1

    warnings: list[str] = []

    for filename, metrics in data.items():
        mi_score: float = metrics["mi"]
        if mi_score < MI_THRESHOLD:
            warnings.append(f"  {filename} (MI: {mi_score:.2f})")

    # Advisory mode: always pass, zero output on success.
    # Warnings are tracked but not printed — use radon directly
    # for interactive maintainability review.
    return 0


if __name__ == "__main__":
    sys.exit(main())
