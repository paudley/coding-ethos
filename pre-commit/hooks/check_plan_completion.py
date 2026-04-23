#!/usr/bin/env python3
"""Pre-commit hook to prevent merging plans with unchecked items.

This hook blocks commits that:
1. Mark a plan's metadata.yaml as 'review' or 'complete'
2. While the plan still has unchecked items in markdown files

This prevents the pattern where agents claim work is "done" without
actually completing all specified requirements.
"""

import re
import shutil
import subprocess
import sys
from pathlib import Path

from hook_config import get_bool, get_list


def get_staged_files() -> list[Path]:
    """Get list of staged files."""
    git_path = shutil.which("git")
    if not git_path:
        sys.stderr.write("Error: 'git' not found in PATH\n")
        return []
    # S603: Safe - using full path to git binary, trusted command
    result = subprocess.run(
        [git_path, "diff", "--cached", "--name-only"],
        capture_output=True,
        text=True,
        check=True,
    )
    return [Path(f) for f in result.stdout.strip().split("\n") if f]


def find_plan_metadata_files(staged_files: list[Path]) -> list[Path]:
    """Find staged plan metadata.yaml files."""
    metadata_filename = str(
        get_list("python.plan_completion.metadata_filename", ["metadata.yaml"])[0]
    ).strip()
    root_markers = [
        str(item).strip()
        for item in get_list("python.plan_completion.root_markers", ["docs/plans/"])
        if str(item).strip()
    ]
    return [
        f
        for f in staged_files
        if f.name == metadata_filename and any(marker in str(f) for marker in root_markers)
    ]


def get_plan_status(metadata_path: Path) -> str:
    """Extract status from metadata.yaml.

    Args:
        metadata_path: Path to the metadata.yaml file.

    Returns:
        Status string, or empty string if file missing or no status found.

    """
    if not metadata_path.exists():
        return ""

    content = metadata_path.read_text()
    match = re.search(r"^status:\s*(\w+)", content, re.MULTILINE)
    return match.group(1) if match else ""


def find_unchecked_items(plan_dir: Path) -> list[tuple[Path, int, str]]:
    """Find all unchecked markdown items in a plan directory.

    Returns list of (file, line_number, item_text) tuples.
    """
    unchecked: list[tuple[Path, int, str]] = []
    for md_file in plan_dir.rglob("*.md"):
        content = md_file.read_text()
        for i, line in enumerate(content.split("\n"), start=1):
            # Match unchecked checkbox items: - [ ] text
            if re.match(r"^-\s*\[\s*\]\s+.+", line):
                item_text = line.strip()
                unchecked.append((md_file, i, item_text))
    return unchecked


def check_plan_completion(metadata_path: Path) -> list[str]:
    """Check if a plan marked complete/review has unchecked items.

    Returns list of error messages, empty if no issues.
    """
    errors: list[str] = []

    status = get_plan_status(metadata_path)
    completed_status_values = {
        str(item).strip()
        for item in get_list(
            "python.plan_completion.completed_status_values",
            ["review", "complete"],
        )
        if str(item).strip()
    }
    if status not in completed_status_values:
        # Plan not being marked as done, no check needed
        return errors

    plan_dir = metadata_path.parent
    unchecked = find_unchecked_items(plan_dir)

    if unchecked:
        errors.append(
            f"\n{'=' * 60}\n"
            f"PLAN COMPLETION FRAUD DETECTED\n"
            f"{'=' * 60}\n"
            f"\nPlan: {plan_dir.name}\n"
            f"Claimed status: {status}\n"
            f"\nBut these items are still unchecked:\n"
        )
        for file_path, line_num, item_text in unchecked:
            rel_path = file_path.relative_to(plan_dir)
            errors.append(f"  {rel_path}:{line_num}: {item_text}")

        errors.append(
            f"\n{'=' * 60}\n"
            "BLOCKED: Cannot mark plan as '{status}' with incomplete items.\n"
            "\nOptions:\n"
            "  1. Complete the work (check off items when done)\n"
            "  2. Get explicit user approval to remove items from scope\n"
            "  3. Change status back to 'in_progress'\n"
            "\nDO NOT:\n"
            "  - Use 'git commit --no-verify' to bypass this check\n"
            "  - Rationalize why incomplete items don't matter\n"
            "  - Claim YAGNI/KISS for spec'd requirements\n"
            f"{'=' * 60}\n"
        )

    return errors


def main() -> int:
    """Verify plan completion status before commit."""
    if not get_bool("python.plan_completion.enabled", False):
        return 0

    staged_files = get_staged_files()
    metadata_files = find_plan_metadata_files(staged_files)

    all_errors: list[str] = []
    for metadata_path in metadata_files:
        errors = check_plan_completion(metadata_path)
        all_errors.extend(errors)

    if all_errors:
        print("\n".join(all_errors), file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
