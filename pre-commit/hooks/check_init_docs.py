#!/usr/bin/env python3
"""Pre-commit hook to enforce module documentation.

NOTE: This hook only validates __init__.py and conftest.py files that are
passed to it by pre-commit. Due to staged-only filtering in pre-commit,
this hook will only check files that are being committed, not all files
in the repository.

Checks ALL `__init__.py` and `conftest.py` files in the repository:

1. The Python file MUST have a non-empty module docstring
2. The directory MUST contain at least one .md documentation file
3. The docstring MUST contain a "See Also:" section referencing all co-located .md files
4. All .md files MUST be listed in docs/SOURCE_DOCS.md
5. References must be to CO-LOCATED files only (no path prefixes like "subdir/FILE.md")
6. Referenced files must actually exist

This ensures all modules are documented and source-level docs are discoverable.

Usage:
    Pre-commit: python pre-commit/hooks/check_init_docs.py [files...]

Exit codes:
    0: All checks passed
    1: One or more violations found
"""

import ast
import re
import sys
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final

# Configuration
SOURCE_DOCS_PATH: Final[Path] = Path("docs/SOURCE_DOCS.md")

# Files to check for documentation
CHECK_FILENAMES: Final[set[str]] = {"__init__.py", "conftest.py"}

# Directories excluded from scanning (vendored deps, caches, venvs)
EXCLUDED_DIRS: Final[frozenset[str]] = frozenset(
    {
        ".venv",
        ".lint-cache",
        ".mypy_cache",
        ".ruff_cache",
        "__pycache__",
        "node_modules",
        ".git",
    }
)

# Banned documentation filenames (use MODULE.md convention instead)
# Primary docs should be named after the containing directory (e.g., foo/FOO.md)
# Secondary docs can be any name except these banned names
BANNED_DOC_FILENAMES: Final[set[str]] = {"README.md", "readme.md"}

# Pattern for extracting filenames from "See Also:" section
# Matches lines like "    FILENAME.md: Description" or "    FILENAME.md - Description"
# Only matches simple filenames (no path components) - paths are an anti-pattern
SEE_ALSO_ENTRY_PATTERN: Final[re.Pattern[str]] = re.compile(
    r"^\s+([A-Za-z0-9_-]+\.md)\s*[:|-]",
    re.MULTILINE,
)

# Pattern to detect path-prefixed references (anti-pattern)
# Matches lines like "    subdir/FILENAME.md: Description"
PATH_PREFIXED_PATTERN: Final[re.Pattern[str]] = re.compile(
    r"^\s+([A-Za-z0-9_/-]+/[A-Za-z0-9_-]+\.md)\s*[:|-]",
    re.MULTILINE,
)


def should_check_file(filepath: Path) -> bool:
    """Determine if a file should be checked for documentation.

    Args:
        filepath: Path to the file being checked.

    Returns:
        True if the file is a checkable file not under an excluded directory.

    """
    if filepath.name not in CHECK_FILENAMES:
        return False
    # Reject files under excluded directories (vendored deps, caches)
    return not any(part in EXCLUDED_DIRS for part in filepath.parts)


def get_colocated_md_files(python_file: Path) -> list[Path]:
    """Find all .md files in the same directory as the Python file.

    Args:
        python_file: Path to the Python file being checked.

    Returns:
        List of paths to .md files in the same directory.

    """
    directory = python_file.parent
    if not directory.exists():
        return []
    return sorted(directory.glob("*.md"))


def get_module_docstring(python_file: Path) -> str:
    """Extract the module docstring from a Python file using AST.

    Args:
        python_file: Path to the Python file.

    Returns:
        The module docstring, or empty string if none defined.

    Raises:
        SyntaxError: If the file contains invalid Python.
        OSError: If the file cannot be read.
        UnicodeDecodeError: If the file encoding is invalid.

    """
    content = python_file.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(python_file))
    return ast.get_docstring(tree) or ""


def has_meaningful_docstring(docstring: str) -> bool:
    """Check if a docstring is non-empty and meaningful.

    Args:
        docstring: The module docstring to check.

    Returns:
        True if the docstring has non-whitespace content.

    """
    return bool(docstring.strip())


def extract_see_also_references(docstring: str) -> set[str]:
    """Extract .md filenames referenced in the See Also section.

    Args:
        docstring: The module docstring to parse.

    Returns:
        Set of .md filenames found in the See Also section.

    """
    # Find the "See Also:" section
    see_also_match = re.search(r"See Also:\s*\n", docstring, re.IGNORECASE)
    if not see_also_match:
        return set()

    # Extract the content after "See Also:"
    see_also_content = docstring[see_also_match.end() :]

    # Find all .md references
    references: set[str] = set()
    for match in SEE_ALSO_ENTRY_PATTERN.finditer(see_also_content):
        references.add(match.group(1))

    return references


