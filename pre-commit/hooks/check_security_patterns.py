#!/usr/bin/env python3
"""Pre-commit hook to detect security anti-patterns in Python code.

This enforces:
- ETHOS §24 (Security by Design): No SQL injection vectors via f-strings
- ETHOS §24 (Security by Design): No default secrets in os.getenv()
- ETHOS §9 (No Inline CLI Environment Variables): No os.environ assignment in tests

Usage:
    Pre-commit: python pre-commit/hooks/check_security_patterns.py [files...]

Exit codes:
    0: All checks passed
    1: Security anti-patterns detected
"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final

MIN_REQUIRED_ARGS: Final[int] = 2
_MIN_GETENV_ARGS_WITH_DEFAULT: Final[int] = 2


@dataclass(frozen=True)
class SecurityViolation:
    """Represents a security anti-pattern finding."""

    file: Path
    line: int
    category: str
    message: str
    code_snippet: str

    def render(self) -> str:
        """Render the violation for console output."""
        return f"{self.file}:{self.line}: [{self.category}] {self.message}"


class SecurityPatternVisitor(ast.NodeVisitor):
    """AST visitor to detect security anti-patterns."""

    # SQL keywords that indicate potential injection when used in f-strings
    SQL_KEYWORDS: Final[frozenset[str]] = frozenset(
        {
            "SELECT",
            "INSERT",
            "UPDATE",
            "DELETE",
            "DROP",
            "CREATE",
            "ALTER",
            "TRUNCATE",
            "EXECUTE",
            "EXEC",
        }
    )

    # Suspicious patterns in default values that suggest secrets
    SECRET_PATTERNS: Final[tuple[str, ...]] = (
        "sk-",  # OpenAI/Stripe style keys
        "pk-",  # Public keys
        "api_",
        "key_",
        "token_",
        "secret_",
        "password",
        "passwd",
        "credential",
    )

    def __init__(self, file_path: Path, source_lines: list[str]) -> None:
        """Initialize the visitor.

        Args:
            file_path: Path to the file being analyzed.
            source_lines: Source code split by lines for snippet extraction.

        """
        self.file_path = file_path
        self.source_lines = source_lines
        self.violations: list[SecurityViolation] = []
        self.is_test_file = self._is_test_file(file_path)

    def _is_test_file(self, path: Path) -> bool:
        """Check if a file is a test file."""
        name = path.name
        return (
            name.startswith("test_")
            or name.endswith("_test.py")
            or "tests" in path.parts
            or "conftest" in name
        )

    def _get_snippet(self, lineno: int) -> str:
        """Get the source line for a given line number."""
        if 1 <= lineno <= len(self.source_lines):
            return self.source_lines[lineno - 1].strip()
        return "<unknown>"

    def _check_sql_fstring(self, node: ast.JoinedStr) -> None:
        """Check if an f-string contains SQL keywords (injection risk).

        Args:
            node: The f-string AST node.

        """
        # Extract string parts from the f-string using list comprehension
        string_parts = [
            value.value
            for value in node.values
            if isinstance(value, ast.Constant) and isinstance(value.value, str)
        ]

        combined = " ".join(string_parts).upper()

        # Check if any SQL keyword appears at the start of a statement
        # Must be followed by space/punctuation to avoid partial matches
        stripped = combined.lstrip()
        for keyword in self.SQL_KEYWORDS:
            if stripped.startswith(keyword):
                # Check for word boundary - keyword must be followed by non-alphanumeric
                rest = stripped[len(keyword) :]
                if not rest or not rest[0].isalnum():
                    self.violations.append(
                        SecurityViolation(
                            file=self.file_path,
                            line=node.lineno,
                            category="SQL_INJECTION",
                            message=f"F-string appears to contain SQL ({keyword}...). "
                            "Use parameterized queries instead.",
                            code_snippet=self._get_snippet(node.lineno),
                        )
                    )
                    return  # Only report once per f-string

    def _is_getenv_call(self, func: ast.expr) -> bool:
        """Check if a function call is os.getenv() or os.environ.get().

        Args:
            func: The function expression from the call node.

        Returns:
            True if this is a getenv-style call.

        """
        if not isinstance(func, ast.Attribute):
            return False
        if func.attr not in {"getenv", "get"}:
            return False
        # Check for os.getenv
        if isinstance(func.value, ast.Name) and func.value.id == "os":
            return True
        # Check for os.environ.get
        return isinstance(func.value, ast.Attribute) and func.value.attr == "environ"

    def _get_default_value(self, node: ast.Call) -> ast.expr:
        """Extract default value from getenv-style call.

        Args:
            node: The function call AST node.

        Returns:
            The default value expression.

        Raises:
            LookupError: If no default value is present in the call.

        """
        # Second positional argument
        if len(node.args) >= _MIN_GETENV_ARGS_WITH_DEFAULT:
            return node.args[1]
        # Or 'default' keyword argument
        for keyword in node.keywords:
            if keyword.arg == "default":
                return keyword.value
        msg = "No default value in getenv call"
        raise LookupError(msg)

    def _is_suspicious_default(self, value: str) -> bool:
        """Check if a default value looks like a secret.

        Args:
            value: The string value to check.

        Returns:
            True if the value looks like it might be a secret.

        """
        value_lower = value.lower()
        return any(pattern in value_lower for pattern in self.SECRET_PATTERNS)

    def _check_getenv_default_secret(self, node: ast.Call) -> None:
        """Check for os.getenv() with suspicious default values.

        Args:
            node: The function call AST node.

        """
        if not self._is_getenv_call(node.func):
            return

        try:
            default_value = self._get_default_value(node)
        except LookupError:
            return

        # Check if default looks like a secret
        if not isinstance(default_value, ast.Constant):
            return
        if not isinstance(default_value.value, str):
            return

        if self._is_suspicious_default(default_value.value):
            self.violations.append(
                SecurityViolation(
                    file=self.file_path,
                    line=node.lineno,
                    category="DEFAULT_SECRET",
                    message="os.getenv() has default value that looks like a secret. "
                    "Secrets must come from environment with no defaults.",
                    code_snippet=self._get_snippet(node.lineno),
                )
            )

    def _is_os_environ_subscript(self, node: ast.Subscript) -> bool:
        """Check if a subscript node is os.environ[...].

        Args:
            node: The subscript AST node.

        Returns:
            True if this is os.environ subscript access.

        """
        if not isinstance(node.value, ast.Attribute):
            return False
        if node.value.attr != "environ":
            return False
        if not isinstance(node.value.value, ast.Name):
            return False
        return node.value.value.id == "os"

    def _check_environ_assignment(self, node: ast.Subscript) -> None:
        """Check for os.environ["VAR"] = ... assignment (test bypass).

        This is only flagged in test files per ETHOS §9.

        Args:
            node: The subscript AST node (target of assignment).

        """
        if not self.is_test_file:
            return

        if not self._is_os_environ_subscript(node):
            return

        self.violations.append(
            SecurityViolation(
                file=self.file_path,
                line=node.lineno,
                category="TEST_ENV_BYPASS",
                message="os.environ assignment in test file bypasses "
                "bootstrap validation. Use fixtures that call bootstrap().",
                code_snippet=self._get_snippet(node.lineno),
            )
        )

    def visit_JoinedStr(self, node: ast.JoinedStr) -> None:
        """Visit f-string nodes."""
        self._check_sql_fstring(node)
        self.generic_visit(node)

    def visit_Call(self, node: ast.Call) -> None:
        """Visit function call nodes."""
        self._check_getenv_default_secret(node)
        self.generic_visit(node)

    def visit_Assign(self, node: ast.Assign) -> None:
        """Visit assignment nodes."""
        for target in node.targets:
            if isinstance(target, ast.Subscript):
                self._check_environ_assignment(target)
        self.generic_visit(node)


def check_file(path: Path) -> tuple[list[SecurityViolation], str]:
    """Check a Python file for security anti-patterns.

    Args:
        path: Path to the Python file.

    Returns:
        Tuple of (violations list, error message).
        Error message is empty string if file was processed successfully.

    """
    try:
        content = path.read_text(encoding="utf-8")
    except OSError as exc:
        return [], f"Could not read file: {exc}"

    try:
        tree = ast.parse(content, filename=str(path))
    except SyntaxError as exc:
        return [], f"Syntax error at line {exc.lineno}: {exc.msg}"

    source_lines = content.splitlines()
    visitor = SecurityPatternVisitor(path, source_lines)
    visitor.visit(tree)

    return visitor.violations, ""


# Category descriptions for output
CATEGORY_DESCRIPTIONS: dict[str, tuple[str, str]] = {
    "SQL_INJECTION": (
        "SQL Injection Risk (ETHOS §24):",
        "Use parameterized queries instead of f-strings for SQL.",
    ),
    "DEFAULT_SECRET": (
        "Default Secret Values (ETHOS §24):",
        "Remove default values from secret-related getenv() calls.",
    ),
    "TEST_ENV_BYPASS": (
        "Test Environment Bypass (ETHOS §9):",
        "Use fixtures that call bootstrap() instead of direct env assignment.",
    ),
}


def _print_file_errors(file_errors: list[tuple[Path, str]]) -> None:
    """Print file processing errors."""
    print("=" * 60)
    print("FILE PROCESSING ERRORS")
    print("=" * 60)
    for path, error in file_errors:
        print(f"  {path}: {error}")
    print()


def _print_violations(violations: list[SecurityViolation]) -> None:
    """Print security violations grouped by category."""
    print("=" * 60)
    print("SECURITY ANTI-PATTERNS DETECTED")
    print("=" * 60)
    print()

    by_category: dict[str, list[SecurityViolation]] = {}
    for v in violations:
        by_category.setdefault(v.category, []).append(v)

    for category, category_violations in sorted(by_category.items()):
        title, description = CATEGORY_DESCRIPTIONS.get(
            category, (f"{category}:", "Security issue detected.")
        )
        print(title)
        print(f"  {description}")
        print()

        for v in category_violations:
            print(f"  {v.render()}")
            print(f"    > {v.code_snippet}")
        print()

    print("=" * 60)


def main() -> int:
    """Scan files for security anti-patterns."""
    if len(sys.argv) < MIN_REQUIRED_ARGS:
        sys.stderr.write("No files to check\n")
        return 0

    files = [
        Path(f) for f in sys.argv[1:] if Path(f).suffix == ".py" and Path(f).exists()
    ]
    if not files:
        return 0

    all_violations: list[SecurityViolation] = []
    file_errors: list[tuple[Path, str]] = []

    for path in files:
        violations, error = check_file(path)
        if error:
            file_errors.append((path, error))
        all_violations.extend(violations)

    if file_errors:
        _print_file_errors(file_errors)

    if all_violations:
        _print_violations(all_violations)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
