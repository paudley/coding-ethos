#!/usr/bin/env python3
"""Pre-commit hook to ban direct imports from internal modules.

Enforces that imports use package public APIs (via __init__.py) rather than
importing directly from internal module files.

Good:  from your_pkg.util.jsonl import dumps       # Uses package API
Bad:   from your_pkg.util.jsonl.json import dumps  # Bypasses package API

Rationale:
    - Forces packages to define explicit public APIs in __init__.py
    - Allows internal refactoring without breaking external code
    - Makes dependencies on internal implementation explicit violations

Detection logic:
    - For imports like `from package.submodule import X`
    - Check if submodule is a .py file (not a subpackage with __init__.py)
    - If so, it's a direct import violation

Exceptions:
    - Imports within the same package (relative imports or same-package absolute)
    - Third-party packages (only checks our own packages)
    - Imports from __init__.py itself (re-exports are allowed)

File filtering is handled by pre-commit configuration.

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_direct_imports.py [files...]

Exit codes:
    0: No direct import violations found
    1: One or more files have direct import violations
"""

import ast
import sys
from pathlib import Path
from typing import Final, NamedTuple

from hook_config import get_bool, get_list

# Minimum number of args (script name + at least one file)
MIN_ARGS: Final[int] = 2

# Minimum parts in an import path to be checkable.
MIN_IMPORT_PARTS: Final[int] = 2


class ImportViolation(NamedTuple):
    """Record of a direct import violation."""

    file: Path
    line: int
    import_statement: str
    suggestion: str


class _FileContext(NamedTuple):
    """Context about the file being checked for import violations."""

    filepath: Path
    file_package: str


def _our_packages() -> frozenset[str]:
    packages = [
        str(item).strip()
        for item in get_list("python.direct_imports.packages", ["coding_ethos"])
        if str(item).strip()
    ]
    return frozenset(packages)


def get_package_of_file(filepath: Path) -> str:
    """Determine which package a file belongs to.

    Args:
        filepath: Path to the Python file.

    Returns:
        The top-level package name, or empty string if not in a package.

    """
    # Walk up looking for __init__.py files
    parts: list[str] = []
    current = filepath.parent

    while current.name:
        init_file = current / "__init__.py"
        if init_file.exists():
            parts.insert(0, current.name)
            current = current.parent
        else:
            break

    return parts[0] if parts else ""


def get_module_path_for_import(import_module: str, search_paths: list[Path]) -> Path:
    """Find the file path for an import module string.

    Args:
        import_module: Module path like ``your_pkg.util.jsonl.json``.
        search_paths: Directories to search for the module

    Returns:
        Path to the module file.

    Raises:
        FileNotFoundError: If the module cannot be found in search paths.

    """
    parts = import_module.split(".")

    for search_path in search_paths:
        # Try as a module file: your_pkg/jsonl/json.py
        module_file = (
            search_path / "/".join(parts[:-1]) / f"{parts[-1]}.py"
            if len(parts) > 1
            else None
        )
        if module_file and module_file.exists():
            return module_file

        # Also check direct: your_pkg/jsonl/json.py from root
        direct_path = search_path
        for part in parts:
            direct_path = direct_path / part

        module_as_file = direct_path.with_suffix(".py")
        if module_as_file.exists():
            return module_as_file

        # Check if it's a package (has __init__.py)
        package_init = direct_path / "__init__.py"
        if package_init.exists():
            msg = f"Module {import_module} is a package, not a module file"
            raise FileNotFoundError(msg)

    msg = f"Module {import_module} not found in search paths"
    raise FileNotFoundError(msg)


def is_direct_module_import(import_module: str, filepath: Path) -> tuple[bool, str]:
    """Check if an import is a direct module import (not through __init__.py).

    Args:
        import_module: The full module path being imported
            (e.g., ``'your_pkg.util.jsonl.json'``).
        filepath: Path to the file containing the import.

    Returns:
        Tuple of (is_violation, suggested_import)

    """
    parts = import_module.split(".")

    if len(parts) < MIN_IMPORT_PARTS:
        # Single-level imports like ``import your_pkg`` are always OK.
        return False, ""

    # Check if the top-level package is one of ours
    if parts[0] not in _our_packages():
        return False, ""

    # Find the root directory to search from
    # Walk up from the file to find the package root
    search_root = filepath.parent
    while search_root.name and search_root.name != parts[0]:
        parent = search_root.parent
        if parent == search_root:
            break
        search_root = parent

    if search_root.name == parts[0]:
        search_root = search_root.parent

    # Check if the import target is a module file
    target_path = search_root
    for i, part in enumerate(parts):
        target_path = target_path / part

        # Check if this level is a .py file (direct module)
        package_init = target_path / "__init__.py"

        if i == len(parts) - 1:
            # Last component - check if it's a module file
            if Path(str(target_path) + ".py").exists():
                # It's a direct module import
                # Suggest importing from parent package
                parent_module = ".".join(parts[:-1])
                return True, parent_module
            if not package_init.exists() and not target_path.exists():
                # Module doesn't exist at all - let other tools handle this
                return False, ""

    return False, ""


