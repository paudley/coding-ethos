#!/usr/bin/env python3
"""Pre-commit hook to gate expensive checks behind a passing test suite.

Ensures all tests pass with zero skips before allowing the commit to
proceed to expensive downstream hooks (e.g., Gemini AI review). This
enforces ETHOS §22 (Testing as Specification): 100% pass rate is
non-negotiable, and no tests may be skipped.

Two-phase check:

    Phase A (Static): AST-walk staged Python files for banned pytest
        markers (``@pytest.mark.skip``, ``@pytest.mark.skipif``).
        These markers are banned per ETHOS §22.  ``@pytest.mark.xfail``
        is permitted with a mandatory ``reason=`` string — it documents
        known temporary failures while keeping tests visible.

    Phase B (Runtime): Run ``uv run pytest --strict-markers -q`` and
        parse the summary line. Exit code must be 0, and the summary
        must show zero skipped tests.  ``xfail`` results are allowed.

Phase A runs first as a fast static check. If banned markers are found,
the hook fails immediately without running the test suite.

File filtering for tests and .venv is handled by pre-commit configuration
(exclude directive).

Usage:
    Pre-commit: uv run python pre-commit/hooks/check_pytest_gate.py [files...]

Exit codes:
    0: All tests pass with zero skips
    1: Banned markers found, tests failed, or skipped tests detected

"""

import ast
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Final, NamedTuple

from hook_config import get_bool, get_list


class MarkerViolation(NamedTuple):
    """Record of a banned pytest marker found in source code."""

    file: Path
    line: int
    marker: str


# Minimum number of args (script name + at least one file).
MIN_ARGS: Final[int] = 2

# Minimum attribute chain length to match a marker (e.g., ["mark", "skip"]).
_MIN_MARKER_CHAIN: Final[int] = 2

# Maximum number of output lines to show in failure reports.
_MAX_OUTPUT_LINES: Final[int] = 30

# Regex to parse pytest summary line, e.g.:
#   "47 passed, 2 skipped, 1 xfailed in 12.34s"
#   "6177 passed in 45.67s"
_SUMMARY_PATTERN: Final[re.Pattern[str]] = re.compile(
    r"(?P<passed>\d+) passed"
    r"(?:.*?(?P<skipped>\d+) skipped)?"
    r"(?:.*?(?P<xfailed>\d+) xfailed)?"
    r"(?:.*?(?P<failed>\d+) failed)?"
    r"(?:.*?(?P<errors>\d+) error)?"
)


def _banned_markers() -> frozenset[str]:
    markers = [
        str(item).strip()
        for item in get_list("python.pytest_gate.banned_markers", ["skip", "skipif"])
        if str(item).strip()
    ]
    return frozenset(markers)


def _test_command() -> list[str]:
    return [
        str(item).strip()
        for item in get_list(
            "python.pytest_gate.test_command",
            ["uv", "run", "--frozen", "pytest", "tests", "--strict-markers"],
        )
        if str(item).strip()
    ]


def _is_banned_marker(attr_chain: list[str]) -> str:
    """Check if an attribute chain matches a banned pytest marker.

    Recognizes patterns like ``pytest.mark.skip``, ``pytest.mark.skipif``,
    and ``pytest.mark.xfail`` regardless of how they are imported (direct
    attribute access or ``from pytest import mark``).

    Args:
        attr_chain: List of attribute names from the decorator AST node,
            e.g., ``["pytest", "mark", "skip"]`` or ``["mark", "xfail"]``.

    Returns:
        The matched marker name if banned, or empty string if not banned.

    """
    if len(attr_chain) < _MIN_MARKER_CHAIN:
        return ""
    # Expect [..., "mark", "<marker_name>"] pattern
    if attr_chain[-2] != "mark":
        return ""
    marker = attr_chain[-1]
    if marker in _banned_markers():
        return marker
    return ""


