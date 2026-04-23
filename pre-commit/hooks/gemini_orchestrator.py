#!/usr/bin/env python3
"""Parallel orchestrator for Gemini-powered code pre-commit checks.

Runs language-agnostic ETHOS checks plus shell-specific review, documentation,
and placeholder detection IN PARALLEL with shared rate limiting.

Usage:
    Pre-commit: python pre-commit/hooks/gemini_orchestrator.py [files...]
    Pre-push: python pre-commit/hooks/gemini_orchestrator.py --full-check

Behavior:
    - If GEMINI_API_KEY is not set, fails (exit 1) - AI review is required
    - Runs all check types in parallel with shared API rate limiting
    - Aggregates results: fails if ANY check has CRITICAL violations
    - On API errors, blocks commit (exit 1) - unverified code cannot be committed

Design:
    - Single asyncio event loop runs all check types concurrently
    - Shared semaphore limits concurrent API requests (rate limiting)
    - Each check type processes its batches concurrently
    - Results aggregated: exit 1 if CRITICAL violations OR API errors

ETHOS.md Section 2: Fail Fast, Fail Hard
    AI code review is a required quality gate. If we cannot verify code
    quality (API errors, missing key), we cannot allow the commit.
"""

import asyncio
import subprocess
import sys
import time
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Final, Self

from ethos_prompts import CODE_ETHOS_PROMPT_TEMPLATE
from hook_config import get_bool, get_str
from gemini_utils import (
    CheckScope,
    FilteredViolations,
    GeminiClientError,
    GeminiResponse,
    Violation,
    collect_changed_lines_async,
    create_client,
    filter_modal_allowed_violations,
    filter_violations_by_diff,
    format_severity_icon,
    generate_with_retry_async,
    load_ethos_quick,
    parse_json_response,
    read_file_safely,
    require_api_key,
)
from google import genai
from shell_prompts import (
    SHELL_DOCUMENTATION_PROMPT_TEMPLATE,
    SHELL_ETHOS_PROMPT_TEMPLATE,
    SHELL_PLACEHOLDER_PROMPT_TEMPLATE,
    SHELL_REVIEW_PROMPT_TEMPLATE,
    SHELLCHECK_SUPPRESSION_PROMPT_TEMPLATE,
)


# =============================================================================
# CONFIGURATION
# =============================================================================

# Rate limiting: maximum concurrent API calls across all checks
MAX_CONCURRENT_API_CALLS: Final[int] = int(
    get_str("gemini.max_concurrent_api_calls", "10")
)

# Timeout per check type (5 minutes)
CHECK_TIMEOUT_SECONDS: Final[int] = int(get_str("gemini.timeout_seconds", "300"))


# =============================================================================
# DATA STRUCTURES
# =============================================================================


def _shell_file_filter(path: Path) -> bool:
    """Filter for shell scripts.

    Args:
        path: File path to check.

    Returns:
        True if file is a shell script.

    """
    # Check extension
    if path.suffix in {".sh", ".bash"}:
        return True

    # Check if in scripts directory (extensionless files are likely shell scripts)
    if ("scripts/" in str(path) or "scripts\\" in str(path)) and path.suffix == "":
        return True

    # Check shebang for extensionless scripts
    # Let exceptions propagate - caller handles unreadable files
    with path.open(encoding="utf-8") as f:
        first_line = f.readline()
        if first_line.startswith("#!") and ("bash" in first_line or "sh" in first_line):
            return True

    return False


