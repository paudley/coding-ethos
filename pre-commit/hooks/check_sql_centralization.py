#!/usr/bin/env python3
"""Pre-commit hook to ban SQL strings outside the configured SQL module.

All SQL, DDL, DML, and Cypher query strings must live in the configured
central SQL module. Other modules import named constants and
query-builder functions from that module and must never embed raw SQL
in string literals.

Detection strategy:
    AST-walk each file for ``ast.Constant`` (plain strings) and
    ``ast.JoinedStr`` (f-strings). Check for compound SQL patterns —
    multi-word regex that matches actual SQL syntax structure (e.g.,
    ``SELECT ... FROM``, ``INSERT INTO``, ``CREATE TABLE``). Single
    keywords like "CREATE" are not sufficient because they appear in
    English error messages ("Failed to create entity").

Exemptions:
    - Files under the configured SQL module path.
    - Files whose path or name contains ``alembic`` (migration scripts
      necessarily embed SQL).
    - Strings shorter than 15 characters (too short to be meaningful SQL).
    - Docstrings (AST position check — documentation may discuss SQL).

File filtering for tests and .venv is handled by pre-commit configuration
(exclude directive).

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_sql_centralization.py [files...]

Exit codes:
    0: No SQL strings found outside the configured SQL module
    1: One or more files contain SQL strings that should be centralized
"""

import ast
import re
import sys
from pathlib import Path
from typing import Final, NamedTuple

from hook_config import get_bool, get_list, get_str


class SQLViolation(NamedTuple):
    """Record of a SQL string found outside the configured SQL module."""

    file: Path
    line: int
    pattern: str
    snippet: str


# Minimum number of args (script name + at least one file).
MIN_ARGS: Final[int] = 2

# Maximum snippet length for display in violation messages.
MAX_SNIPPET_LENGTH: Final[int] = 80

# Compound SQL patterns — multi-word regex that matches actual SQL syntax.
# Single keywords like "CREATE" are NOT checked because they appear in
# English prose ("Failed to create entity"). These compound patterns
# require SQL grammar structure that never appears in error messages.
#
# Each tuple: (display_name, compiled_regex)
# All patterns use re.IGNORECASE and re.DOTALL for multiline strings.
_SQL_COMPOUND_PATTERNS: Final[list[tuple[str, re.Pattern[str]]]] = [
    # DML patterns
    ("SELECT...FROM", re.compile(r"\bSELECT\b.+\bFROM\b", re.IGNORECASE | re.DOTALL)),
    ("INSERT INTO", re.compile(r"\bINSERT\s+INTO\b", re.IGNORECASE)),
    ("DELETE FROM", re.compile(r"\bDELETE\s+FROM\b", re.IGNORECASE)),
    ("UPDATE...SET", re.compile(r"\bUPDATE\b.+\bSET\b", re.IGNORECASE | re.DOTALL)),
    # DDL patterns
    ("CREATE TABLE", re.compile(r"\bCREATE\s+TABLE\b", re.IGNORECASE)),
    ("CREATE INDEX", re.compile(r"\bCREATE\s+(UNIQUE\s+)?INDEX\b", re.IGNORECASE)),
    ("CREATE EXTENSION", re.compile(r"\bCREATE\s+EXTENSION\b", re.IGNORECASE)),
    ("CREATE OR REPLACE", re.compile(r"\bCREATE\s+OR\s+REPLACE\b", re.IGNORECASE)),
    ("CREATE POLICY", re.compile(r"\bCREATE\s+POLICY\b", re.IGNORECASE)),
    ("CREATE GRAPH", re.compile(r"\bCREATE\s+GRAPH\b", re.IGNORECASE)),
    ("ALTER TABLE", re.compile(r"\bALTER\s+TABLE\b", re.IGNORECASE)),
    ("DROP TABLE", re.compile(r"\bDROP\s+TABLE\b", re.IGNORECASE)),
    ("DROP INDEX", re.compile(r"\bDROP\s+INDEX\b", re.IGNORECASE)),
    ("DROP POLICY", re.compile(r"\bDROP\s+POLICY\b", re.IGNORECASE)),
    ("DROP GRAPH", re.compile(r"\bDROP\s+GRAPH\b", re.IGNORECASE)),
    ("TRUNCATE", re.compile(r"\bTRUNCATE\s+\w+", re.IGNORECASE)),
    # Security patterns
    ("ENABLE RLS", re.compile(r"\bENABLE\s+ROW\s+LEVEL\s+SECURITY\b", re.IGNORECASE)),
    ("FORCE RLS", re.compile(r"\bFORCE\s+ROW\s+LEVEL\s+SECURITY\b", re.IGNORECASE)),
    ("GRANT...ON", re.compile(r"\bGRANT\b.+\bON\b", re.IGNORECASE | re.DOTALL)),
    ("REVOKE...ON", re.compile(r"\bREVOKE\b.+\bON\b", re.IGNORECASE | re.DOTALL)),
    # Session patterns
    ("SET LOCAL", re.compile(r"\bSET\s+LOCAL\b", re.IGNORECASE)),
    ("SET SEARCH_PATH", re.compile(r"\bSET\s+SEARCH_PATH\b", re.IGNORECASE)),
    ("LOAD extension", re.compile(r"\bLOAD\s+'", re.IGNORECASE)),
    # PL/pgSQL patterns
    ("EXECUTE format", re.compile(r"\bEXECUTE\s+format\b", re.IGNORECASE)),
    # Cypher graph query patterns (Apache AGE)
    ("Cypher CREATE", re.compile(r"\bCREATE\s*\(", re.IGNORECASE)),
    ("Cypher MATCH", re.compile(r"\bMATCH\s*\(", re.IGNORECASE)),
    ("Cypher MERGE", re.compile(r"\bMERGE\s*\(", re.IGNORECASE)),
    ("Cypher RETURN", re.compile(r"\bRETURN\s+id\s*\(", re.IGNORECASE)),
    # Parameterized query markers ($1, $2, etc.)
    ("Parameterized $N", re.compile(r"\$\d+")),
    # Structural SQL patterns
    ("VALUES(...)", re.compile(r"\bVALUES\s*\(", re.IGNORECASE)),
    ("IF NOT EXISTS", re.compile(r"\bIF\s+NOT\s+EXISTS\b", re.IGNORECASE)),
    ("IF EXISTS", re.compile(r"\bIF\s+EXISTS\b", re.IGNORECASE)),
    ("WHERE clause", re.compile(r"\bWHERE\b.+[=<>]", re.IGNORECASE | re.DOTALL)),
]


