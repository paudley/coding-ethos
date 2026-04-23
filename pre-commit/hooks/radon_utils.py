#!/usr/bin/env python3
"""Shared utilities for radon-based code quality checks.

Provides a common interface for running radon commands and parsing the JSON
output for use by complexity and maintainability checkers.
"""

import json
import shutil
import subprocess
import sys
from typing import Any


def run_radon(command_args: list[str], timeout: int = 60) -> dict[str, Any]:
    """Run a radon command and return the parsed JSON output.

    Args:
        command_args: Arguments for radon (e.g., ["cc", ".", "-j"])
        timeout: Maximum seconds to wait for radon to complete

    Returns:
        Parsed JSON output as a dict.

    Raises:
        RuntimeError: If radon cannot be run or output cannot be parsed.

    """
    radon_path = shutil.which("radon")
    if not radon_path:
        msg = "'radon' not found in PATH"
        sys.stderr.write(f"Error: {msg}\n")
        raise RuntimeError(msg)

    command = [radon_path, *command_args]
    try:
        # S603: Safe - hardcoded command with trusted input from pre-commit config
        result = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired as exc:
        msg = f"radon timed out after {timeout}s"
        sys.stderr.write(f"Error: {msg}\n")
        raise RuntimeError(msg) from exc

    if result.returncode != 0:
        msg = f"radon failed: {result.stderr}"
        sys.stderr.write(f"Error running radon: {result.stderr}\n")
        raise RuntimeError(msg)

    try:
        parsed: dict[str, Any] = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        msg = f"radon output parse error: {exc}"
        sys.stderr.write(f"Error parsing radon output: {exc}\n")
        raise RuntimeError(msg) from exc
    else:
        return parsed
