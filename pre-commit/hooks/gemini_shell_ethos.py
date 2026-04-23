#!/usr/bin/env python3
"""Shell ETHOS compliance checker - thin wrapper around orchestrator.

For normal use, the orchestrator runs all checks in parallel.
This script is for debugging/manual runs of just the ETHOS check.

Usage:
    python pre-commit/hooks/gemini_shell_ethos.py [files...]
    python pre-commit/hooks/gemini_shell_ethos.py --full-check
"""

import subprocess
import sys
from pathlib import Path


def main() -> int:
    """Run ETHOS compliance check via orchestrator.

    Returns:
        Exit code (0 = pass/skipped, 1 = CRITICAL violations found).

    """
    script_dir = Path(__file__).parent
    cmd = [
        sys.executable,
        str(script_dir / "gemini_orchestrator.py"),
        "--check-type",
        "shell_ethos",
    ]

    cmd.extend(sys.argv[1:])

    # S603: Safe - calling our own orchestrator script with controlled args
    result = subprocess.run(cmd, check=False)
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