def _module_name() -> str:
    configured = get_str("python.sql_centralization.module_name", "project.sql").strip()
    return configured or "project.sql"


def _central_paths() -> list[str]:
    return [
        str(item).strip()
        for item in get_list("python.sql_centralization.central_paths", [])
        if str(item).strip()
    ]


def _central_path_hint() -> str:
    central_paths = _central_paths()
    if central_paths:
        return central_paths[0]
    return _module_name().replace(".", "/")


def _is_exempt_path(path: Path) -> bool:
    """Check if a file is in an exempt directory or has an exempt name.

    Args:
        path: Path to the Python file.

    Returns:
        True if the file is inside the configured SQL module or is a migration.

    """
    path_str = str(path)
    migration_markers = [
        str(item).strip()
        for item in get_list(
            "python.sql_centralization.migration_markers",
            ["alembic", "migrations"],
        )
        if str(item).strip()
    ]
    return any(marker in path_str for marker in [*_central_paths(), *migration_markers])


def _truncate(text: str, max_length: int = MAX_SNIPPET_LENGTH) -> str:
    """Truncate text for display, adding ellipsis if needed.

    Args:
        text: The text to truncate.
        max_length: Maximum length before truncation.

    Returns:
        Truncated text with ellipsis, or original if short enough.

    """
    collapsed = " ".join(text.split())
    if len(collapsed) <= max_length:
        return collapsed
    return collapsed[: max_length - 3] + "..."


def _find_sql_patterns(text: str) -> list[str]:
    """Find all compound SQL patterns present in a string.

    Uses multi-word regex patterns that match actual SQL syntax
    structure, not individual keywords. This avoids false positives
    on English prose like "Failed to create entity".

    Args:
        text: The string to search for SQL patterns.

    Returns:
        List of matched pattern display names.

    """
    return [name for name, pattern in _SQL_COMPOUND_PATTERNS if pattern.search(text)]


def _extract_fstring_text(node: ast.JoinedStr) -> str:
    """Extract the literal text portions of an f-string for analysis.

    Only examines the constant parts of the f-string, not the
    interpolated expressions.

    Args:
        node: AST JoinedStr (f-string) node.

    Returns:
        Concatenated literal text from the f-string.

    """
    parts: list[str] = [
        value.value
        for value in node.values
        if isinstance(value, ast.Constant) and isinstance(value.value, str)
    ]
    return " ".join(parts)


def _check_constant(node: ast.Constant, *, path: Path) -> list[SQLViolation]:
    """Check a string constant for compound SQL patterns.

    Args:
        node: AST Constant node containing a string value.
        path: Source file path (for violation records).

    Returns:
        List of violations found in this string constant.

    """
    if not isinstance(node.value, str):
        return []
    text = node.value
    matched = _find_sql_patterns(text)
    if not matched:
        return []

    return [
        SQLViolation(
            file=path,
            line=node.lineno,
            pattern=matched[0],
            snippet=_truncate(text),
        ),
    ]


def _check_fstring(node: ast.JoinedStr, *, path: Path) -> list[SQLViolation]:
    """Check an f-string for compound SQL patterns.

    Args:
        node: AST JoinedStr (f-string) node.
        path: Source file path (for violation records).

    Returns:
        List of violations found in this f-string.

    """
    text = _extract_fstring_text(node)
    matched = _find_sql_patterns(text)
    if not matched:
        return []

    return [
        SQLViolation(
            file=path,
            line=node.lineno,
            pattern=matched[0],
            snippet=_truncate(text),
        ),
    ]


