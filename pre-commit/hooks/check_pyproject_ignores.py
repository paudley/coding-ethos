#!/usr/bin/env python3
"""Pre-commit hook to block linter file-ignore config in pyproject.toml.

This hook prevents file-level ignore/exclude configuration for ruff,
mypy, pyright, or pylint inside any pyproject.toml.

All ignores are blocked except for test files (tests/**). File-specific
suppressions must be inline comments (e.g., # noqa: CODE) with documentation.
"""

import sys
import tomllib
import typing
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final

from hook_config import get_bool, get_list

# Type alias for TOML dict structures
TOMLDict = dict[str, Any]


MIN_REQUIRED_ARGS: Final[int] = 2


# Patterns that are allowed to have per-file ignores.
# Tests: relaxed rules for pytest fixtures, assertions, etc.
# Stubs: third-party API stubs inherently use Any, lack docstrings, etc.
# Protocols: interface definitions inherently use Any for abstract contracts
#   (Contract 1 forbids importing concrete schema types into protocols).
def _allowed_ignore_patterns() -> frozenset[str]:
    return frozenset(
        str(item).strip()
        for item in get_list(
            "python.pyproject_ignores.allowed_ignore_patterns",
            ["tests/**", "tests/*", "test_*.py", "*_test.py", "stubs/**", "stubs/*"],
        )
        if str(item).strip()
    )


# Build/cache directories that are legitimately excluded from linting
def _allowed_exclude_patterns() -> frozenset[str]:
    return frozenset(
        str(item).strip()
        for item in get_list(
            "python.pyproject_ignores.allowed_exclude_patterns",
            [
                ".git",
                ".venv",
                ".mypy_cache",
                ".ruff_cache",
                "__pycache__",
                "*.egg-info",
                ".eggs",
                "build",
                "dist",
                "node_modules",
            ],
        )
        if str(item).strip()
    )


# External package patterns for mypy ignore_missing_imports (no type stubs)
def _allowed_mypy_missing_imports() -> frozenset[str]:
    return frozenset(
        str(item).strip()
        for item in get_list(
            "python.pyproject_ignores.allowed_mypy_missing_imports",
            [],
        )
        if str(item).strip()
    )


RUFF_PER_FILE_IGNORE_KEYS: Final[tuple[str, ...]] = (
    "per-file-ignores",
    "extend-per-file-ignores",
    "per_file_ignores",
    "extend_per_file_ignores",
)
RUFF_EXCLUDE_KEYS: Final[tuple[str, ...]] = (
    "exclude",
    "extend-exclude",
    "extend_exclude",
)
MYPY_PER_FILE_IGNORE_KEYS: Final[tuple[str, ...]] = (
    "per-file-ignores",
    "per_file_ignores",
)
MYPY_OVERRIDE_IGNORE_KEYS: Final[tuple[str, ...]] = (
    "ignore_errors",
    "ignore_missing_imports",
    "disable_error_code",
    "disable_error_codes",
)
PYRIGHT_IGNORE_KEYS: Final[tuple[str, ...]] = (
    "exclude",
    "ignore",
)
PYLINT_IGNORE_KEYS: Final[tuple[str, ...]] = (
    "ignore",
    "ignore-patterns",
    "ignore-paths",
    "ignore-modules",
    "ignored-modules",
)


@dataclass(frozen=True)
class Finding:
    """Represents a single ignore/exclude config entry."""

    tool: str
    setting: str
    target: str
    detail: str = ""

    def render(self) -> str:
        """Render the finding for console output."""
        if self.detail:
            return f"{self.tool} {self.setting}: {self.target} -> {self.detail}"
        return f"{self.tool} {self.setting}: {self.target}"


def normalize_list(value: object) -> list[str]:
    """Normalize TOML values to a list of strings."""
    if value is None:
        return []
    if isinstance(value, list):
        typed_list = typing.cast("list[object]", value)
        return [str(item) for item in typed_list if item is not None]
    if isinstance(value, tuple):
        typed_tuple = typing.cast("tuple[object, ...]", value)
        return [str(item) for item in typed_tuple if item is not None]
    if isinstance(value, set):
        typed_set = typing.cast("set[object]", value)
        return [str(item) for item in typed_set if item is not None]
    return [str(value)]


