#!/usr/bin/env python3
"""Pre-commit hook to ban TYPE_CHECKING and future annotations in Python code.

Two patterns are banned:

1. ``typing.TYPE_CHECKING`` — creates a conditional import path that only
   runs under type checkers but not at runtime. This is a sanctioned form
   of conditional import (ETHOS 3 violation) and hides circular dependency
   problems that should be fixed structurally.

2. ``from __future__ import annotations`` (PEP 563) — turns all annotations
   into lazy strings, deferring evaluation to type-checker time only. On
   Python 3.13+ this is unnecessary: native syntax supports ``list[str]``,
   ``X | Y``, and all modern generics at runtime. String annotations hide
   missing imports and break runtime introspection (Pydantic, dataclasses,
   ``get_type_hints()``).

The correct fix for both patterns is to use real, runtime-visible types:
extract shared types into a ``protocols`` module, use Protocol-first design
(ETHOS 12), or break dependency cycles with Dependency Inversion.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_type_checking_imports.py [files...]

Exit codes:
    0: All checks passed
    1: TYPE_CHECKING or future annotations usage detected
"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final

MIN_REQUIRED_ARGS: Final[int] = 2

# Names that indicate TYPE_CHECKING usage.
_TYPE_CHECKING_NAMES: Final[frozenset[str]] = frozenset(
    {
        "TYPE_CHECKING",
    }
)


@dataclass(frozen=True, slots=True)
class TypeCheckingViolation:
    """A detected TYPE_CHECKING usage in source code."""

    file: Path
    line: int
    pattern: str

    def render(self) -> str:
        """Render the violation for console output."""
        return f"  {self.file}:{self.line}: {self.pattern}"


def _is_type_checking_ref(node: ast.expr) -> bool:
    """Check if an expression refers to TYPE_CHECKING.

    Handles both ``TYPE_CHECKING`` (bare name) and
    ``typing.TYPE_CHECKING`` (attribute access).

    Args:
        node: AST expression node to check.

    Returns:
        True if the node references TYPE_CHECKING.

    """
    if isinstance(node, ast.Name) and node.id in _TYPE_CHECKING_NAMES:
        return True
    return (
        isinstance(node, ast.Attribute)
        and node.attr in _TYPE_CHECKING_NAMES
        and isinstance(node.value, ast.Name)
        and node.value.id == "typing"
    )


class TypeCheckingVisitor(ast.NodeVisitor):
    """AST visitor to detect TYPE_CHECKING import patterns.

    Finds three patterns:
    1. ``from typing import TYPE_CHECKING`` imports
    2. ``if TYPE_CHECKING:`` guard blocks
    3. ``if typing.TYPE_CHECKING:`` guard blocks
    """

    def __init__(self, filepath: Path) -> None:
        """Initialize the visitor.

        Args:
            filepath: Path to the file being checked.

        """
        self.filepath = filepath
        self.violations: list[TypeCheckingViolation] = []

    def visit_ImportFrom(self, node: ast.ImportFrom) -> None:
        """Check for ``from typing import TYPE_CHECKING`` and ``from __future__ import annotations``.

        Args:
            node: The ``ImportFrom`` AST node.

        """
        if node.module == "typing" and node.names:
            for alias in node.names:
                if alias.name in _TYPE_CHECKING_NAMES:
                    self.violations.append(
                        TypeCheckingViolation(
                            file=self.filepath,
                            line=node.lineno,
                            pattern="from typing import TYPE_CHECKING",
                        )
                    )
        if node.module == "__future__" and node.names:
            for alias in node.names:
                if alias.name == "annotations":
                    self.violations.append(
                        TypeCheckingViolation(
                            file=self.filepath,
                            line=node.lineno,
                            pattern="from __future__ import annotations (PEP 563 string annotations)",
                        )
                    )
        self.generic_visit(node)

    def visit_If(self, node: ast.If) -> None:
        """Check for ``if TYPE_CHECKING:`` guard blocks.

        Args:
            node: The ``If`` AST node.

        """
        if _is_type_checking_ref(node.test):
            self.violations.append(
                TypeCheckingViolation(
                    file=self.filepath,
                    line=node.lineno,
                    pattern="if TYPE_CHECKING: (conditional import guard)",
                )
            )
        self.generic_visit(node)


def check_file(filepath: Path) -> list[TypeCheckingViolation]:
    """Check a Python file for TYPE_CHECKING usage.

    Parses the file as an AST and walks it looking for
    TYPE_CHECKING imports and guard blocks.

    Args:
        filepath: Path to the Python file to check.

    Returns:
        List of violations found in the file.

    """
    content = filepath.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(filepath))
    visitor = TypeCheckingVisitor(filepath)
    visitor.visit(tree)
    return visitor.violations


def main() -> int:
    """Check Python files for TYPE_CHECKING import anti-patterns.

    Pre-commit passes only files matching the configured types and
    exclusions. Checks all files passed as command-line arguments.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_REQUIRED_ARGS:
        return 0

    files_to_check = [Path(arg) for arg in sys.argv[1:] if Path(arg).exists()]

    if not files_to_check:
        return 0

    all_violations: list[TypeCheckingViolation] = []

    for filepath in files_to_check:
        try:
            violations = check_file(filepath)
            all_violations.extend(violations)
        except SyntaxError as exc:
            sys.stderr.write(f"  skipping {filepath}: {exc}\n")
            continue

    if not all_violations:
        return 0

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("STRING ANNOTATION PATTERN DETECTED (ETHOS \u00a73, \u00a712)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Both TYPE_CHECKING and `from __future__ import annotations` make\n"
        "types exist only at check time, not at runtime. TYPE_CHECKING\n"
        "creates a conditional import path. PEP 563 future annotations\n"
        "turn ALL annotations into lazy strings, hiding missing imports\n"
        "and breaking runtime introspection (Pydantic, dataclasses,\n"
        "get_type_hints()). On Python 3.13+ neither pattern is needed.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for violation in all_violations:
        sys.stderr.write(violation.render() + "\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write(
        "  1. Remove `from __future__ import annotations` — use real\n"
        "     Python 3.13+ type syntax (list[str], X | Y, generics)\n"
    )
    sys.stderr.write(
        "  2. Extract shared types into a shared protocols module (ETHOS \u00a712)\n"
    )
    sys.stderr.write("  3. Use Protocol-first design to break circular deps\n")
    sys.stderr.write(
        "  4. Move type definitions to a shared module both sides import\n"
    )
    sys.stderr.write("  5. Apply Dependency Inversion to eliminate the cycle\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