def _is_error_context_kwarg(node: ast.AST, parent_map: dict[int, ast.AST]) -> bool:
    """Check if a Constant is a keyword argument for error/log context.

    Strings passed as ``suggestion=``, ``reason=``, ``message=``, or
    ``match=`` keyword arguments are human-readable error context that
    may reference SQL syntax without being actual queries. These should
    not be flagged.

    Args:
        node: The AST node to check.
        parent_map: Mapping from node id to parent node.

    Returns:
        True if the node is a value for an error-context keyword argument.

    """
    parent_id = id(node)
    if parent_id not in parent_map:
        return False
    parent = parent_map[parent_id]
    if not isinstance(parent, ast.keyword):
        return False
    return parent.arg in {"suggestion", "reason", "message", "match", "extra"}


def _is_docstring_or_standalone_string(
    node: ast.AST, parent_map: dict[int, ast.AST]
) -> bool:
    """Check if a Constant is a docstring or standalone string expression.

    Catches both standard docstrings (first expression in a body) and
    variable docstrings (string expression after an assignment, used by
    Sphinx and other doc tools to document module-level variables).
    These may reference SQL keywords in documentation context.

    Args:
        node: The AST node to check.
        parent_map: Mapping from node id to parent node.

    Returns:
        True if the node is a docstring or standalone string expression.

    """
    parent_id = id(node)
    if parent_id not in parent_map or not isinstance(parent_map[parent_id], ast.Expr):
        return False
    parent = parent_map[parent_id]

    grandparent_id = id(parent)
    if grandparent_id not in parent_map:
        return False
    grandparent = parent_map[grandparent_id]

    if not isinstance(
        grandparent,
        (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef),
    ):
        return False
    body = grandparent.body

    # Standard docstring: first statement in a body
    if body and body[0] is parent:
        return True

    # Variable docstring: string expression that follows an assignment.
    # Pattern: `MY_VAR = value` followed by `"""docstring"""`.
    # Used by Sphinx autodoc and other documentation tools.
    return any(
        stmt is parent
        and i > 0
        and isinstance(body[i - 1], (ast.Assign, ast.AnnAssign))
        for i, stmt in enumerate(body)
    )


def _build_parent_map(tree: ast.Module) -> dict[int, ast.AST]:
    """Build a mapping from child node id to parent node.

    Args:
        tree: The root AST module.

    Returns:
        Dictionary mapping id(child) to parent node.

    """
    parent_map: dict[int, ast.AST] = {}
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            parent_map[id(child)] = parent
    return parent_map


def find_sql_strings(path: Path) -> list[SQLViolation]:
    """Find SQL string literals in a Python file.

    Scans string constants and f-strings for compound SQL patterns.
    Skips docstrings and short strings. Uses multi-word patterns to
    avoid false positives on English prose.

    Args:
        path: Path to the Python file.

    Returns:
        List of SQLViolation records.

    Raises:
        OSError: If the file cannot be read.
        SyntaxError: If the file cannot be parsed as Python.

    """
    if _is_exempt_path(path):
        return []

    content = path.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(path))
    parent_map = _build_parent_map(tree)

    violations: list[SQLViolation] = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Constant) and isinstance(node.value, str):
            if _is_docstring_or_standalone_string(node, parent_map):
                continue
            if _is_error_context_kwarg(node, parent_map):
                continue
            violations.extend(_check_constant(node, path=path))
        elif isinstance(node, ast.JoinedStr):
            violations.extend(_check_fstring(node, path=path))

    return violations


def main() -> int:
    """Scan files for SQL strings outside the configured SQL module.

    Pre-commit passes only files matching the configured types and
    exclusions. Check all files passed to us.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_ARGS:
        return 0

    if not get_bool("python.sql_centralization.enabled", False):
        return 0

    files_to_check = [Path(arg) for arg in sys.argv[1:] if Path(arg).exists()]

    if not files_to_check:
        return 0

    all_violations: list[SQLViolation] = []

    for filepath in files_to_check:
        try:
            violations = find_sql_strings(filepath)
            all_violations.extend(violations)
        except SyntaxError as exc:
            sys.stderr.write(f"  skipping {filepath}: {exc}\n")
            continue

    if not all_violations:
        return 0

    module_name = _module_name()
    central_path_hint = _central_path_hint()

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write(f"SQL STRINGS FOUND OUTSIDE {module_name}\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        f"All SQL, DDL, DML, and Cypher strings must live in {module_name}.\n"
        f"Other modules import named constants from {module_name}.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in all_violations:
        sys.stderr.write(f"  {v.file}:{v.line}: [{v.pattern}] {v.snippet}\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write(
        f"  1. Move the SQL string to {central_path_hint} as a Final[str] constant\n"
    )
    sys.stderr.write(f"  2. Import it: from {module_name} import MY_QUERY\n")
    sys.stderr.write(
        f"  3. For dynamic queries, create a builder function in {module_name}\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
