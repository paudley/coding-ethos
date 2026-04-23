#!/usr/bin/env python3
"""Pre-commit hook to detect catch-and-silence anti-patterns in Python code.

This enforces ETHOS §23 (Exception Hierarchy and Error Messages): exceptions
must never be silently swallowed. Every ``except`` handler must either handle
the exception (take corrective action), transform and re-raise, or log and
re-raise. Handlers that consist solely of ``pass``, ``continue``, or
``return None`` are bugs masquerading as error handling.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_catch_and_silence.py [files...]

Exit codes:
    0: All checks passed
    1: Catch-and-silence anti-patterns detected

"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final


MIN_REQUIRED_ARGS: Final[int] = 2


@dataclass(frozen=True)
class SilenceViolation:
    """Represents a catch-and-silence anti-pattern finding."""

    file: Path
    line: int
    exception_type: str
    handler_body: str

    def render(self) -> str:
        """Render the violation for console output."""
        return (
            f"  {self.file}:{self.line}: "
            f"except {self.exception_type}: {self.handler_body}"
        )


def _is_pass_stmt(node: ast.stmt) -> bool:
    """Check if a statement is a bare ``pass``.

    Args:
        node: The AST statement node to check.

    Returns:
        True if the node is an ``ast.Pass`` statement.

    """
    return isinstance(node, ast.Pass)


def _is_continue_stmt(node: ast.stmt) -> bool:
    """Check if a statement is a bare ``continue``.

    Args:
        node: The AST statement node to check.

    Returns:
        True if the node is an ``ast.Continue`` statement.

    """
    return isinstance(node, ast.Continue)


def _is_return_none(node: ast.stmt) -> bool:
    """Check if a statement is ``return`` or ``return None``.

    Args:
        node: The AST statement node to check.

    Returns:
        True if the node is a bare return or ``return None``.

    """
    if not isinstance(node, ast.Return):
        return False
    return node.value is None or (
        isinstance(node.value, ast.Constant) and node.value.value is None
    )


def _is_ellipsis_stmt(node: ast.stmt) -> bool:
    """Check if a statement is an ellipsis (``...``).

    Args:
        node: The AST statement node to check.

    Returns:
        True if the node is an ``ast.Expr`` containing ``ast.Constant(Ellipsis)``.

    """
    return (
        isinstance(node, ast.Expr)
        and isinstance(node.value, ast.Constant)
        and node.value.value is Ellipsis
    )


def _describe_handler_body(body: list[ast.stmt]) -> str:
    """Describe the handler body for the violation message.

    Examines the statement list and returns a human-readable description
    of the silencing pattern found (e.g., ``pass``, ``continue``,
    ``return None``).

    Args:
        body: The list of statements in the except handler.

    Returns:
        Human-readable description of the silencing pattern.

    """
    if not body:
        return "empty body"
    stmt = body[0]
    if _is_pass_stmt(stmt):
        return "pass"
    if _is_continue_stmt(stmt):
        return "continue"
    if _is_return_none(stmt):
        return "return None"
    if _is_ellipsis_stmt(stmt):
        return "..."
    return "unknown"


def _get_exception_type(handler: ast.ExceptHandler) -> str:
    """Extract the exception type name from an except handler.

    Args:
        handler: The AST ``ExceptHandler`` node.

    Returns:
        The exception type as a string, or ``(bare except)`` for
        handlers without a type specification.

    """
    if handler.type is None:
        return "(bare except)"
    if isinstance(handler.type, ast.Name):
        return handler.type.id
    if isinstance(handler.type, ast.Attribute):
        return ast.dump(handler.type)
    if isinstance(handler.type, ast.Tuple):
        names: list[str] = []
        for elt in handler.type.elts:
            if isinstance(elt, ast.Name):
                names.append(elt.id)
            else:
                names.append(ast.dump(elt))
        return f"({', '.join(names)})"
    return ast.dump(handler.type)


class CatchSilenceVisitor(ast.NodeVisitor):
    """AST visitor to detect catch-and-silence anti-patterns.

    Walks the AST looking for ``except`` handlers where the body consists
    solely of a silencing statement: ``pass``, ``continue``, ``return None``,
    or ``...`` (ellipsis). These patterns silently swallow exceptions, hiding
    bugs and violating ETHOS §23.
    """

    def __init__(self, filepath: Path) -> None:
        """Initialize the visitor.

        Args:
            filepath: Path to the file being checked.

        """
        self.filepath = filepath
        self.violations: list[SilenceViolation] = []

    def visit_ExceptHandler(self, node: ast.ExceptHandler) -> None:
        """Check an except handler for silencing patterns.

        An except handler is flagged if its body contains exactly one
        statement and that statement is ``pass``, ``continue``,
        ``return None``, or ``...``.

        Args:
            node: The ``ExceptHandler`` AST node.

        """
        body = node.body
        # Filter out docstrings (Expr with Constant string)
        effective_body = [
            stmt
            for stmt in body
            if not (
                isinstance(stmt, ast.Expr)
                and isinstance(stmt.value, ast.Constant)
                and isinstance(stmt.value.value, str)
            )
        ]

        if len(effective_body) == 1:
            stmt = effective_body[0]
            if (
                _is_pass_stmt(stmt)
                or _is_continue_stmt(stmt)
                or _is_return_none(stmt)
                or _is_ellipsis_stmt(stmt)
            ):
                self.violations.append(
                    SilenceViolation(
                        file=self.filepath,
                        line=node.lineno,
                        exception_type=_get_exception_type(node),
                        handler_body=_describe_handler_body(effective_body),
                    )
                )

        self.generic_visit(node)


def check_file(filepath: Path) -> list[SilenceViolation]:
    """Check a Python file for catch-and-silence anti-patterns.

    Parses the file as an AST and walks it looking for ``except``
    handlers that silently swallow exceptions.

    Args:
        filepath: Path to the Python file to check.

    Returns:
        List of violations found in the file.

    """
    content = filepath.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(filepath))
    visitor = CatchSilenceVisitor(filepath)
    visitor.visit(tree)
    return visitor.violations


def main() -> int:
    """Check Python files for catch-and-silence anti-patterns.

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

    all_violations: list[SilenceViolation] = []

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
    sys.stderr.write("CATCH-AND-SILENCE CHECK FAILED (ETHOS §23)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Per ETHOS §23: exceptions must never be silently swallowed.\n"
        "Every except handler must handle, transform+re-raise, or\n"
        "log+re-raise the exception.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for violation in all_violations:
        sys.stderr.write(violation.render() + "\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  Replace silencing patterns with proper handling:\n")
    sys.stderr.write("    except SomeError as exc:\n")
    sys.stderr.write('        logger.warning("operation_failed", error=str(exc))\n')
    sys.stderr.write("        raise  # or raise DifferentError(...) from exc\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