def extract_path_prefixed_references(docstring: str) -> set[str]:
    """Extract path-prefixed .md references from See Also section (anti-pattern).

    Path-prefixed references like "subdir/FILE.md" are an anti-pattern because
    documentation references should be to co-located files only. Each module
    should reference its own documentation, not reach into subdirectories.

    Args:
        docstring: The module docstring to parse.

    Returns:
        Set of path-prefixed .md references found (these are violations).

    """
    # Find the "See Also:" section
    see_also_match = re.search(r"See Also:\s*\n", docstring, re.IGNORECASE)
    if not see_also_match:
        return set()

    # Extract the content after "See Also:"
    see_also_content = docstring[see_also_match.end() :]

    # Find path-prefixed .md references
    bad_references: set[str] = set()
    for match in PATH_PREFIXED_PATTERN.finditer(see_also_content):
        bad_references.add(match.group(1))

    return bad_references


def check_reference_existence(
    python_file: Path, referenced_files: set[str]
) -> list[str]:
    """Verify that referenced .md files actually exist.

    Args:
        python_file: The Python file containing the docstring.
        referenced_files: Set of .md filenames referenced in the docstring.

    Returns:
        List of referenced files that do NOT exist in the same directory.

    """
    directory = python_file.parent
    return [ref for ref in referenced_files if not (directory / ref).exists()]


def check_banned_filenames(md_files: list[Path]) -> list[Path]:
    """Check for .md files with banned names like README.md.

    Args:
        md_files: List of .md files to check.

    Returns:
        List of .md files with banned names.

    """
    return [f for f in md_files if f.name in BANNED_DOC_FILENAMES]


def check_docstring_references(docstring: str, md_files: list[Path]) -> list[Path]:
    """Check if all .md files are referenced in the docstring's See Also section.

    Args:
        docstring: The module docstring.
        md_files: List of .md files that should be referenced.

    Returns:
        List of .md files NOT referenced in the docstring.

    """
    if not docstring:
        return md_files

    referenced = extract_see_also_references(docstring)
    return [md_file for md_file in md_files if md_file.name not in referenced]


def load_source_docs_index() -> str:
    """Load the contents of docs/SOURCE_DOCS.md.

    Returns:
        The file contents, or empty string if file doesn't exist.

    Raises:
        OSError: If the file exists but cannot be read.
        UnicodeDecodeError: If the file encoding is invalid.

    """
    if not SOURCE_DOCS_PATH.exists():
        return ""
    return SOURCE_DOCS_PATH.read_text(encoding="utf-8")


def check_source_docs_index(md_files: list[Path]) -> list[Path]:
    """Check if all .md files are listed in docs/SOURCE_DOCS.md.

    Args:
        md_files: List of .md files that should be indexed.

    Returns:
        List of .md files NOT found in SOURCE_DOCS.md.

    """
    source_docs_content = load_source_docs_index()
    if not source_docs_content:
        return md_files

    missing: list[Path] = []
    for md_file in md_files:
        # Check if the file's directory and name appear in the index
        # Use as_posix() for cross-platform compatibility
        directory_pattern = md_file.parent.as_posix() + "/"
        # Also check without leading ./
        directory_pattern = directory_pattern.removeprefix("./")

        # Check if both directory and filename are mentioned
        if (
            directory_pattern not in source_docs_content
            or md_file.name not in source_docs_content
        ):
            missing.append(md_file)

    return missing


@dataclass(frozen=True, slots=True)
class DocumentationViolations:
    """Collected documentation violations from all checked files.

    Each field corresponds to a specific category of documentation
    violation found during the check.

    Args:
        missing_docstring: Python files with missing/empty module docstrings.
        missing_md: Python files with no co-located .md documentation.
        docstring_refs: (python_file, unreferenced_md_files) pairs.
        index_violations: .md files not listed in SOURCE_DOCS.md.
        path_prefixed: (python_file, bad_path_refs) pairs.
        nonexistent_refs: (python_file, nonexistent_filenames) pairs.
        banned_filenames: .md files using banned names (README.md).

    """

    missing_docstring: list[Path]
    missing_md: list[Path]
    docstring_refs: list[tuple[Path, list[Path]]]
    index_violations: list[Path]
    path_prefixed: list[tuple[Path, set[str]]]
    nonexistent_refs: list[tuple[Path, list[str]]]
    banned_filenames: list[Path]


