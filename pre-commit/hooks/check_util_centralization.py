#!/usr/bin/env python3
"""Pre-commit hook to enforce utility module centralization.

Production code must use project wrapper modules instead of importing
utility libraries directly. This keeps behavior centralized, improves
type safety, and gives the repository one policy surface per utility
domain.

The actual bans, exemptions, and suggested replacements come from
``python.util_centralization.banned_modules`` in the bundle's repo-root
``config.yaml`` plus any consuming-repo override config.

Dotted module bans (e.g., ``google.genai``) match both direct dotted
imports (``from google.genai import types``) and parent-level imports
that pull in the banned module (``from google import genai``).

Auto-exemptions:
    Files inside a module's exempt path are the wrapper implementation
    itself and may import the underlying library directly.

File filtering is handled by pre-commit configuration (exclude: /tests/).

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_util_centralization.py [files...]

Exit codes:
    0: No banned imports found
    1: One or more files have banned direct imports

"""

import ast
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final, NamedTuple

from hook_config import get_bool, get_list


class ImportViolation(NamedTuple):
    """Record of a banned direct import violation."""

    file: Path
    line: int
    import_statement: str
    alternative: str


# Minimum number of args (script name + at least one file).
MIN_ARGS: Final[int] = 2


@dataclass(frozen=True, slots=True)
class BannedModule:
    """A module banned from direct import with its exemption paths.

    Attributes:
        name: Module name as it appears in import statements.
        exempt_paths: Path fragments that identify wrapper implementations.
            Files whose path contains any of these fragments are exempt.
            Empty tuple means the module is banned everywhere.
        alternative: Suggested replacement import for error messages.

    """

    name: str
    exempt_paths: tuple[str, ...]
    alternative: str


def _banned_modules() -> tuple[BannedModule, ...]:
    items: list[BannedModule] = []
    for raw in get_list("python.util_centralization.banned_modules", []):
        if not isinstance(raw, dict):
            continue
        name = str(raw.get("module", "")).strip()
        alternative = str(raw.get("alternative", "")).strip()
        exempt_paths = tuple(
            str(item).strip()
            for item in raw.get("exempt_paths", [])
            if str(item).strip()
        )
        if name and alternative:
            items.append(
                BannedModule(
                    name=name,
                    exempt_paths=exempt_paths,
                    alternative=alternative,
                )
            )
    return tuple(items)


def _banned_lookup() -> dict[str, BannedModule]:
    return {module.name: module for module in _banned_modules()}


def _find_banned(module_name: str) -> BannedModule | None:
    """Find the BannedModule matching a module name (exact or prefix).

    Supports both simple names (``"json"``) and dotted names
    (``"google.genai"``).  For dotted bans, any sub-module import
    also matches (e.g., ``"google.genai.errors"`` matches the
    ``"google.genai"`` ban).

    Args:
        module_name: The imported module name.

    Returns:
        The matching BannedModule, or None if not banned.

    """
    # Exact match first (fast path for simple module names).
    exact = _banned_lookup().get(module_name)
    if exact is not None:
        return exact
    # Prefix match for dotted bans (e.g., google.genai.errors → google.genai).
    for banned in _banned_modules():
        if "." in banned.name and module_name.startswith(banned.name + "."):
            return banned
    return None


def _is_exempt(module_name: str, path: Path) -> bool:
    """Check whether an import is exempt from the ban.

    Args:
        module_name: The imported module name (e.g., ``"json"``, ``"hashlib"``,
            ``"google.genai"``).
        path: Path to the file containing the import.

    Returns:
        True if the import is allowed (not banned, or file is inside an
        exempt path for this module).

    """
    banned = _find_banned(module_name)
    if banned is None:
        return True
    if not banned.exempt_paths:
        return False
    path_str = str(path)
    return any(marker in path_str for marker in banned.exempt_paths)


