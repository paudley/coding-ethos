#!/usr/bin/env python3
"""Pre-commit hook to validate manifest.yaml structure.

Ensures the manifest.yaml file has a valid structure with:
1. Required version field
2. Valid symlinks section with source/target pairs
3. Valid repositories section with name/url pairs (if present)

Usage:
    Pre-commit: python pre-commit/hooks/validate_manifest.py

Exit codes:
    0: Manifest is valid
    1: Manifest has structural issues
"""

import sys
import typing
from pathlib import Path
from typing import Any

import yaml

from hook_config import get, get_bool, get_list


# Type alias for YAML dict structures
YAMLDict = dict[str, Any]


def find_manifest() -> Path:
    """Find a configured manifest file.

    Returns:
        Path to manifest.yaml.

    Raises:
        FileNotFoundError: If manifest.yaml cannot be found.

    """
    for raw_path in get_list(
        "python.manifest_validation.candidate_paths",
        ["manifest.yaml", "code-ethos/manifest.yaml"],
    ):
        candidate = Path(str(raw_path))
        if candidate.exists():
            return candidate

    msg = "manifest candidate not found"
    raise FileNotFoundError(msg)


def validate_symlink(symlink: object, index: int) -> list[str]:
    """Validate a single symlink entry.

    Args:
        symlink: The symlink dictionary (from YAML, so typed as object).
        index: Index in the symlinks list (for error messages).

    Returns:
        List of error messages.

    """
    errors: list[str] = []

    if not isinstance(symlink, dict):
        return [f"symlinks[{index}]: Expected dict, got {type(symlink).__name__}"]

    symlink_dict = typing.cast("YAMLDict", symlink)
    if "source" not in symlink_dict:
        errors.append(f"symlinks[{index}]: Missing 'source' field")
    elif not isinstance(symlink_dict["source"], str):
        errors.append(f"symlinks[{index}].source: Expected string")

    if "target" not in symlink_dict:
        errors.append(f"symlinks[{index}]: Missing 'target' field")
    elif not isinstance(symlink_dict["target"], str):
        errors.append(f"symlinks[{index}].target: Expected string")

    return errors


def validate_repository(repo: object, index: int) -> list[str]:
    """Validate a single repository entry.

    Args:
        repo: The repository dictionary (from YAML, so typed as object).
        index: Index in the repositories list (for error messages).

    Returns:
        List of error messages.

    """
    errors: list[str] = []

    if not isinstance(repo, dict):
        return [f"repositories[{index}]: Expected dict, got {type(repo).__name__}"]

    repo_dict = typing.cast("YAMLDict", repo)
    if "name" not in repo_dict:
        errors.append(f"repositories[{index}]: Missing 'name' field")
    elif not isinstance(repo_dict["name"], str):
        errors.append(f"repositories[{index}].name: Expected string")

    if "url" not in repo_dict:
        errors.append(f"repositories[{index}]: Missing 'url' field")
    elif not isinstance(repo_dict["url"], str):
        errors.append(f"repositories[{index}].url: Expected string")

    # Branch is optional, but if present must be string
    if "branch" in repo_dict and not isinstance(repo_dict["branch"], str):
        errors.append(f"repositories[{index}].branch: Expected string")

    return errors


def validate_manifest(data: YAMLDict) -> list[str]:
    """Validate the entire manifest structure.

    Args:
        data: Parsed YAML data.

    Returns:
        List of error messages.

    """
    errors: list[str] = []

    for field_name in get_list(
        "python.manifest_validation.required_string_fields",
        ["version"],
    ):
        if field_name not in data:
            errors.append(f"Missing required '{field_name}' field")
        elif not isinstance(data[str(field_name)], str):
            errors.append(f"'{field_name}' must be a string")

    section_specs = typing.cast(
        "dict[str, object]",
        get(
            "python.manifest_validation.required_list_sections",
            {
                "symlinks": {
                    "required": True,
                    "required_string_fields": ["source", "target"],
                },
                "repositories": {
                    "required": False,
                    "required_string_fields": ["name", "url"],
                    "optional_string_fields": ["branch"],
                },
            },
        ),
    )

    for section_name, raw_spec in section_specs.items():
        if not isinstance(raw_spec, dict):
            continue
        section_required = bool(raw_spec.get("required", False))
        section_value = data.get(section_name)
        if section_value is None:
            if section_required:
                errors.append(f"Missing required '{section_name}' section")
            continue
        if not isinstance(section_value, list):
            errors.append(f"'{section_name}' must be a list")
            continue
        required_fields = [
            str(item).strip()
            for item in raw_spec.get("required_string_fields", [])
            if str(item).strip()
        ]
        optional_fields = [
            str(item).strip()
            for item in raw_spec.get("optional_string_fields", [])
            if str(item).strip()
        ]
        for i, entry in enumerate(typing.cast("list[object]", section_value)):
            if not isinstance(entry, dict):
                errors.append(
                    f"{section_name}[{i}]: Expected dict, got {type(entry).__name__}"
                )
                continue
            entry_dict = typing.cast("YAMLDict", entry)
            for field_name in required_fields:
                if field_name not in entry_dict:
                    errors.append(
                        f"{section_name}[{i}]: Missing '{field_name}' field"
                    )
                elif not isinstance(entry_dict[field_name], str):
                    errors.append(f"{section_name}[{i}].{field_name}: Expected string")
            for field_name in optional_fields:
                if field_name in entry_dict and not isinstance(entry_dict[field_name], str):
                    errors.append(f"{section_name}[{i}].{field_name}: Expected string")

    return errors


def main() -> int:
    """Validate manifest.yaml structure and report errors.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if not get_bool("python.manifest_validation.enabled", False):
        return 0

    try:
        manifest_path = find_manifest()
    except FileNotFoundError as exc:
        print(f"ERROR: {exc}")
        return 1

    try:
        content = manifest_path.read_text(encoding="utf-8")
        data = yaml.safe_load(content)
    except yaml.YAMLError as e:
        print(f"ERROR: Invalid YAML syntax in {manifest_path}:")
        print(f"  {e}")
        return 1
    except OSError as e:
        print(f"ERROR: Could not read {manifest_path}: {e}")
        return 1

    if not isinstance(data, dict):
        print(f"ERROR: {manifest_path} must be a YAML mapping (dict)")
        return 1

    errors = validate_manifest(typing.cast("YAMLDict", data))

    if errors:
        print(f"ERROR: {manifest_path} validation failed:")
        for error in errors:
            print(f"  - {error}")
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