def _extract_attr_chain(node: ast.expr) -> list[str]:
    """Extract the full attribute chain from a decorator expression.

    Handles both simple attribute access (``pytest.mark.skip``) and
    call expressions (``pytest.mark.skip(reason="...")``).

    Args:
        node: AST expression node from a decorator.

    Returns:
        List of attribute names, e.g., ``["pytest", "mark", "skip"]``.
        Returns empty list if the node is not an attribute chain.

    """
    # Unwrap call: @pytest.mark.skip(reason="...") → pytest.mark.skip
    if isinstance(node, ast.Call):
        node = node.func

    chain: list[str] = []
    current = node
    while isinstance(current, ast.Attribute):
        chain.append(current.attr)
        current = current.value
    if isinstance(current, ast.Name):
        chain.append(current.id)

    chain.reverse()
    return chain


def find_banned_markers(path: Path) -> list[MarkerViolation]:
    """Find banned pytest markers in a Python file.

    AST-walks the file looking for decorator expressions that match
    ``@pytest.mark.skip`` or ``@pytest.mark.skipif``.

    Args:
        path: Path to the Python file.

    Returns:
        List of MarkerViolation records.

    Raises:
        OSError: If the file cannot be read.
        SyntaxError: If the file cannot be parsed as Python.

    """
    content = path.read_text(encoding="utf-8")
    tree = ast.parse(content, filename=str(path))

    violations: list[MarkerViolation] = []
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            continue
        for decorator in node.decorator_list:
            chain = _extract_attr_chain(decorator)
            marker = _is_banned_marker(chain)
            if marker:
                violations.append(
                    MarkerViolation(
                        file=path,
                        line=decorator.lineno,
                        marker=f"pytest.mark.{marker}",
                    )
                )

    return violations


def run_pytest() -> tuple[int, str, str]:
    """Run the pytest suite and capture output.

    Returns:
        Tuple of (return_code, stdout, stderr).

    """
    cmd = _test_command()

    # S603: Safe — hardcoded command with all arguments from constants
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        check=False,
    )

    return result.returncode, result.stdout, result.stderr


def parse_summary(output: str) -> dict[str, int]:
    """Parse the pytest summary line for test counts.

    Extracts passed, skipped, xfailed, failed, and error counts from
    the pytest summary output.

    Args:
        output: Full pytest stdout output.

    Returns:
        Dictionary with keys: passed, skipped, xfailed, failed, errors.
        Missing categories default to 0.

    """
    counts: dict[str, int] = {
        "passed": 0,
        "skipped": 0,
        "xfailed": 0,
        "failed": 0,
        "errors": 0,
    }

    # Search from the end of output for the summary line
    for line in reversed(output.splitlines()):
        match = _SUMMARY_PATTERN.search(line)
        if match:
            for key in counts:
                value = match.group(key)
                if value is not None:
                    counts[key] = int(value)
            break

    return counts


def _report_marker_violations(violations: list[MarkerViolation]) -> None:
    """Report banned marker violations to stderr.

    Args:
        violations: List of marker violations found.

    """
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("BANNED PYTEST MARKERS DETECTED (ETHOS §22)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")
    sys.stderr.write(
        "Per ETHOS §22: 100% pass rate is non-negotiable. Tests must\n"
        "not be skipped. Use @pytest.mark.xfail(reason='...') for known\n"
        "temporary failures instead.\n"
    )
    sys.stderr.write("\n")
    sys.stderr.write("Violations found:\n")

    for v in violations:
        sys.stderr.write(f"  {v.file}:{v.line}: @{v.marker}\n")

    sys.stderr.write("\n")
    sys.stderr.write("How to fix:\n")
    sys.stderr.write("  1. Remove the @pytest.mark.skip/skipif decorator\n")
    sys.stderr.write("  2. Fix the test or the code it tests\n")
    sys.stderr.write("  3. Use @pytest.mark.xfail(reason='...') for known gaps\n")
    sys.stderr.write("  4. If the test is truly obsolete, delete it entirely\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")