def _check_import(node: ast.Import, *, path: Path) -> list[ImportViolation]:
    """Check an ``import X`` statement for banned modules.

    Handles both simple names (``import json``) and dotted names
    (``import google.genai``).

    Args:
        node: AST Import node.
        path: Source file path (for violation records).

    Returns:
        List of violations found in this import statement.

    """
    violations: list[ImportViolation] = []
    for alias in node.names:
        if _is_exempt(alias.name, path):
            continue
        banned = _find_banned(alias.name)
        if banned is None:
            continue
        stmt = (
            f"import {alias.name} as {alias.asname}"
            if alias.asname
            else f"import {alias.name}"
        )
        violations.append(ImportViolation(path, node.lineno, stmt, banned.alternative))
    return violations


def _check_import_from(node: ast.ImportFrom, *, path: Path) -> list[ImportViolation]:
    """Check a ``from X import ...`` statement for banned modules.

    Handles three patterns:

    1. Direct match: ``from json import dumps`` (module is banned).
    2. Dotted prefix: ``from google.genai.errors import ...`` (module
       starts with a banned dotted name).
    3. Parent import: ``from google import genai`` where
       ``google.genai`` is banned (parent module + imported name
       forms a banned module path).

    Args:
        node: AST ImportFrom node.
        path: Source file path (for violation records).

    Returns:
        List of violations found in this import statement.

    """
    if node.module is None:
        return []

    # Pattern 1 & 2: module itself is banned (exact or prefix).
    if not _is_exempt(node.module, path):
        banned = _find_banned(node.module)
        if banned is not None:
            names = ", ".join(
                f"{alias.name} as {alias.asname}" if alias.asname else alias.name
                for alias in node.names
            )
            stmt = f"from {node.module} import {names}"
            return [ImportViolation(path, node.lineno, stmt, banned.alternative)]

    # Pattern 3: ``from google import genai`` where google.genai is banned.
    violations: list[ImportViolation] = []
    for alias in node.names:
        qualified = f"{node.module}.{alias.name}"
        if _is_exempt(qualified, path):
            continue
        banned = _find_banned(qualified)
        if banned is None:
            continue
        stmt_name = f"{alias.name} as {alias.asname}" if alias.asname else alias.name
        stmt = f"from {node.module} import {stmt_name}"
        violations.append(ImportViolation(path, node.lineno, stmt, banned.alternative))
    return violations


def find_banned_imports(path: Path) -> list[ImportViolation]:
    """Find banned direct utility imports in a Python file.

    Scans AST for ``import`` and ``from ... import`` statements that
    match any configured banned module. Files inside a module's
    exempt path are skipped for that module.

    Args:
        path: Path to the Python file.

    Returns:
        List of ImportViolation records.

    Raises:
        OSError: If the file cannot be read.
        SyntaxError: If the file cannot be parsed as Python.

    """
    content = path.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(path))

    violations: list[ImportViolation] = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            violations.extend(_check_import(node, path=path))
        elif isinstance(node, ast.ImportFrom):
            violations.extend(_check_import_from(node, path=path))

    return violations


def main() -> int:
    """Scan files for banned direct utility imports.

    Pre-commit passes only files matching the configured types and exclusions.
    Check all files passed to us.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_ARGS:
        return 0

    if not get_bool("python.util_centralization.enabled", False):
        return 0

    files_to_check = [Path(arg) for arg in sys.argv[1:] if Path(arg).exists()]

    if not files_to_check:
        return 0

    all_violations: list[ImportViolation] = []

    for filepath in files_to_check:
        violations = find_banned_imports(filepath)
        all_violations.extend(violations)

    if not all_violations:
        return 0

    # Report violations.
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("BANNED DIRECT IMPORT DETECTED\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Production code must use the repository's configured utility\n"
        "wrapper modules instead of importing utility libraries directly.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in all_violations:
        sys.stderr.write(f"\n  {v.file}:{v.line}\n")
        sys.stderr.write(f"    Bad:  {v.import_statement}\n")
        sys.stderr.write(f"    Good: {v.alternative}\n")

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
