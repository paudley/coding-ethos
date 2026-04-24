#!/usr/bin/env python3
"""Check for dead/unused code using vulture.

Pre-commit hook for dead code detection.
Fails if potentially unused code is detected with high confidence.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_vulture.py

Exit codes:
    0: No dead code detected
    1: Dead code detected or error
"""

import shutil
import subprocess
import sys
from pathlib import Path
from typing import Final

# Configuration
MIN_CONFIDENCE: Final[int] = 80
EXCLUDE_PATTERNS: Final[list[str]] = [
    # Virtual environments - various locations
    ".venv",
    "*/.venv",
    "*/.venv/*",
    "lib/python/.venv",
    "lib/python/.venv/*",
    # Lint cache (tool archives)
    ".lint-cache",
    "*/.lint-cache",
    "*/.lint-cache/*",
    "lib/python/.lint-cache",
    "lib/python/.lint-cache/*",
    # Python cache
    "__pycache__",
    "*/__pycache__",
    # Node modules
    "node_modules",
    "*/node_modules",
    # Test files — pytest fixtures appear as unused variables to vulture
    # because they are activated via dependency injection, not direct calls
    "tests",
    "*/tests",
    "*/tests/*",
]
TARGET_PATH: Final[str] = "."


def find_whitelist() -> Path:
    """Find the vulture whitelist file.

    Returns:
        Path to whitelist file.

    Raises:
        FileNotFoundError: If whitelist file cannot be found.

    """
    # Try relative to script location first
    script_dir = Path(__file__).parent
    whitelist = script_dir / "vulture_whitelist.py"
    if whitelist.exists():
        return whitelist

    # Try pre-commit directory
    whitelist = script_dir.parent / "vulture_whitelist.py"
    if whitelist.exists():
        return whitelist

    msg = "vulture_whitelist.py not found"
    raise FileNotFoundError(msg)


def main() -> int:
    """Run vulture dead code detection.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    vulture_path = shutil.which("vulture")
    if not vulture_path:
        sys.stderr.write("Error: 'vulture' not found in PATH\n")
        return 1

    # Build command
    command = [vulture_path, TARGET_PATH]

    # Add whitelist if found
    try:
        whitelist = find_whitelist()
    except FileNotFoundError:
        pass
    else:
        command.append(str(whitelist))

    # Add confidence threshold
    command.extend(["--min-confidence", str(MIN_CONFIDENCE)])

    # Add exclude patterns as comma-separated list (vulture only uses last --exclude)
    if EXCLUDE_PATTERNS:
        command.extend(["--exclude", ",".join(EXCLUDE_PATTERNS)])

    try:
        # S603: Safe - hardcoded command with trusted input
        result = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=120,
        )
    except subprocess.TimeoutExpired:
        sys.stderr.write("Error: vulture timed out after 120s\n")
        return 1

    if result.returncode != 0:
        # Only print output on failure — silence is golden on success
        if result.stdout:
            sys.stdout.write(result.stdout)
        if result.stderr:
            sys.stderr.write(result.stderr)
        sys.stderr.write("\n")
        sys.stderr.write("=" * 60 + "\n")
        sys.stderr.write("Dead code detection FAILED\n")
        sys.stderr.write("=" * 60 + "\n")
        sys.stderr.write("\nTo fix:\n")
        sys.stderr.write("  - Remove unused code\n")
        sys.stderr.write("  - Add to vulture_whitelist.py if intentionally unused\n")
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