def _code_file_filter(path: Path) -> bool:
    """Filter for all code files (language-agnostic ETHOS check).

    Includes Python, shell, Go, and other code files.
    Excludes tests, generated files, and vendored code.

    Args:
        path: File path to check.

    Returns:
        True if file should be checked for ETHOS compliance.

    """
    # Supported code extensions
    code_extensions = {
        ".py",  # Python
        ".pyi",  # Python stubs
        ".sh",  # Shell
        ".bash",  # Bash
        ".go",  # Go
        ".rs",  # Rust
        ".ts",  # TypeScript
        ".js",  # JavaScript
    }

    # Exclude patterns (both absolute and relative paths)
    exclude_patterns = {
        "test_",  # Test files
        "_test.",  # Go test files
        ".test.",  # JS test files
        "/tests/",  # Test directories
        "/test/",  # Test directories
        "/__pycache__/",  # Python cache
        "/node_modules/",  # JS dependencies
        "/vendor/",  # Vendored code
        "/.venv/",  # Virtual environments (absolute)
        "/venv/",  # Virtual environments (absolute)
        "/migrations/",  # Database migrations (often generated)
    }

    # Also exclude paths that START with these patterns (relative paths)
    start_exclude_patterns = {
        ".venv/",  # Virtual environments (relative)
        "venv/",  # Virtual environments (relative)
        "__pycache__/",  # Python cache (relative)
        "node_modules/",  # JS dependencies (relative)
    }

    path_str = str(path)

    # Check exclusions first (patterns anywhere in path)
    for pattern in exclude_patterns:
        if pattern in path_str:
            return False

    # Check patterns at start of path (relative paths)
    for pattern in start_exclude_patterns:
        if path_str.startswith(pattern):
            return False

    # Check extension
    if path.suffix in code_extensions:
        return True

    # Check shebang for extensionless scripts
    # Let exceptions propagate - caller handles unreadable files
    with path.open(encoding="utf-8") as f:
        first_line = f.readline()
        # Check for Python or shell shebang
        if first_line.startswith("#!") and (
            "python" in first_line or "bash" in first_line or "sh" in first_line
        ):
            return True

    return False


@dataclass
class CheckConfig:
    """Configuration for a single check type."""

    name: str
    prompt_template: str
    batch_size: int
    max_file_size_kb: int
    file_filter: Callable[[Path], bool] = _shell_file_filter
    ethos_quick_placeholder: str = ""


@dataclass
class CheckResult:
    """Result from a single check type."""

    check_name: str
    verdict: str  # PASS, FAIL, WARN
    violations: list[Violation]
    filtered: FilteredViolations
    execution_time_ms: float
    batches_processed: int = 0
    error: str = ""


# =============================================================================
# CHECK CONFIGURATIONS
# =============================================================================

CHECK_CONFIGS: Final[list[CheckConfig]] = [
    # Language-agnostic ETHOS check - applies to ALL code
    CheckConfig(
        name="code_ethos",
        prompt_template=CODE_ETHOS_PROMPT_TEMPLATE,
        batch_size=3,  # Smaller batches for thorough analysis
        max_file_size_kb=50,
        file_filter=_code_file_filter,  # All code files, not just shell
    ),
    # Shell-specific checks below
    CheckConfig(
        name="shell_review",
        prompt_template=SHELL_REVIEW_PROMPT_TEMPLATE,
        batch_size=5,
        max_file_size_kb=50,
        ethos_quick_placeholder="ethos_quick",
    ),
    CheckConfig(
        name="shell_ethos",
        prompt_template=SHELL_ETHOS_PROMPT_TEMPLATE,
        batch_size=5,
        max_file_size_kb=30,
    ),
    CheckConfig(
        name="shell_documentation",
        prompt_template=SHELL_DOCUMENTATION_PROMPT_TEMPLATE,
        batch_size=5,
        max_file_size_kb=50,
    ),
    CheckConfig(
        name="shellcheck_suppression",
        prompt_template=SHELLCHECK_SUPPRESSION_PROMPT_TEMPLATE,
        batch_size=8,
        max_file_size_kb=50,
    ),
    CheckConfig(
        name="shell_placeholder",
        prompt_template=SHELL_PLACEHOLDER_PROMPT_TEMPLATE,
        batch_size=10,
        max_file_size_kb=50,
    ),
]

_ALL_CHECKS: Final[list[str]] = [cfg.name for cfg in CHECK_CONFIGS]


