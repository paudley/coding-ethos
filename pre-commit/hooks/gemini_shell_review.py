#!/usr/bin/env python3
"""Shell script review checker - thin wrapper around orchestrator.

For normal use, the orchestrator runs all checks in parallel.
This script is for debugging/manual runs of just the shell review check.

Usage:
    python pre-commit/hooks/gemini_shell_review.py [files...]
    python pre-commit/hooks/gemini_shell_review.py --full-check
"""

import subprocess
import sys
from pathlib import Path


def main() -> int:
    """Run shell review check via orchestrator.

    Returns:
        Exit code (0 = pass/skipped, 1 = CRITICAL violations found).

    """
    # Build command: orchestrator with --check-type shell_review
    script_dir = Path(__file__).parent
    cmd = [
        sys.executable,
        str(script_dir / "gemini_orchestrator.py"),
        "--check-type",
        "shell_review",
    ]

    # Pass through all arguments except the script name
    cmd.extend(sys.argv[1:])

    # S603: Safe - calling our own orchestrator script with controlled args
    result = subprocess.run(cmd, check=False)
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