def _is_same_package_import(module: str, filepath: Path, file_package: str) -> bool:
    """Check if an import is from the same package (internal import allowed).

    Args:
        module: The module being imported.
        filepath: Path to the file containing the import.
        file_package: The top-level package the file belongs to.

    Returns:
        True if this is an internal same-package import that should be allowed.

    """
    if not file_package:
        return False

    parts = module.split(".")
    if parts[0] != file_package:
        return False

    # Build the file's module path
    file_parts: list[str] = []
    current = filepath.parent
    while current.name and (current / "__init__.py").exists():
        file_parts.insert(0, current.name)
        current = current.parent

    # If a file is inside a package, allow its own internal absolute imports.
    file_module = ".".join(file_parts)
    return module.startswith(file_module) or file_module.startswith(module)


def _check_import_from_node(
    node: ast.ImportFrom,
    filepath: Path,
    file_package: str,
) -> list[ImportViolation]:
    """Check an ImportFrom node for direct import violations.

    Args:
        node: The AST ImportFrom node.
        filepath: Path to the file containing the import.
        file_package: The top-level package the file belongs to.

    Returns:
        List containing one ImportViolation if found, empty otherwise.

    """
    if not node.module:
        return []

    module = node.module

    # Skip internal same-package imports
    if _is_same_package_import(module, filepath, file_package):
        return []

    is_violation, suggestion = is_direct_module_import(module, filepath)
    if not is_violation:
        return []

    names = ", ".join(
        f"{alias.name} as {alias.asname}" if alias.asname else alias.name
        for alias in node.names
    )
    stmt = f"from {module} import {names}"
    return [
        ImportViolation(
            file=filepath,
            line=node.lineno,
            import_statement=stmt,
            suggestion=f"from {suggestion} import {names}",
        )
    ]


def _check_import_alias(
    alias: ast.alias,
    lineno: int,
    ctx: _FileContext,
) -> list[ImportViolation]:
    """Check an import alias for direct import violations.

    Args:
        alias: The AST alias from an Import node.
        lineno: Line number of the import statement.
        ctx: File context (filepath and package name).

    Returns:
        List containing one ImportViolation if found, empty otherwise.

    """
    module = alias.name
    parts = module.split(".")

    if len(parts) < MIN_IMPORT_PARTS:
        return []

    # Skip if same package
    if ctx.file_package and parts[0] == ctx.file_package:
        return []

    is_violation, suggestion = is_direct_module_import(module, ctx.filepath)
    if not is_violation:
        return []

    stmt = f"import {module}" + (f" as {alias.asname}" if alias.asname else "")
    return [
        ImportViolation(
            file=ctx.filepath,
            line=lineno,
            import_statement=stmt,
            suggestion=f"import {suggestion}",
        )
    ]


def _parse_file_ast(filepath: Path) -> ast.Module:
    """Parse a Python file into an AST.

    Args:
        filepath: Path to the Python file.

    Returns:
        AST Module node.

    Raises:
        SyntaxError: If the file contains invalid Python syntax.
        OSError: If the file cannot be read.

    """
    content = filepath.read_text(encoding="utf-8")
    return ast.parse(content, filename=str(filepath))


def find_direct_import_violations(filepath: Path) -> list[ImportViolation]:
    """Find all direct module import violations in a Python file.

    Args:
        filepath: Path to the Python file.

    Returns:
        List of ImportViolation records.

    """
    tree = _parse_file_ast(filepath)

    violations: list[ImportViolation] = []
    file_package = get_package_of_file(filepath)

    file_ctx = _FileContext(filepath, file_package)

    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom):
            violations.extend(_check_import_from_node(node, filepath, file_package))
        elif isinstance(node, ast.Import):
            for alias in node.names:
                violations.extend(_check_import_alias(alias, node.lineno, file_ctx))

    return violations


def main() -> int:
    """Check files for banned direct internal imports.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if len(sys.argv) < MIN_ARGS:
        return 0

    if not get_bool("python.direct_imports.enabled", True):
        return 0

    files_to_check = [Path(arg) for arg in sys.argv[1:] if Path(arg).exists()]

    if not files_to_check:
        return 0

    all_violations: list[ImportViolation] = []

    for filepath in files_to_check:
        violations = find_direct_import_violations(filepath)
        all_violations.extend(violations)

    if not all_violations:
        return 0

    # Report violations
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("DIRECT MODULE IMPORT DETECTED\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write("Import from package __init__.py, not internal modules.\n")
    sys.stderr.write("This ensures you use the package's public API.\n")
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in all_violations:
        sys.stderr.write(f"\n  {v.file}:{v.line}\n")
        sys.stderr.write(f"    Bad:  {v.import_statement}\n")
        sys.stderr.write(f"    Good: {v.suggestion}\n")

    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")

    return 1


if __name__ == "__main__":
    sys.exit(main())