def _filter_check_configs(check_names: list[str]) -> list[CheckConfig]:
    """Filter CHECK_CONFIGS to only include requested checks.

    Args:
        check_names: List of check names to include.

    Returns:
        Filtered list of CheckConfig objects.

    Raises:
        ValueError: If any check_name is not found.

    """
    valid_names = {cfg.name for cfg in CHECK_CONFIGS}
    invalid = set(check_names) - valid_names
    if invalid:
        # TRY003: Detailed message aids debugging invalid check configurations
        msg = f"Unknown check types: {sorted(invalid)}. Valid: {sorted(valid_names)}"
        raise ValueError(msg)

    name_set = set(check_names)
    return [cfg for cfg in CHECK_CONFIGS if cfg.name in name_set]


# =============================================================================
# ORCHESTRATOR
# =============================================================================


class GeminiOrchestrator:
    """Orchestrates parallel execution of all Gemini checks.

    Runs all check types concurrently using asyncio, with a shared
    semaphore for API rate limiting across all operations.
    """

    def __init__(
        self,
        semaphore: asyncio.Semaphore,
        client: genai.Client,
        ethos_quick: str,
    ) -> None:
        """Initialize orchestrator with required resources.

        Args:
            semaphore: Rate-limiting semaphore for API calls.
            client: Gemini API client.
            ethos_quick: ETHOS quick reference content for prompts.

        """
        self._semaphore = semaphore
        self._client = client
        self._ethos_quick = ethos_quick

    @classmethod
    async def create(cls) -> Self:
        """Create a fully-initialized orchestrator.

        Returns:
            Fully-initialized GeminiOrchestrator instance.

        """
        semaphore = asyncio.Semaphore(MAX_CONCURRENT_API_CALLS)
        client = create_client()
        ethos_quick = _get_ethos_reference()
        return cls(semaphore, client, ethos_quick)

    async def run_all_checks(
        self,
        files: list[Path],
        scope: str = CheckScope.STAGED,
        check_names: list[str] = _ALL_CHECKS,
    ) -> list[CheckResult]:
        """Run check types in parallel.

        Args:
            files: List of file paths to check.
            scope: CheckScope.STAGED for pre-commit, CheckScope.BRANCH for pre-push.
            check_names: List of check names to run. Defaults to all checks.

        Returns:
            List of CheckResult for each check type.

        """
        if not files:
            return []

        configs_to_run = _filter_check_configs(check_names)

        # Collect changed lines once (shared across all checks)
        changed_lines = await collect_changed_lines_async(files, scope=scope)

        # Run all checks concurrently with timeout
        tasks = [
            asyncio.wait_for(
                self._run_check(cfg, files, changed_lines),
                timeout=CHECK_TIMEOUT_SECONDS,
            )
            for cfg in configs_to_run
        ]

        results = await asyncio.gather(*tasks, return_exceptions=True)

        # Convert exceptions to CheckResult with error
        processed_results: list[CheckResult] = []
        for i, result in enumerate(results):
            if isinstance(result, asyncio.TimeoutError):
                processed_results.append(
                    CheckResult(
                        check_name=configs_to_run[i].name,
                        verdict="ERROR",
                        violations=[],
                        filtered=FilteredViolations(in_diff=[], pre_existing=[]),
                        execution_time_ms=CHECK_TIMEOUT_SECONDS * 1000,
                        error=f"Timeout after {CHECK_TIMEOUT_SECONDS}s",
                    )
                )
            elif isinstance(result, Exception):
                processed_results.append(
                    CheckResult(
                        check_name=configs_to_run[i].name,
                        verdict="ERROR",
                        violations=[],
                        filtered=FilteredViolations(in_diff=[], pre_existing=[]),
                        execution_time_ms=0,
                        error=str(result),
                    )
                )
            elif isinstance(result, CheckResult):
                processed_results.append(result)

        return processed_results

    async def _run_check(
        self,
        config: CheckConfig,
        files: list[Path],
        changed_lines: dict[str, set[int]],
    ) -> CheckResult:
        """Run a single check type with parallel batches.

        Args:
            config: Check configuration.
            files: All files to potentially check.
            changed_lines: Pre-collected changed lines for diff filtering.

        Returns:
            CheckResult for this check type.

        """
        start_time = time.perf_counter()

        # Filter files for this check - fail if any file is unreadable
        check_files: list[Path] = []
        for f in files:
            try:
                if config.file_filter(f):
                    check_files.append(f)
            except (OSError, UnicodeDecodeError) as exc:
                # Cannot determine file type - this is a critical failure
                return CheckResult(
                    check_name=config.name,
                    verdict="ERROR",
                    violations=[],
                    filtered=FilteredViolations(in_diff=[], pre_existing=[]),
                    execution_time_ms=(time.perf_counter() - start_time) * 1000,
                    error=f"Cannot read file {f}: {exc}",
                )

        if not check_files:
            return CheckResult(
                check_name=config.name,
                verdict="PASS",
                violations=[],
                filtered=FilteredViolations(in_diff=[], pre_existing=[]),
                execution_time_ms=0,
            )

        # Collect and batch file contents
        contents = self._collect_contents(check_files, config.max_file_size_kb)
        if not contents:
            return CheckResult(
                check_name=config.name,
                verdict="PASS",
                violations=[],
                filtered=FilteredViolations(in_diff=[], pre_existing=[]),
                execution_time_ms=(time.perf_counter() - start_time) * 1000,
            )

        batches = self._create_batches(contents, config.batch_size)

        # Run batches in parallel (rate-limited by semaphore)
        batch_tasks = [
            self._run_batch(config, batch, i + 1, len(batches))
            for i, batch in enumerate(batches)
        ]
        batch_results = await asyncio.gather(*batch_tasks, return_exceptions=True)

        # Aggregate violations
        all_violations: list[Violation] = []
        batch_errors = 0
        batches_completed = 0

        for batch_result in batch_results:
            if isinstance(batch_result, Exception):
                sys.stderr.write(f"  [{config.name}] Batch error: {batch_result}\n")
                batch_errors += 1
            elif isinstance(batch_result, GeminiResponse):
                all_violations.extend(batch_result.violations)
                batches_completed += 1

        # Apply explicit local waivers before diff-based reporting.
        effective_violations = filter_modal_allowed_violations(all_violations)

        # Filter by diff
        filtered = filter_violations_by_diff(effective_violations, changed_lines)

        # Determine final verdict
        final_verdict = _determine_verdict(filtered, batch_errors, batches_completed)

        execution_time_ms = (time.perf_counter() - start_time) * 1000

        return CheckResult(
            check_name=config.name,
            verdict=final_verdict,
            violations=effective_violations,
            filtered=filtered,
            execution_time_ms=execution_time_ms,
            batches_processed=len(batches),
        )

    async def _run_batch(
        self,
        config: CheckConfig,
        batch: list[str],
        _batch_num: int,
        _total_batches: int,
    ) -> GeminiResponse:
        """Run a single batch with API rate limiting.

        Args:
            config: Check configuration.
            batch: List of file content strings for this batch.
            _batch_num: Current batch number (1-indexed, unused).
            _total_batches: Total number of batches (unused).

        Returns:
            Parsed GeminiResponse.

        """
        code_content = "\n".join(batch)
        format_kwargs: dict[str, str] = {"code_content": code_content}

        if config.ethos_quick_placeholder:
            format_kwargs[config.ethos_quick_placeholder] = self._ethos_quick

        prompt = config.prompt_template.format(
            **format_kwargs,
            project_name=get_str("gemini.repo_name", "coding-ethos"),
            project_context=get_str(
                "gemini.repo_context",
                "shared repository automation and engineering tooling",
            ),
        )
        response_text = await generate_with_retry_async(
            self._client,
            prompt,
            self._semaphore,
        )

        return parse_json_response(response_text)

    def _collect_contents(self, files: list[Path], max_size_kb: int) -> list[str]:
        """Collect readable file contents from paths.

        Args:
            files: List of file paths to read.
            max_size_kb: Maximum file size in KB.

        Returns:
            List of formatted file content strings.

        Raises:
            OSError: If any file cannot be read.
            UnicodeDecodeError: If any file contains invalid UTF-8.

        """
        contents: list[str] = []
        for path in files:
            # Let exceptions propagate - partial checks are not valid checks
            content = read_file_safely(path, max_size_kb)
            if content:
                contents.append(f"--- {path} ---\n{content}\n")
        return contents

    def _create_batches(self, contents: list[str], batch_size: int) -> list[list[str]]:
        """Create batches from file contents.

        Args:
            contents: List of file content strings.
            batch_size: Maximum files per batch.

        Returns:
            List of batches.

        """
        return [
            contents[i : i + batch_size] for i in range(0, len(contents), batch_size)
        ]