@dataclass(frozen=True, slots=True)
class _PytestResult:
    """Bundled result from a pytest run."""

    return_code: int
    counts: dict[str, int]
    stdout: str
    stderr: str


def _report_test_failures(result: _PytestResult) -> None:
    """Report test suite failures to stderr.

    Args:
        result: Bundled pytest execution result.

    """
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("PYTEST GATE FAILED (ETHOS §22)\n")
    sys.stderr.write("=" * 70 + "\n")
    sys.stderr.write("\n")

    if result.return_code != 0:
        sys.stderr.write(f"Pytest exited with code {result.return_code}.\n")
    if result.counts["failed"] > 0:
        sys.stderr.write(f"Failed tests: {result.counts['failed']}\n")
    if result.counts["errors"] > 0:
        sys.stderr.write(f"Errors: {result.counts['errors']}\n")
    if result.counts["skipped"] > 0:
        sys.stderr.write(f"Skipped tests: {result.counts['skipped']}\n")
    sys.stderr.write("\n")

    output_lines = result.stdout.strip().splitlines()
    if len(output_lines) > _MAX_OUTPUT_LINES:
        truncated = len(output_lines) - _MAX_OUTPUT_LINES
        sys.stderr.write(f"... ({truncated} lines truncated)\n")
        output_lines = output_lines[-_MAX_OUTPUT_LINES:]
    for line in output_lines:
        sys.stderr.write(f"  {line}\n")

    if result.stderr.strip():
        sys.stderr.write("\nStderr:\n")
        for line in result.stderr.strip().splitlines()[-_MAX_OUTPUT_LINES:]:
            sys.stderr.write(f"  {line}\n")

    sys.stderr.write("\n")
    sys.stderr.write("All tests must pass with zero skips.\n")
    sys.stderr.write("Fix failing tests before committing.\n")
    sys.stderr.write("\n")
    sys.stderr.write("=" * 70 + "\n")


def main() -> int:
    """Run the pytest gate check.

    Phase A: Static scan for banned markers in staged files.
    Phase B: Run pytest and verify all tests pass cleanly.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    if not get_bool("python.pytest_gate.enabled", True):
        return 0

    # ------------------------------------------------------------------
    # Phase A: Static scan for banned markers
    # ------------------------------------------------------------------
    all_violations: list[MarkerViolation] = []

    if len(sys.argv) >= MIN_ARGS:
        files_to_check = [
            Path(arg)
            for arg in sys.argv[1:]
            if Path(arg).exists() and Path(arg).suffix == ".py"
        ]
        for filepath in files_to_check:
            try:
                violations = find_banned_markers(filepath)
                all_violations.extend(violations)
            except SyntaxError as exc:
                sys.stderr.write(f"  skipping {filepath}: {exc}\n")
                continue

    if all_violations:
        _report_marker_violations(all_violations)
        return 1

    # ------------------------------------------------------------------
    # Phase B: Run pytest
    # ------------------------------------------------------------------
    sys.stderr.write("Running pytest gate...\n")

    return_code, stdout, stderr = run_pytest()
    counts = parse_summary(stdout)
    result = _PytestResult(
        return_code=return_code,
        counts=counts,
        stdout=stdout,
        stderr=stderr,
    )

    # Check for any failure condition (xfail is allowed)
    has_failures = return_code != 0
    has_skips = counts["skipped"] > 0

    if has_failures or has_skips:
        _report_test_failures(result)
        return 1

    # All tests passed cleanly (xfails are informational, not failures)
    xfail_note = f", {counts['xfailed']} xfailed" if counts["xfailed"] > 0 else ""
    sys.stderr.write(
        f"Pytest gate passed: {counts['passed']} tests, 0 skipped{xfail_note}.\n"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
