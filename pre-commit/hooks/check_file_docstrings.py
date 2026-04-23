#!/usr/bin/env python3
"""Pre-commit hook to enforce file-level module docstrings.

Every Python file must have a module-level docstring of at least 3
sentences. This enforces ETHOS §18 (Documentation as Contract): an
undocumented module is a bug. Module docstrings serve as the contract
between the module and its consumers, explaining purpose, usage, and
important caveats.

Sentence counting uses a simple heuristic: a sentence ends with a
period, exclamation mark, or question mark followed by whitespace or
end-of-string. This intentionally counts code examples and URLs as
part of the docstring length rather than as separate sentences.

Exemptions:
    - ``__init__.py`` files (covered by ``check-init-docs`` hook).
    - ``conftest.py`` files (pytest configuration, not public API).
    - Test files (filtered by pre-commit exclude directive).
    - ``.venv`` files (filtered by pre-commit exclude directive).

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_file_docstrings.py [files...]

Exit codes:
    0: All files have adequate module docstrings
    1: One or more files have missing or insufficient docstrings

"""

import ast
import re
import sys
from pathlib import Path
from typing import Final, NamedTuple


class DocstringViolation(NamedTuple):
    """Record of a module docstring violation."""

    file: Path
    reason: str
    sentence_count: int


# Minimum number of args (script name + at least one file).
MIN_ARGS: Final[int] = 2

# Minimum number of sentences required in a module docstring.
MIN_SENTENCES: Final[int] = 3

# Files exempt from the module docstring requirement.
_EXEMPT_FILENAMES: Final[frozenset[str]] = frozenset(
    {
        "__init__.py",
        "conftest.py",
    }
)

# Sentence-ending pattern: period, exclamation, or question mark
# followed by whitespace or end-of-string.
_SENTENCE_END: Final[re.Pattern[str]] = re.compile(r"[.!?](?:\s|$)")


def count_sentences(text: str) -> int:
    """Count the number of sentences in a docstring.

    Uses a simple heuristic: a sentence ends with a period, exclamation
    mark, or question mark followed by whitespace or end-of-string.

    Args:
        text: The docstring text to analyze.

    Returns:
        Number of sentences found.

    """
    return len(_SENTENCE_END.findall(text))


def check_module_docstring(path: Path) -> DocstringViolation:
    """Check a Python file for an adequate module docstring.

    Parses the file as an AST and extracts the module-level docstring.
    Verifies that the docstring exists and contains at least
    ``MIN_SENTENCES`` sentences.

    Args:
        path: Path to the Python file.

    Returns:
        A DocstringViolation if the docstring is missing or insufficient,
        or a DocstringViolation with empty reason if the file passes.

    Raises:
        OSError: If the file cannot be read.
        SyntaxError: If the file cannot be parsed as Python.

    """
    content = path.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(path))
    docstring = ast.get_docstring(tree)

    if docstring is None:
        return DocstringViolation(
            file=path,
            reason="missing module docstring",
            sentence_count=0,
        )

    sentences = count_sentences(docstring)
    if sentences < MIN_SENTENCES:
        return DocstringViolation(
            file=path,
            reason=(
                f"module docstring has {sentences} sentence(s), need {MIN_SENTENCES}"
            ),
            sentence_count=sentences,
        )

    # File passes — return empty reason to indicate success.
    return DocstringViolation(file=path, reason="", sentence_count=sentences)


def _is_exempt(path: Path) -> bool:
    """Check if a file is exempt from the docstring requirement.

    Args:
        path: Path to the Python file.

    Returns:
        True if the file is exempt (``__init__.py``, ``conftest.py``).

    """
    return path.name in _EXEMPT_FILENAMES


def main() -> int:
    """Enforce module-level docstrings on Python files.

    Pre-commit passes only files matching the configured types and
    exclusions. Check all files passed to us except exempt filenames.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_ARGS:
        return 0

    files_to_check = [
        Path(arg)
        for arg in sys.argv[1:]
        if Path(arg).exists() and not _is_exempt(Path(arg))
    ]

    if not files_to_check:
        return 0

    violations: list[DocstringViolation] = []

    for filepath in files_to_check:
        try:
            result = check_module_docstring(filepath)
            if result.reason:
                violations.append(result)
        except SyntaxError as exc:
            sys.stderr.write(f"  skipping {filepath}: {exc}\n")
            continue

    if not violations:
        return 0

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("MODULE DOCSTRING CHECK FAILED (ETHOS §18)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Per ETHOS §18 (Documentation as Contract): every Python file\n"
        f"must have a module-level docstring of at least {MIN_SENTENCES} sentences.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in violations:
        sys.stderr.write(f"  {v.file}: {v.reason}\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  Add a module-level docstring at the top of the file:\n")
    sys.stderr.write('  """Brief summary of the module.\n')
    sys.stderr.write("\n")
    sys.stderr.write("  More detail about what the module provides. Include\n")
    sys.stderr.write("  usage examples and important caveats.\n")
    sys.stderr.write('  """\n')
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