# =============================================================================
# HELPER FUNCTIONS
# =============================================================================


def _get_ethos_reference() -> str:
    """Get ETHOS quick reference content.

    ETHOS.md is required for quality AI code review. If not found,
    the review quality would degrade without explicit failure.

    Returns:
        ETHOS quick reference content.

    Raises:
        GeminiClientError: If ETHOS.md cannot be loaded.

    """
    ethos_quick = load_ethos_quick()
    if not ethos_quick:
        # ETHOS §7: Required capabilities must exist, not silently degrade
        msg = (
            "ETHOS.md not found. AI code review requires ETHOS reference. "
            "Ensure ETHOS.md exists in the configured project roots."
        )
        raise GeminiClientError(msg, retryable=False)
    return ethos_quick


def _determine_verdict(
    filtered: FilteredViolations, batch_errors: int, batches_completed: int
) -> str:
    """Determine final verdict based on filtered violations and batch status.

    Args:
        filtered: Filtered violations from diff analysis.
        batch_errors: Number of batches that failed with errors.
        batches_completed: Number of batches that completed successfully.

    Returns:
        Verdict string: FAIL, ERROR, WARN, or PASS.

    """
    if filtered.has_blocking_criticals:
        return "FAIL"
    if batch_errors > 0 and batches_completed == 0:
        return "ERROR"
    if batch_errors > 0:
        return "WARN"
    if filtered.in_diff and any(v.severity == "WARNING" for v in filtered.in_diff):
        return "WARN"
    return "PASS"