def extract_per_file_mapping(
    tool: str,
    setting: str,
    value: object,
) -> set[Finding]:
    """Extract per-file ignore mappings into normalized findings."""
    findings: set[Finding] = set()
    if isinstance(value, dict):
        typed_dict = typing.cast("TOMLDict", value)
        for pattern, codes in typed_dict.items():
            code_list = normalize_list(codes)
            if not code_list:
                findings.add(Finding(tool, setting, str(pattern), "<all>"))
                continue
            for code in code_list:
                findings.add(Finding(tool, setting, str(pattern), code))
        return findings
    if value is None:
        return findings
    for entry in normalize_list(value):
        findings.add(Finding(tool, setting, entry))
    return findings


def extract_pattern_list(tool: str, setting: str, value: object) -> set[Finding]:
    """Extract simple pattern lists into findings."""
    findings: set[Finding] = set()
    for pattern in normalize_list(value):
        findings.add(Finding(tool, setting, pattern))
    return findings


def extract_ruff_findings(tool_table: TOMLDict) -> set[Finding]:
    """Extract ruff per-file ignore settings."""
    findings: set[Finding] = set()
    ruff = tool_table.get("ruff")
    if not isinstance(ruff, dict):
        return findings
    ruff_dict = typing.cast("TOMLDict", ruff)
    lint = ruff_dict.get("lint")
    if isinstance(lint, dict):
        lint_dict = typing.cast("TOMLDict", lint)
        for key in RUFF_PER_FILE_IGNORE_KEYS:
            if key in lint_dict:
                findings.update(extract_per_file_mapping("ruff", key, lint_dict[key]))
    for key in RUFF_PER_FILE_IGNORE_KEYS:
        if key in ruff_dict:
            findings.update(extract_per_file_mapping("ruff", key, ruff_dict[key]))
    for key in RUFF_EXCLUDE_KEYS:
        if key in ruff_dict:
            findings.update(extract_pattern_list("ruff", key, ruff_dict[key]))
    return findings


def _process_mypy_override_key(
    key: str,
    value: object,
    modules: list[str],
) -> set[Finding]:
    """Process a single mypy override key and return findings.

    Args:
        key: The mypy override key being processed.
        value: The value associated with the key.
        modules: List of module names from the override block.

    Returns:
        Set of findings for this override key.

    """
    result: set[Finding] = set()
    if key in {"disable_error_code", "disable_error_codes"}:
        for code in normalize_list(value):
            if code:
                for module in modules:
                    result.add(Finding("mypy", f"override.{key}", module, code))
        return result
    if isinstance(value, bool) and value:
        for module in modules:
            result.add(Finding("mypy", f"override.{key}", module))
        return result
    if value is not None and not isinstance(value, bool):
        for module in modules:
            result.add(Finding("mypy", f"override.{key}", module, str(value)))
    return result


def _process_single_mypy_override(
    override: TOMLDict,
    findings: set[Finding],
) -> None:
    """Process a single mypy override block."""
    modules_value = override.get("module") or override.get("modules")
    modules = normalize_list(modules_value) or ["<unknown>"]
    for key in MYPY_OVERRIDE_IGNORE_KEYS:
        if key in override:
            findings.update(_process_mypy_override_key(key, override[key], modules))


def extract_mypy_overrides(overrides: object) -> set[Finding]:
    """Extract mypy override ignore settings."""
    findings: set[Finding] = set()
    if not isinstance(overrides, list):
        return findings
    for override in typing.cast("list[object]", overrides):
        if isinstance(override, dict):
            _process_single_mypy_override(typing.cast("TOMLDict", override), findings)
    return findings


def extract_mypy_findings(tool_table: TOMLDict) -> set[Finding]:
    """Extract mypy per-file ignore settings."""
    findings: set[Finding] = set()
    mypy = tool_table.get("mypy")
    if not isinstance(mypy, dict):
        return findings
    mypy_dict = typing.cast("TOMLDict", mypy)
    for key in MYPY_PER_FILE_IGNORE_KEYS:
        if key in mypy_dict:
            findings.update(extract_per_file_mapping("mypy", key, mypy_dict[key]))
    for pattern in normalize_list(mypy_dict.get("exclude")):
        findings.add(Finding("mypy", "exclude", pattern))
    findings.update(extract_mypy_overrides(mypy_dict.get("overrides")))
    return findings


def extract_pyright_findings(tool_table: TOMLDict) -> set[Finding]:
    """Extract pyright ignore/exclude settings."""
    findings: set[Finding] = set()
    pyright = tool_table.get("pyright")
    if not isinstance(pyright, dict):
        return findings
    pyright_dict = typing.cast("TOMLDict", pyright)
    for key in PYRIGHT_IGNORE_KEYS:
        for pattern in normalize_list(pyright_dict.get(key)):
            findings.add(Finding("pyright", key, pattern))
    return findings