def collect_violations(
    files: list[Path],
) -> DocumentationViolations:
    """Collect all documentation violations.

    Args:
        files: List of Python files to check.

    Returns:
        DocumentationViolations containing all violation categories.

    """
    missing_docstring_violations: list[Path] = []
    missing_md_violations: list[Path] = []
    docstring_ref_violations: list[tuple[Path, list[Path]]] = []
    all_md_files: list[Path] = []
    path_prefixed_violations: list[tuple[Path, set[str]]] = []
    nonexistent_ref_violations: list[tuple[Path, list[str]]] = []

    for filepath in files:
        if not should_check_file(filepath):
            continue

        # Check for missing/empty docstring
        docstring = get_module_docstring(filepath)
        if not has_meaningful_docstring(docstring):
            missing_docstring_violations.append(filepath)

        # Check for co-located .md files (REQUIRED)
        md_files = get_colocated_md_files(filepath)
        if not md_files:
            # No .md files = violation (documentation is mandatory)
            missing_md_violations.append(filepath)
        else:
            # Track all md files for SOURCE_DOCS.md check
            all_md_files.extend(md_files)

            # Check docstring references
            missing_refs = check_docstring_references(docstring, md_files)
            if missing_refs:
                docstring_ref_violations.append((filepath, missing_refs))

        # Check for path-prefixed references (anti-pattern) - requires docstring
        if docstring:
            bad_refs = extract_path_prefixed_references(docstring)
            if bad_refs:
                path_prefixed_violations.append((filepath, bad_refs))

            # Check that referenced files actually exist
            referenced = extract_see_also_references(docstring)
            nonexistent = check_reference_existence(filepath, referenced)
            if nonexistent:
                nonexistent_ref_violations.append((filepath, nonexistent))

    # Check SOURCE_DOCS.md index
    index_violations = check_source_docs_index(all_md_files) if all_md_files else []

    # Check for banned filenames (README.md)
    banned_filename_violations = check_banned_filenames(all_md_files)

    return DocumentationViolations(
        missing_docstring=missing_docstring_violations,
        missing_md=missing_md_violations,
        docstring_refs=docstring_ref_violations,
        index_violations=index_violations,
        path_prefixed=path_prefixed_violations,
        nonexistent_refs=nonexistent_ref_violations,
        banned_filenames=banned_filename_violations,
    )


def print_missing_docstring_errors(violations: list[Path]) -> None:
    """Print missing docstring violation errors.

    Args:
        violations: List of Python files without docstrings.

    """
    print("ERROR: Modules missing docstrings!")
    print()
    print("The following __init__.py/conftest.py files have no module docstring:")
    print()
    for filepath in violations:
        print(f"  - {filepath}")
    print()
    print("Add a module docstring at the top of each file:")
    print()
    print('    """Brief description of this module.')
    print()
    print("    Longer description explaining the module's purpose,")
    print("    key classes/functions, and usage patterns.")
    print()
    print("    See Also:")
    print("        README.md: Detailed documentation for this module.")
    print('    """')
    print()


def print_missing_md_errors(violations: list[Path]) -> None:
    """Print missing .md documentation violation errors.

    Args:
        violations: List of Python files without co-located .md files.

    """
    print("ERROR: Modules missing documentation files!")
    print()
    print("Every __init__.py/conftest.py directory MUST have at least one .md file.")
    print("The following directories have no documentation:")
    print()
    for filepath in violations:
        print(f"  - {filepath.parent}/")
    print()
    print("Create a README.md or similar documentation file in each directory:")
    print()
    print("    # Module Name")
    print()
    print("    Brief description of this module's purpose and usage.")
    print()


def print_docstring_ref_errors(violations: list[tuple[Path, list[Path]]]) -> None:
    """Print docstring reference violation errors.

    Args:
        violations: List of (python_file, missing_refs) tuples.

    """
    print("ERROR: Documentation reference violations found!")
    print()
    print("When __init__.py/conftest.py files have co-located .md documentation,")
    print(
        'the module docstring MUST include a "See Also:" section '
        "referencing those files."
    )
    print()
    print("Violations:")
    for python_file, missing_refs in violations:
        print(f"  {python_file} missing references to:")
        for md_file in missing_refs:
            print(f"    - {md_file.name}")
    print()
    print('Add a "See Also:" section to the module docstring:')
    print()
    print('    """Module docstring.')
    print()
    print("    See Also:")
    print("        FILENAME.md: Brief description of the documentation.")
    print('    """')
    print()


