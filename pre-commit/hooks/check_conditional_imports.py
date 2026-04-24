#!/usr/bin/env python3
"""Pre-commit hook to detect conditional import anti-patterns in Python code.

This enforces ETHOS §3 (No Conditional Imports): if a module requires a
library, that library must be present. We do not wrap imports in
``try``/``except`` blocks to hide missing dependencies. If the environment
is missing a requirement, the application must crash at the import stage.

The hook detects two patterns:
1. ``try: import X except ImportError: ...`` — direct conditional import.
2. ``HAS_*`` variable assignments following ``except ImportError`` —
   the "soft dependency" capability flag pattern.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_conditional_imports.py [files...]

Exit codes:
    0: All checks passed
    1: Conditional import anti-patterns detected

"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final

MIN_REQUIRED_ARGS: Final[int] = 2

# Exception types that indicate a conditional import pattern.
_IMPORT_ERROR_NAMES: Final[frozenset[str]] = frozenset(
    {
        "ImportError",
        "ModuleNotFoundError",
    }
)


@dataclass(frozen=True)
class ConditionalImportViolation:
    """Represents a conditional import anti-pattern finding."""

    file: Path
    line: int
    module_name: str
    pattern: str

    def render(self) -> str:
        """Render the violation for console output."""
        return (
            f"  {self.file}:{self.line}: "
            f"conditional import of '{self.module_name}' ({self.pattern})"
        )


def _extract_import_names(body: list[ast.stmt]) -> list[str]:
    """Extract module names from import statements in a statement list.

    Walks the list of statements and collects module names from
    ``import X`` and ``from X import Y`` statements.

    Args:
        body: List of AST statement nodes.

    Returns:
        List of module names found in import statements.

    """
    names: list[str] = []
    for stmt in body:
        if isinstance(stmt, ast.Import):
            names.extend(alias.name for alias in stmt.names)
        elif isinstance(stmt, ast.ImportFrom) and stmt.module:
            names.append(stmt.module)
    return names


def _catches_import_error(handlers: list[ast.ExceptHandler]) -> bool:
    """Check if any except handler catches ImportError or ModuleNotFoundError.

    Args:
        handlers: List of ``ExceptHandler`` AST nodes.

    Returns:
        True if any handler catches an import-related exception.

    """
    for handler in handlers:
        if handler.type is None:
            # Bare except catches everything including ImportError
            return True
        if (
            isinstance(handler.type, ast.Name)
            and handler.type.id in _IMPORT_ERROR_NAMES
        ):
            return True
        if isinstance(handler.type, ast.Tuple):
            for elt in handler.type.elts:
                if isinstance(elt, ast.Name) and elt.id in _IMPORT_ERROR_NAMES:
                    return True
    return False


def _has_capability_flag(handlers: list[ast.ExceptHandler]) -> list[str]:
    """Find HAS_* variable assignments in except handlers.

    Detects the ``HAS_X = False`` pattern that indicates a soft
    dependency capability flag.

    Args:
        handlers: List of ``ExceptHandler`` AST nodes.

    Returns:
        List of ``HAS_*`` variable names found in handlers.

    """
    flags: list[str] = []
    for handler in handlers:
        for stmt in handler.body:
            if isinstance(stmt, ast.Assign):
                flags.extend(
                    target.id
                    for target in stmt.targets
                    if isinstance(target, ast.Name) and target.id.startswith("HAS_")
                )
    return flags


class ConditionalImportVisitor(ast.NodeVisitor):
    """AST visitor to detect conditional import anti-patterns.

    Walks the AST looking for ``try`` blocks that contain imports and
    have ``except ImportError`` or ``except ModuleNotFoundError`` handlers.
    Also detects ``HAS_*`` capability flag patterns.
    """

    def __init__(self, filepath: Path) -> None:
        """Initialize the visitor.

        Args:
            filepath: Path to the file being checked.

        """
        self.filepath = filepath
        self.violations: list[ConditionalImportViolation] = []

    def visit_Try(self, node: ast.Try) -> None:
        """Check a try block for conditional import patterns.

        A try block is flagged if it contains import statements in the
        try body and any except handler catches ``ImportError`` or
        ``ModuleNotFoundError``.

        Args:
            node: The ``Try`` AST node.

        """
        import_names = _extract_import_names(node.body)

        if import_names and _catches_import_error(node.handlers):
            for name in import_names:
                self.violations.append(
                    ConditionalImportViolation(
                        file=self.filepath,
                        line=node.lineno,
                        module_name=name,
                        pattern="try/import/except ImportError",
                    )
                )

        # Also check for HAS_* capability flags
        if _catches_import_error(node.handlers):
            flags = _has_capability_flag(node.handlers)
            for flag in flags:
                self.violations.append(
                    ConditionalImportViolation(
                        file=self.filepath,
                        line=node.lineno,
                        module_name=flag,
                        pattern="HAS_* capability flag in except ImportError",
                    )
                )

        self.generic_visit(node)


def check_file(filepath: Path) -> list[ConditionalImportViolation]:
    """Check a Python file for conditional import anti-patterns.

    Parses the file as an AST and walks it looking for ``try``/``import``/
    ``except ImportError`` patterns.

    Args:
        filepath: Path to the Python file to check.

    Returns:
        List of violations found in the file.

    """
    content = filepath.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(filepath))
    visitor = ConditionalImportVisitor(filepath)
    visitor.visit(tree)
    return visitor.violations


def main() -> int:
    """Check Python files for conditional import anti-patterns.

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

    all_violations: list[ConditionalImportViolation] = []

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
    sys.stderr.write("CONDITIONAL IMPORT CHECK FAILED (ETHOS §3)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Per ETHOS §3: if a module requires a library, that library must\n"
        "be present. Do not wrap imports in try/except to hide missing\n"
        "dependencies. The application must crash at the import stage.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for violation in all_violations:
        sys.stderr.write(violation.render() + "\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  Remove the try/except and import directly:\n")
    sys.stderr.write("    import some_library  # Crash if missing — good.\n")
    sys.stderr.write("\n")
    sys.stderr.write("  Add the dependency to pyproject.toml if needed.\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