class GitCommandError(Exception):
    """Raised when a git command fails."""


class FileReadError(Exception):
    """Raised when a file cannot be read for type detection."""


def get_changed_files_for_push() -> list[Path]:
    """Get files changed between HEAD and origin/main for pre-push.

    Returns:
        List of changed code file paths (Python, shell, etc.).

    Raises:
        GitCommandError: If git diff command fails.

    """
    # S607: Safe - hardcoded git command in pre-commit hook
    result = subprocess.run(
        ["git", "diff", "--name-only", "origin/main...HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        msg = f"git diff failed: {result.stderr.strip()}"
        raise GitCommandError(msg)

    files: list[Path] = []
    for f in result.stdout.strip().split("\n"):
        if not f:
            continue
        path = Path(f)
        if not path.exists():
            continue
        # Use code filter to include all checkable files
        try:
            if _code_file_filter(path):
                files.append(path)
        except (OSError, UnicodeDecodeError) as exc:
            msg = f"Cannot read file for type detection: {path}: {exc}"
            raise FileReadError(msg) from exc
    return files


# =============================================================================
# REPORTING
# =============================================================================


@dataclass(frozen=True, slots=True)
class _VerdictCounts:
    """Counts of each verdict type."""

    passed: int
    warned: int
    failed: int
    errored: int


def _count_verdicts(results: list[CheckResult]) -> _VerdictCounts:
    """Count results by verdict type.

    Args:
        results: List of check results.

    Returns:
        Verdict counts dataclass.

    """
    return _VerdictCounts(
        passed=sum(1 for r in results if r.verdict == "PASS"),
        warned=sum(1 for r in results if r.verdict == "WARN"),
        failed=sum(1 for r in results if r.verdict == "FAIL"),
        errored=sum(1 for r in results if r.verdict == "ERROR" or r.error),
    )


def _format_single_result(result: CheckResult) -> list[str]:
    """Format a single check result for output.

    Args:
        result: The check result to format.

    Returns:
        List of formatted lines.

    """
    lines: list[str] = []
    icon = _get_verdict_icon(result.verdict, result.error)
    time_str = f"{result.execution_time_ms:.0f}ms"

    if result.error:
        lines.append(f"{icon} {result.check_name}: ERROR ({time_str})")
        lines.append(f"   {result.error}")
        return lines

    lines.append(f"{icon} {result.check_name}: {result.verdict} ({time_str})")
    lines.extend(_format_result_details(result))
    return lines


def _format_result_details(result: CheckResult) -> list[str]:
    """Format the detail lines for a non-error result.

    Shows ALL violations - CRITICAL, WARNING, INFO, and pre-existing.

    Args:
        result: The check result (must not have error).

    Returns:
        List of detail lines.

    """
    lines: list[str] = []

    # Show ALL violations in changed code (grouped by severity)
    if result.filtered.in_diff:
        lines.append("   [In your changes]")
        for v in result.filtered.in_diff:
            v_icon = format_severity_icon(v.severity)
            lines.append(f"   {v_icon} {v.file}:{v.line or '?'}")
            lines.append(f"       {v.message}")
            if v.ethos_section:
                lines.append(f"       (ETHOS {v.ethos_section})")

    # Show ALL pre-existing issues
    if result.filtered.pre_existing:
        lines.append(f"   [Pre-existing ({len(result.filtered.pre_existing)})]")
        for v in result.filtered.pre_existing:
            v_icon = format_severity_icon(v.severity)
            lines.append(f"   {v_icon} {v.file}:{v.line or '?'}")
            lines.append(f"       {v.message}")
            if v.ethos_section:
                lines.append(f"       (ETHOS {v.ethos_section})")

    return lines


def format_results(results: list[CheckResult]) -> str:
    """Format check results for terminal output.

    Quiet by default: prints nothing when all checks pass with no
    in-diff violations. Only prints details for FAIL, ERROR, WARN,
    or when pre-existing issues are found.

    Args:
        results: List of check results.

    Returns:
        Formatted report string (empty string when all clean).

    """
    counts = _count_verdicts(results)

    # Actionable issues: in-diff violations, failures, errors
    has_actionable = (
        counts.failed > 0
        or counts.errored > 0
        or counts.warned > 0
        or any(r.filtered.in_diff for r in results)
    )

    # All clean — zero output (pre-existing issues are informational,
    # they don't warrant a full report on every commit)
    if not has_actionable:
        return ""

    lines = [
        "",
        "=" * 70,
        "GEMINI AI CODE CHECKS (PARALLEL)",
        "=" * 70,
        "",
    ]

    total_time_ms = sum(r.execution_time_ms for r in results)

    lines.append(
        f"Summary: {counts.passed} passed, {counts.warned} warned, "
        f"{counts.failed} failed, {counts.errored} errors"
    )
    lines.append(f"Total time: {total_time_ms:.0f}ms (parallel execution)")
    lines.append("")

    # Only print results that have actionable items
    for result in results:
        if result.error or result.filtered.in_diff:
            lines.extend(_format_single_result(result))
            lines.append("")

    lines.append("=" * 70)

    return "\n".join(lines)


def _get_verdict_icon(verdict: str, error: str) -> str:
    """Get icon for verdict.

    Args:
        verdict: PASS, FAIL, WARN, or ERROR.
        error: Error message if any.

    Returns:
        Two-character icon.

    """
    if error:
        return "W "
    return {"PASS": "OK", "FAIL": "XX", "WARN": "W ", "ERROR": "!!"}.get(verdict, "??")


def report_and_exit(results: list[CheckResult]) -> int:
    """Print report and return exit code.

    Args:
        results: List of check results.

    Returns:
        Exit code (0 = pass, 1 = fail).

    """
    report = format_results(results)
    if report:
        print(report)

    has_blocking = any(
        r.filtered.has_blocking_criticals for r in results if not r.error
    )

    if has_blocking:
        sys.stderr.write(
            "\nXX Commit blocked: CRITICAL violations in your changes\n"
            "   Fix the issues above and try again.\n\n"
        )
        return 1

    has_errors = any(r.verdict == "ERROR" for r in results)
    if has_errors:
        sys.stderr.write(
            "\nXX Commit blocked: API errors prevented code verification.\n"
            "   Cannot commit unverified code. Retry or check API status.\n\n"
        )
        return 1

    has_warnings = any(r.filtered.has_any_in_diff for r in results if not r.error)
    if has_warnings:
        sys.stderr.write(
            "\nW  Issues found in your changes but commit allowed.\n"
            "   Consider addressing the issues above.\n\n"
        )

    return 0


# =============================================================================
# MAIN ENTRY POINT
# =============================================================================


def _parse_check_type() -> str:
    """Parse --check-type argument from sys.argv.

    Returns:
        Check type name if specified, empty string if not provided.

    """
    for i, arg in enumerate(sys.argv):
        if arg == "--check-type" and i + 1 < len(sys.argv):
            return sys.argv[i + 1]
        if arg.startswith("--check-type="):
            return arg.split("=", 1)[1]
    return ""


def _get_files_to_check() -> tuple[list[Path], str]:
    """Parse arguments and return files to check with scope.

    Returns:
        Tuple of (files, scope). Files list is empty if nothing to check.

    """
    full_check = "--full-check" in sys.argv

    if full_check:
        sys.stderr.write("Pre-push mode: checking all branch changes...\n")
        files = get_changed_files_for_push()
        if not files:
            sys.stderr.write("    No code files changed in branch\n")
            return [], ""
        sys.stderr.write(f"    Found {len(files)} code file(s) to check\n\n")
        return files, CheckScope.BRANCH

    # Pre-commit mode: files passed as arguments
    min_argc = 2
    if len(sys.argv) < min_argc:
        return [], ""

    arg_files: list[Path] = []
    for f in sys.argv[1:]:
        if f.startswith("--"):
            continue
        path = Path(f)
        if not path.exists():
            continue
        # Accept all code files - individual checks filter by type
        try:
            if _code_file_filter(path):
                arg_files.append(path)
        except (OSError, UnicodeDecodeError) as exc:
            msg = f"Cannot read file for type detection: {path}: {exc}"
            raise FileReadError(msg) from exc

    if not arg_files:
        return [], ""

    return arg_files, CheckScope.STAGED


async def _run_orchestrator(
    files: list[Path], scope: str, check_names: list[str]
) -> list[CheckResult]:
    """Run the orchestrator with proper async initialization.

    Args:
        files: Files to check.
        scope: CheckScope.STAGED or CheckScope.BRANCH.
        check_names: List of check names to run.

    Returns:
        List of check results.

    """
    orchestrator = await GeminiOrchestrator.create()
    return await orchestrator.run_all_checks(
        files, scope=scope, check_names=check_names
    )


def main() -> int:
    """Run Gemini AI code checks as a pre-commit hook.

    Supports:
        - Default: Run all checks in parallel
        - --full-check: Pre-push mode (check all branch changes)
        - --check-type <name>: Run single check type

    Returns:
        Exit code (0 = pass, 1 = fail with violations or errors).

    ETHOS.md Section 2: Fail fast on configuration errors.

    """
    if not get_bool("gemini.enabled", False):
        return 0

    # Validate API key - raises GeminiClientError if missing
    require_api_key()

    files, scope = _get_files_to_check()
    if not files:
        return 0
    check_type = _parse_check_type()
    check_names = [check_type] if check_type else _ALL_CHECKS

    try:
        results = asyncio.run(_run_orchestrator(files, scope, check_names))
        return report_and_exit(results)

    except GeminiClientError as exc:
        sys.stderr.write(f"\nXX Gemini checks failed: {exc}\n")
        sys.stderr.write("    AI code review is required. Fix the error above.\n\n")
        return 1
    except GitCommandError as exc:
        sys.stderr.write(f"\nXX Git command failed: {exc}\n")
        sys.stderr.write("    Cannot determine changed files for review.\n\n")
        return 1
    except FileReadError as exc:
        sys.stderr.write(f"\nXX File read error: {exc}\n")
        sys.stderr.write("    Cannot determine file type for review.\n\n")
        return 1
    except ValueError as exc:
        sys.stderr.write(f"\nXX Configuration error: {exc}\n")
        return 1


if __name__ == "__main__":
    sys.exit(main())