def print_index_errors(violations: list[Path]) -> None:
    """Print SOURCE_DOCS.md index violation errors.

    Args:
        violations: List of .md files not in SOURCE_DOCS.md.

    """
    print("ERROR: Source documentation not indexed!")
    print()
    print("The following .md files must be added to docs/SOURCE_DOCS.md:")
    print()
    for md_file in violations:
        print(f"  - {md_file}")
    print()
    print("Add entries to docs/SOURCE_DOCS.md:")
    print()
    print("    | `directory/` | `FILENAME.md` | Description here |")
    print()


def print_path_prefixed_errors(violations: list[tuple[Path, set[str]]]) -> None:
    """Print path-prefixed reference violation errors.

    Args:
        violations: List of (python_file, bad_refs) tuples.

    """
    print("ERROR: Path-prefixed documentation references found!")
    print()
    print("References in 'See Also:' sections must be to CO-LOCATED files only.")
    print("Path prefixes like 'subdir/FILE.md' are an anti-pattern.")
    print("Each module should reference its own documentation, not reach into subdirs.")
    print()
    print("Violations:")
    for python_file, bad_refs in violations:
        print(f"  {python_file}:")
        for ref in sorted(bad_refs):
            print(f"    - {ref}")
    print()
    print("Fix by either:")
    print("  1. Moving the reference to the submodule's own __init__.py docstring")
    print("  2. Describing the submodule without a file path reference")
    print()


def print_nonexistent_ref_errors(violations: list[tuple[Path, list[str]]]) -> None:
    """Print errors for references to non-existent files.

    Args:
        violations: List of (python_file, nonexistent_refs) tuples.

    """
    print("ERROR: References to non-existent documentation files!")
    print()
    print("The 'See Also:' section references .md files that do not exist.")
    print()
    print("Violations:")
    for python_file, bad_refs in violations:
        print(f"  {python_file}:")
        for ref in sorted(bad_refs):
            print(f"    - {ref} (does not exist)")
    print()
    print("Fix by either:")
    print("  1. Creating the missing .md file")
    print("  2. Updating the reference to the correct filename")
    print()


def print_banned_filename_errors(violations: list[Path]) -> None:
    """Print errors for banned documentation filenames like README.md.

    Args:
        violations: List of .md files with banned names.

    """
    print("ERROR: Banned documentation filename(s) found!")
    print()
    print("Documentation files must follow the MODULE.md naming convention:")
    print()
    print("  - Primary docs: Named after containing directory (e.g., foo/FOO.md)")
    print("  - Secondary docs: Any name EXCEPT 'README.md'")
    print("  - All docs: Must be linked in __init__.py/conftest.py AND SOURCE_DOCS.md")
    print()
    print("Files with banned names:")
    for md_file in violations:
        expected_name = md_file.parent.name.upper() + ".md"
        print(f"  - {md_file}")
        print(f"    Rename to: {md_file.parent}/{expected_name}")
    print()


def _print_with_separator(
    print_func: Callable[[], None], *, printed_section: bool
) -> bool:
    """Print a section with separator if needed.

    Args:
        print_func: Function that prints the error section.
        printed_section: Whether a section has already been printed.

    Returns:
        True (always, since we printed something).

    """
    if printed_section:
        print("-" * 70)
        print()
    print_func()
    return True


def main() -> int:
    """Verify module documentation and docstring requirements.

    Returns:
        Exit code: 0 on success, 1 on violations.

    """
    if len(sys.argv) > 1:
        files = [Path(f) for f in sys.argv[1:] if f.endswith(".py")]
    else:
        # Default: check all __init__.py and conftest.py files in repo
        files = list(Path().rglob("__init__.py")) + list(Path().rglob("conftest.py"))

    v = collect_violations(files)

    exit_code = 0
    printed_section = False

    # List of (violations, print_function) pairs to process
    checks: list[tuple[Any, Callable[[], None]]] = [
        (
            v.missing_docstring,
            lambda: print_missing_docstring_errors(v.missing_docstring),
        ),
        (v.missing_md, lambda: print_missing_md_errors(v.missing_md)),
        (v.docstring_refs, lambda: print_docstring_ref_errors(v.docstring_refs)),
        (v.index_violations, lambda: print_index_errors(v.index_violations)),
        (v.path_prefixed, lambda: print_path_prefixed_errors(v.path_prefixed)),
        (v.nonexistent_refs, lambda: print_nonexistent_ref_errors(v.nonexistent_refs)),
        (v.banned_filenames, lambda: print_banned_filename_errors(v.banned_filenames)),
    ]

    for violations, print_func in checks:
        if violations:
            printed_section = _print_with_separator(
                print_func, printed_section=printed_section
            )
            exit_code = 1

    return exit_code


if __name__ == "__main__":
    sys.exit(main())