def extract_pylint_findings(tool_table: TOMLDict) -> set[Finding]:
    """Extract pylint ignore settings."""
    findings: set[Finding] = set()
    pylint = tool_table.get("pylint")
    if not isinstance(pylint, dict):
        return findings
    pylint_dict = typing.cast("TOMLDict", pylint)
    sections: list[TOMLDict] = [pylint_dict]
    main_section = pylint_dict.get("main")
    if isinstance(main_section, dict):
        sections.append(typing.cast("TOMLDict", main_section))
    for section in sections:
        for key in PYLINT_IGNORE_KEYS:
            for pattern in normalize_list(section.get(key)):
                findings.add(Finding("pylint", key, pattern))
    return findings


def extract_findings(config: TOMLDict) -> set[Finding]:
    """Extract all linter file-ignore findings from a TOML config."""
    tool_table = config.get("tool")
    if not isinstance(tool_table, dict):
        return set()
    typed_tool_table = typing.cast("TOMLDict", tool_table)
    findings: set[Finding] = set()
    findings.update(extract_ruff_findings(typed_tool_table))
    findings.update(extract_mypy_findings(typed_tool_table))
    findings.update(extract_pyright_findings(typed_tool_table))
    findings.update(extract_pylint_findings(typed_tool_table))
    return findings


def load_toml_bytes(
    data: bytes, label: str
) -> tuple[dict[str, object] | None, str | None]:
    """Parse TOML bytes into a dict, returning error message on failure."""
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        return None, f"{label}: unable to decode UTF-8: {exc}"
    try:
        return tomllib.loads(text), None
    except tomllib.TOMLDecodeError as exc:
        return None, f"{label}: invalid TOML: {exc}"


def load_toml_from_disk(path: Path) -> tuple[dict[str, object] | None, str | None]:
    """Load TOML content directly from disk."""
    if not path.exists():
        return None, None
    try:
        data = path.read_bytes()
    except OSError as exc:
        return None, f"{path}: unable to read file: {exc}"
    return load_toml_bytes(data, str(path))


def is_allowed_pattern(target: str) -> bool:
    """Check if a target pattern is in the allowed list."""
    return target in _allowed_ignore_patterns()


def is_allowed_exclude(target: str) -> bool:
    """Check if an exclude pattern is a legitimate build/cache directory."""
    return target in _allowed_exclude_patterns()


def is_allowed_mypy_missing_import(target: str) -> bool:
    """Check if a mypy ignore_missing_imports target is an external package."""
    return target in _allowed_mypy_missing_imports()


def is_allowed_finding(finding: Finding) -> bool:
    """Check if a finding should be allowed (not reported as an error)."""
    # Test file patterns are always allowed
    if is_allowed_pattern(finding.target):
        return True

    # Build/cache directory excludes are allowed
    if finding.setting in {
        "exclude",
        "extend-exclude",
        "extend_exclude",
    } and is_allowed_exclude(finding.target):
        return True

    # External package missing imports are allowed for mypy
    return (
        finding.tool == "mypy"
        and finding.setting == "override.ignore_missing_imports"
        and is_allowed_mypy_missing_import(finding.target)
    )


def filter_allowed_findings(findings: set[Finding]) -> set[Finding]:
    """Remove findings for allowed patterns (e.g., test files, build dirs)."""
    return {f for f in findings if not is_allowed_finding(f)}


def format_findings(findings: set[Finding]) -> list[str]:
    """Format findings for output, sorted for deterministic display."""
    ordered = sorted(
        findings, key=lambda item: (item.tool, item.setting, item.target, item.detail)
    )
    return [finding.render() for finding in ordered]


def main() -> int:
    """Run pyproject linter-ignore checks on staged files."""
    if not get_bool("python.pyproject_ignores.enabled", True):
        return 0

    if len(sys.argv) < MIN_REQUIRED_ARGS:
        return 0

    has_errors = False

    for raw_path in sys.argv[1:]:
        path = Path(raw_path)
        if path.name != "pyproject.toml":
            continue

        config, error = load_toml_from_disk(path)
        if error:
            has_errors = True
            print(f"ERROR: {path}: {error}")
            continue
        if config is None:
            continue

        findings = filter_allowed_findings(extract_findings(config))

        if findings:
            has_errors = True
            print(f"ERROR: {path} contains forbidden linter file ignores:")
            for line in format_findings(findings):
                print(f"  {line}")

    if has_errors:
        print("\n" + "=" * 60)
        print("Pyproject linter ignore check FAILED")
        print("=" * 60)
        print(
            "Move file-specific ignores into the files themselves with "
            "documentation (e.g., # noqa / # type: ignore[code]).",
        )
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
