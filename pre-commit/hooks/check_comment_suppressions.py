#!/usr/bin/env python3
"""Pre-commit hook to ban all comment-based lint suppressions in Python files.

Detects and rejects inline and file-level suppression comments:

- ``# noqa`` / ``# noqa: CODE`` (ruff/flake8)
- ``# ruff: noqa`` (ruff file-level)
- ``# type: ignore`` / ``# type: ignore[code]`` (mypy)
- ``# mypy: ignore-errors`` (mypy file-level)
- ``# pragma: no cover`` (coverage)
- ``# pylint: disable`` / ``# pylint: disable-next`` (pylint)
- ``# noinspection`` (PyCharm/JetBrains)
- ``# fmt: off`` / ``# fmt: on`` / ``# fmt: skip`` (formatter bypass)
- ``# isort: skip`` / ``# isort: skip_file`` (isort)
- ``# pyright: ignore`` / ``# pyright: ignore[code]`` (pyright)

Per ETHOS §14, lint suppressions are forbidden. If a linter flags an issue,
fix it properly using SOLID principles. If a suppression seems necessary,
the code design is wrong.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_comment_suppressions.py [files...]

Exit codes:
    0: No suppression comments found
    1: One or more files contain suppression comments
"""

import io
import re
import sys
import tokenize
from pathlib import Path
from typing import Final, NamedTuple


class SuppressionViolation(NamedTuple):
    """Record of a comment-based suppression found in source.

    Args:
        file: Path to the file containing the suppression.
        line: Source line number (1-indexed).
        kind: Category of suppression (e.g., "noqa", "type: ignore").
        comment: The full comment text.

    """

    file: Path
    line: int
    kind: str
    comment: str


MIN_ARGS: Final[int] = 2

# Each pattern matches against the text of a comment token (starts with #).
# Patterns are checked in order; first match wins.
_SUPPRESSION_PATTERNS: Final[list[tuple[re.Pattern[str], str]]] = [
    (re.compile(r"#\s*ruff:\s*noqa\b"), "ruff: noqa (file-level)"),
    (re.compile(r"#\s*mypy:\s*ignore-errors\b"), "mypy: ignore-errors (file-level)"),
    (re.compile(r"#\s*noqa\b"), "noqa"),
    (re.compile(r"#\s*type:\s*ignore\b"), "type: ignore"),
    (re.compile(r"#\s*pragma:\s*no\s*cover\b"), "pragma: no cover"),
    (re.compile(r"#\s*pylint:\s*disable"), "pylint: disable"),
    (re.compile(r"#\s*noinspection\b"), "noinspection"),
    (re.compile(r"#\s*fmt:\s*(off|on|skip)\b"), "fmt: off/on/skip"),
    (re.compile(r"#\s*isort:\s*(skip|skip_file)\b"), "isort: skip"),
    (re.compile(r"#\s*pyright:\s*ignore\b"), "pyright: ignore"),
]


def _classify_comment(comment_text: str) -> str:
    """Classify a comment as a suppression type, if any.

    Args:
        comment_text: The full comment string (including ``#``).

    Returns:
        Suppression kind string, or empty string if not a suppression.

    """
    for pattern, kind in _SUPPRESSION_PATTERNS:
        if pattern.search(comment_text):
            return kind
    return ""


def find_suppressions(path: Path) -> list[SuppressionViolation]:
    """Find all comment-based lint suppressions in a Python file.

    Uses the ``tokenize`` module to extract only actual comments,
    avoiding false positives from strings containing suppression-like text.

    Args:
        path: Path to the Python file.

    Returns:
        List of SuppressionViolation records.

    Raises:
        OSError: If the file cannot be read.
        tokenize.TokenError: If the file cannot be tokenized.

    """
    content = path.read_text(encoding="utf-8")

    violations: list[SuppressionViolation] = []

    tokens = tokenize.generate_tokens(io.StringIO(content).readline)
    for tok in tokens:
        if tok.type != tokenize.COMMENT:
            continue
        kind = _classify_comment(tok.string)
        if kind:
            violations.append(
                SuppressionViolation(path, tok.start[0], kind, tok.string.strip())
            )

    return violations


def main() -> int:
    """Run the suppression check on all provided files.

    Pre-commit passes only files matching configured types and exclusions.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_ARGS:
        return 0

    files_to_check = [Path(arg) for arg in sys.argv[1:] if Path(arg).exists()]
    if not files_to_check:
        return 0

    all_violations: list[SuppressionViolation] = []
    for filepath in files_to_check:
        all_violations.extend(find_suppressions(filepath))

    if not all_violations:
        return 0

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("COMMENT-BASED LINT SUPPRESSION DETECTED\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Comment-based suppressions (noqa, type: ignore, pragma, etc.)\n"
        "are banned. Fix the underlying issue instead of suppressing it.\n"
        "Per ETHOS §14: linters are not suggestions, they are enforcement.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in all_violations:
        sys.stderr.write(f"  {v.file}:{v.line}: [{v.kind}] {v.comment}\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  Remove the suppression comment and fix the code.\n")
    sys.stderr.write("  If a linter flags an issue, apply SOLID principles:\n")
    sys.stderr.write("    - Long function?  Split into focused units (SRP)\n")
    sys.stderr.write("    - Too many params? Use config objects (ISP)\n")
    sys.stderr.write("    - Complex logic?   Use polymorphism (OCP)\n")
    sys.stderr.write("    - Tight coupling?  Inject dependencies (DIP)\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
