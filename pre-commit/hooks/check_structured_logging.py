#!/usr/bin/env python3
"""Pre-commit hook to enforce structured logging in Python code.

This enforces ETHOS §11 (Radical Visibility): every logger call must
include keyword arguments for structured context. Bare string messages
without context are insufficient for production observability — they
create log lines that are human-readable but machine-opaque.

Allowed patterns:
    - ``logger.info("event.name", key=value)`` — structured context.
    - ``logger.error("message", exc_info=True)`` — exception context.
    - ``logger.exception("message")`` — always includes exc_info.

Banned patterns:
    - ``logger.info("bare message")`` — no structured context.
    - ``logger.debug("event %s", value)`` — old-style % formatting.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_structured_logging.py [files...]

Exit codes:
    0: All checks passed
    1: Unstructured logging patterns detected

"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final

MIN_REQUIRED_ARGS: Final[int] = 2

# Maximum length for message preview in violation output.
_MAX_PREVIEW_LENGTH: Final[int] = 60

# Logger methods that require structured context.
_LOGGER_METHODS: Final[frozenset[str]] = frozenset(
    {
        "debug",
        "info",
        "warning",
        "error",
        "critical",
    }
)

# Keyword arguments that provide structured context (exempt from check).
# exc_info is the standard way to attach exception tracebacks.
_EXEMPT_KWARGS: Final[frozenset[str]] = frozenset(
    {
        "exc_info",
        "stack_info",
        "stacklevel",
    }
)


@dataclass(frozen=True)
class LoggingViolation:
    """Represents an unstructured logging finding."""

    file: Path
    line: int
    method: str
    message_preview: str

    def render(self) -> str:
        """Render the violation for console output."""
        preview = self.message_preview[:_MAX_PREVIEW_LENGTH]
        if len(self.message_preview) > _MAX_PREVIEW_LENGTH:
            preview += "..."
        return (
            f"  {self.file}:{self.line}: "
            f"logger.{self.method}({preview!r}) — no structured context"
        )


def _is_logger_call(node: ast.Call) -> tuple[bool, str]:
    """Check if an AST Call node is a logger method call.

    Detects patterns like ``logger.info(...)``, ``self.logger.debug(...)``,
    ``self._logger.warning(...)``, and ``_logger.error(...)``.

    Args:
        node: The ``Call`` AST node to check.

    Returns:
        Tuple of (is_logger_call, method_name).

    """
    if not isinstance(node.func, ast.Attribute):
        return False, ""

    method = node.func.attr
    if method not in _LOGGER_METHODS:
        return False, ""

    # Check if the object is a logger-like name
    value = node.func.value
    if isinstance(value, ast.Name):
        name = value.id
        if name in {"logger", "_logger", "log", "_log"}:
            return True, method
    elif isinstance(value, ast.Attribute):
        # self.logger, self._logger
        attr = value.attr
        if attr in {"logger", "_logger", "log", "_log"}:
            return True, method

    return False, ""


def _has_structured_context(node: ast.Call) -> bool:
    """Check if a logger call includes structured context kwargs.

    A logger call has structured context if it includes at least one
    keyword argument that is NOT in the exempt set (exc_info, stack_info,
    stacklevel). These exempt kwargs are logging infrastructure, not
    structured business context.

    Args:
        node: The ``Call`` AST node to check.

    Returns:
        True if the call includes non-exempt keyword arguments.

    """
    for kw in node.keywords:
        if kw.arg is not None and kw.arg not in _EXEMPT_KWARGS:
            return True
    return False


def _uses_percent_formatting(node: ast.Call) -> bool:
    """Check if a logger call uses old-style percent formatting.

    Detects ``logger.info("message %s", value)`` patterns where
    positional arguments after the message string indicate % formatting.

    Args:
        node: The ``Call`` AST node to check.

    Returns:
        True if the call uses % formatting (>1 positional args).

    """
    return len(node.args) > 1


def _extract_message_preview(node: ast.Call) -> str:
    """Extract the message string preview from a logger call.

    Attempts to extract the literal string from the first positional
    argument for display in violation messages.

    Args:
        node: The ``Call`` AST node.

    Returns:
        The message string preview, or a placeholder description.

    """
    if not node.args:
        return "<no message>"
    first_arg = node.args[0]
    if isinstance(first_arg, ast.Constant) and isinstance(first_arg.value, str):
        return first_arg.value
    if isinstance(first_arg, ast.JoinedStr):
        return "<f-string>"
    return "<dynamic>"


class StructuredLoggingVisitor(ast.NodeVisitor):
    """AST visitor to detect unstructured logging patterns.

    Walks the AST looking for logger calls that lack keyword arguments
    for structured context. These calls produce log lines that are
    human-readable but machine-opaque, violating ETHOS §11.
    """

    def __init__(self, filepath: Path) -> None:
        """Initialize the visitor.

        Args:
            filepath: Path to the file being checked.

        """
        self.filepath = filepath
        self.violations: list[LoggingViolation] = []

    def visit_Call(self, node: ast.Call) -> None:
        """Check a function call for unstructured logging.

        A logger call is flagged if it:
        1. Has no keyword arguments (other than exempt ones).
        2. Uses old-style percent formatting (>1 positional args).

        ``logger.exception()`` is exempt because it always includes
        exception context via ``exc_info=True`` implicitly.

        Args:
            node: The ``Call`` AST node.

        """
        is_logger, method = _is_logger_call(node)
        if not is_logger:
            self.generic_visit(node)
            return

        # logger.exception() is always exempt (implicit exc_info=True)
        if method == "exception":
            self.generic_visit(node)
            return

        has_context = _has_structured_context(node)
        uses_percent = _uses_percent_formatting(node)

        if not has_context or uses_percent:
            self.violations.append(
                LoggingViolation(
                    file=self.filepath,
                    line=node.lineno,
                    method=method,
                    message_preview=_extract_message_preview(node),
                )
            )

        self.generic_visit(node)


def check_file(filepath: Path) -> list[LoggingViolation]:
    """Check a Python file for unstructured logging patterns.

    Parses the file as an AST and walks it looking for logger calls
    that lack structured context keyword arguments.

    Args:
        filepath: Path to the Python file to check.

    Returns:
        List of violations found in the file.

    """
    content = filepath.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(filepath))
    visitor = StructuredLoggingVisitor(filepath)
    visitor.visit(tree)
    return visitor.violations


def main() -> int:
    """Check Python files for unstructured logging patterns.

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

    all_violations: list[LoggingViolation] = []

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
    sys.stderr.write("STRUCTURED LOGGING CHECK FAILED (ETHOS §11)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Per ETHOS §11 (Radical Visibility): every logger call must\n"
        "include keyword arguments for structured context. Bare string\n"
        "messages are insufficient for production observability.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write(f"Violations found ({len(all_violations)}):\n")

    for violation in all_violations:
        sys.stderr.write(violation.render() + "\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  Add keyword arguments for structured context:\n")
    sys.stderr.write('    logger.info("event.name", key=value, other=data)\n')
    sys.stderr.write("\n")
    sys.stderr.write("  For exceptions, use exc_info or logger.exception():\n")
    sys.stderr.write(
        '    logger.error("operation.failed", error=str(exc), exc_info=True)\n'
    )
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
