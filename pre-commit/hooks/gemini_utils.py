"""Shared utilities for Gemini-powered pre-commit hooks.

This module provides:
- Gemini client creation with API key from environment
- Retry logic with exponential backoff for transient errors
- Structured response parsing for code review results
- File reading utilities with size limits
- Diff-aware violation filtering

ETHOS.md Section 2: Fail fast if GEMINI_API_KEY is not configured.
AI code review is a required quality gate, not an optional enhancement.
"""

import asyncio
import json
import os
import re
import subprocess
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Final

from google import genai
from google.api_core import exceptions as google_exceptions

from hook_config import get, get_list, get_str


# Model configuration.
GEMINI_MODEL: Final[str] = get_str("gemini.model", "gemini-2.5-flash")
MAX_RETRIES: Final[int] = int(get("gemini.max_retries", 3))
TIMEOUT_SECONDS: Final[int] = int(get("gemini.timeout_seconds", 120))
INITIAL_BACKOFF_SECONDS: Final[float] = float(
    get("gemini.initial_backoff_seconds", 1.0)
)
_MODAL_ALLOWED_PATTERN: Final[re.Pattern[str]] = re.compile(r"#\s*modal-allowed\b")
_FILE_WIDE_MODAL_ALLOWED_PATTERN: Final[re.Pattern[str]] = re.compile(
    r"^#\s+modal-allowed\s*$"
)


class CheckScope:
    """Scope of files to check.

    Each scope type determines which git diff command is used
    to collect changed lines.
    """

    STAGED = "staged"  # Pre-commit: only staged changes
    BRANCH = "branch"  # Pre-push: all changes since origin/main


@dataclass
class Violation:
    """A single code review violation.

    Attributes:
        severity: Violation severity (CRITICAL, WARNING, INFO).
        file: Path to the file containing the violation.
        line: Source line number (0 means unknown/unspecified).
        message: Human-readable violation description.
        ethos_section: ETHOS section reference (empty string if none).

    """

    severity: str  # CRITICAL, WARNING, INFO
    file: str
    line: int
    message: str
    ethos_section: str = ""


@dataclass
class GeminiResponse:
    """Structured response from Gemini analysis."""

    verdict: str  # PASS, FAIL, WARN
    violations: list[Violation] = field(default_factory=list[Violation])
    suggestions: list[dict[str, str]] = field(default_factory=list[dict[str, str]])
    raw_response: str = ""


class GeminiClientError(Exception):
    """Error during Gemini API interaction."""

    def __init__(self, message: str, *, retryable: bool = False) -> None:
        """Initialize error.

        Args:
            message: Error description.
            retryable: Whether the error is transient and can be retried.

        """
        super().__init__(message)
        self.retryable = retryable


def get_api_key() -> str:
    """Get Gemini API key from environment.

    Returns:
        API key if set, empty string if not configured.

    """
    return os.environ.get("GEMINI_API_KEY", "")


def require_api_key() -> None:
    """Validate that GEMINI_API_KEY is configured.

    ETHOS.md Section 2: Fail fast on missing configuration.
    AI code review is a required quality gate.

    Raises:
        GeminiClientError: If GEMINI_API_KEY is not set.

    """
    if not get_api_key():
        # TRY003: Detailed message is needed for user guidance
        msg = (
            "GEMINI_API_KEY not set. "
            "AI code review is required. "
            "Add GEMINI_API_KEY to your .env file."
        )
        raise GeminiClientError(msg)


def create_client() -> genai.Client:
    """Create Gemini client with API key.

    Returns:
        Configured Gemini client.

    Raises:
        GeminiClientError: If API key is not set.

    """
    api_key = get_api_key()
    if not api_key:
        # TRY003: Detailed message helps users understand how to fix the issue
        msg = (
            "GEMINI_API_KEY is required but not set. "
            "Set GEMINI_API_KEY in .env to enable AI code review."
        )
        raise GeminiClientError(msg, retryable=False)
    return genai.Client(api_key=api_key)


# Retryable exception types
RETRYABLE_EXCEPTIONS: tuple[type[Exception], ...] = (
    google_exceptions.ResourceExhausted,
    google_exceptions.DeadlineExceeded,
    google_exceptions.ServiceUnavailable,
)


def _handle_retryable_error(
    exc: Exception,
    attempt: int,
    max_retries: int,
    backoff: float,
) -> float:
    """Handle a retryable error with backoff.

    Args:
        exc: The exception that occurred.
        attempt: Current attempt number (0-indexed).
        max_retries: Maximum retry attempts.
        backoff: Current backoff duration in seconds.

    Returns:
        New backoff duration after sleeping.

    """
    error_messages: dict[type[Exception], str] = {
        google_exceptions.ResourceExhausted: "Rate limited",
        google_exceptions.DeadlineExceeded: "Timeout",
        google_exceptions.ServiceUnavailable: "Service unavailable",
    }
    msg = error_messages.get(type(exc), "Error")
    sys.stderr.write(
        f"  {msg}, waiting {backoff:.1f}s (attempt {attempt + 1}/{max_retries + 1})\n"
    )
    time.sleep(backoff)
    return backoff * 2


def _generate_sync_inner(
    client: genai.Client,
    prompt: str,
    model: str,
    max_retries: int,
) -> str:
    """Generate text synchronously with retry logic.

    Args:
        client: Gemini client instance.
        prompt: The prompt to send.
        model: Model identifier.
        max_retries: Maximum retry attempts.

    Returns:
        Generated text response.

    Raises:
        GeminiClientError: On non-retryable errors or retry exhaustion.

    """
    backoff = INITIAL_BACKOFF_SECONDS
    last_error: Exception = GeminiClientError("No attempts made", retryable=False)

    for attempt in range(max_retries + 1):
        try:
            response = client.models.generate_content(
                model=model,
                contents=prompt,
            )
        except RETRYABLE_EXCEPTIONS as exc:
            last_error = exc
            if attempt < max_retries:
                backoff = _handle_retryable_error(exc, attempt, max_retries, backoff)
            continue

        except google_exceptions.InvalidArgument as exc:
            msg = f"Invalid request: {exc}"
            raise GeminiClientError(msg, retryable=False) from exc

        except google_exceptions.PermissionDenied as exc:
            msg = f"Permission denied - check API key: {exc}"
            raise GeminiClientError(msg, retryable=False) from exc

        except Exception as exc:
            msg = f"Unexpected error: {exc}"
            raise GeminiClientError(msg, retryable=False) from exc

        else:
            return response.text or ""

    msg = f"Retry exhausted after {max_retries + 1} attempts: {last_error}"
    raise GeminiClientError(msg, retryable=False)


async def generate_with_retry_async(
    client: genai.Client,
    prompt: str,
    semaphore: asyncio.Semaphore,
    *,
    model: str = GEMINI_MODEL,
    max_retries: int = MAX_RETRIES,
) -> str:
    """Async generate with semaphore-controlled rate limiting.

    Uses a semaphore to limit concurrent API calls across all parallel checks.
    The actual API call runs in a thread pool since the Gemini SDK is synchronous.

    Args:
        client: Gemini client instance.
        prompt: The prompt to send.
        semaphore: Rate limiting semaphore (shared across all parallel operations).
        model: Model identifier.
        max_retries: Maximum retry attempts.

    Returns:
        Generated text response.

    Raises:
        GeminiClientError: On non-retryable errors or retry exhaustion.

    """
    async with semaphore:
        return await asyncio.to_thread(
            _generate_sync_inner,
            client,
            prompt,
            model,
            max_retries,
        )


def read_file_safely(path: Path, max_size_kb: int = 100) -> str:
    """Read file content with size limit.

    Returns empty string for intentional skips (file too large, doesn't exist).
    Raises exceptions for actual errors (permission denied, encoding issues).

    Args:
        path: File path to read.
        max_size_kb: Maximum file size in KB.

    Returns:
        File content, or empty string if file doesn't exist or exceeds size limit.

    Raises:
        OSError: If file cannot be read (permission denied, etc.).
        UnicodeDecodeError: If file contains invalid UTF-8.

    """
    if not path.exists():
        return ""

    if path.stat().st_size > max_size_kb * 1024:
        sys.stderr.write(f"  Skipping {path} (>{max_size_kb}KB)\n")
        return ""

    # Let errors propagate - caller decides how to handle
    return path.read_text(encoding="utf-8")


def parse_json_response(response_text: str) -> GeminiResponse:
    """Parse Gemini JSON response into structured format.

    Args:
        response_text: Raw response from Gemini.

    Returns:
        Parsed GeminiResponse object.

    Raises:
        ValueError: If response cannot be parsed.

    """
    text = response_text.strip()
    text = text.removeprefix("```json")
    text = text.removeprefix("```")
    text = text.removesuffix("```")
    text = text.strip()

    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        msg = f"Invalid JSON response: {exc}"
        raise ValueError(msg) from exc

    violations: list[Violation] = []
    for v in data.get("violations", []):
        message = _strip_modal_allowed_commentary(v.get("message", "No message"))
        if not message:
            continue
        violations.append(
            Violation(
                severity=v.get("severity", "INFO"),
                file=v.get("file", "unknown"),
                line=v.get("line", 0),
                message=message,
                ethos_section=v.get("ethos_section", ""),
            )
        )

    return GeminiResponse(
        verdict=data.get("verdict", "PASS"),
        violations=violations,
        suggestions=data.get("suggestions", []),
        raw_response=response_text,
    )


def _strip_modal_allowed_commentary(message: str) -> str:
    """Remove Gemini commentary about the ``# modal-allowed`` marker.

    The marker behavior is enforced deterministically in hook code. Gemini
    mentioning the marker in a finding adds noise without adding signal.

    Args:
        message: Gemini-produced violation message.

    Returns:
        Cleaned message with marker-related commentary removed.

    """
    if "modal-allowed" not in message.casefold():
        return message

    cleaned_lines: list[str] = []
    for raw_line in message.splitlines():
        parts = re.split(r"(?<=[.!?])\s+", raw_line.strip())
        kept_parts = [part for part in parts if "modal-allowed" not in part.casefold()]
        if kept_parts:
            cleaned_lines.append(" ".join(kept_parts))

    return "\n".join(cleaned_lines).strip()


def _is_modal_violation(violation: Violation) -> bool:
    """Return whether a violation is a suppressible ETHOS §19/modal finding.

    The ``# modal-allowed`` marker only waives Gemini's modal-behavior
    findings. It does not suppress combined Sections 5+7+19
    optional-capability findings.

    Args:
        violation: Parsed Gemini violation.

    Returns:
        True if the violation is a modal warning that may be waived.

    """
    text = f"{violation.ethos_section} {violation.message}".lower()

    modal_section = (
        "section 19" in text
        or "one path for critical operations" in text
        or "sections 5+7+19" in text
        or "no optional internal state for capabilities" in text
        or "section 7" in text
        or "if available" in text
    )
    modal_shape = any(
        marker in text
        for marker in (
            "modal",
            "gates the",
            "gates ",
            "gating feature enablement",
            "conditionally disables",
            "conditional execution paths",
            "different execution paths",
            "based on a configuration field",
            "based on an input type",
            "via configuration",
            "enabled/disabled",
            "silently degrade",
            "silent degradation",
            "skipping the",
            "skip the",
            "full job",
        )
    )
    non_modal_section_7 = (
        "section 7" in text
        and not modal_shape
        and "sections 5+7+19" not in text
        and "if available" not in text
    )

    return modal_section and modal_shape and not non_modal_section_7


def _find_modal_allowed_lines(content: str) -> set[int]:
    """Find source lines covered by ``# modal-allowed``.

    Supported forms:
    - Inline on the same source line as the modal construct.
    - On a dedicated comment line immediately above the target line.

    Args:
        content: Full file contents.

    Returns:
        1-indexed source lines whose modal warnings should be waived.

    """
    lines = content.splitlines()

    if any(_FILE_WIDE_MODAL_ALLOWED_PATTERN.match(line) for line in lines):
        return set(range(1, len(lines) + 1))

    allowed_lines: set[int] = set()

    for line_number, line in enumerate(lines, start=1):
        match = _MODAL_ALLOWED_PATTERN.search(line)
        if match is None:
            continue

        allowed_lines.add(line_number)

        if line.lstrip().startswith("#"):
            for target_line in range(line_number + 1, len(lines) + 1):
                stripped = lines[target_line - 1].strip()
                if not stripped:
                    continue
                if stripped.startswith("#"):
                    continue
                allowed_lines.add(target_line)
                break

    return allowed_lines


def filter_modal_allowed_violations(violations: list[Violation]) -> list[Violation]:
    """Remove Gemini modal warnings waived by ``# modal-allowed`` comments.

    Args:
        violations: Violations returned by Gemini.

    Returns:
        Violations with local modal waivers applied.

    """
    allowed_lines_by_file: dict[str, set[int]] = {}
    filtered: list[Violation] = []

    for violation in violations:
        if not _is_modal_violation(violation) or violation.line <= 0:
            filtered.append(violation)
            continue

        if violation.file not in allowed_lines_by_file:
            path = Path(violation.file)
            try:
                content = path.read_text(encoding="utf-8")
            except (OSError, UnicodeDecodeError):
                allowed_lines_by_file[violation.file] = set()
            else:
                allowed_lines_by_file[violation.file] = _find_modal_allowed_lines(
                    content
                )

        if violation.line in allowed_lines_by_file[violation.file]:
            continue

        filtered.append(violation)

    return filtered


def load_ethos_quick() -> str:
    """Load ETHOS.md quick reference for inclusion in prompts.

    Returns:
        Content of ETHOS.md or a summary if not found.

    """
    candidates = [Path(str(item)) for item in get_list("project.ethos_candidates", ["ETHOS.md"])]
    for ethos_path in candidates:
        if ethos_path.exists():
            try:
                # Always return full ETHOS.md - no truncation
                return ethos_path.read_text(encoding="utf-8")
            except (OSError, UnicodeDecodeError):
                pass
    return ""


def format_severity_icon(severity: str) -> str:
    """Get text icon for severity level.

    Args:
        severity: CRITICAL, WARNING, or INFO.

    Returns:
        Appropriate text icon.

    """
    return {
        "CRITICAL": "XX",
        "WARNING": "W ",
        "INFO": "-- ",
    }.get(severity, "??")


def get_changed_lines_for_file(
    file_path: Path, *, scope: str = CheckScope.STAGED
) -> set[int]:
    """Get line numbers that were changed in a file.

    Parses git diff output to extract line numbers that are new or modified.
    Used for diff-aware violation filtering.

    Args:
        file_path: Path to the file to check.
        scope: CheckScope.STAGED for staged changes (pre-commit),
               CheckScope.BRANCH for all changes since origin/main (pre-push).

    Returns:
        Set of line numbers that were changed/added.

    """
    try:
        if scope == CheckScope.STAGED:
            cmd = ["git", "diff", "--no-ext-diff", "-U0", "--staged", str(file_path)]
        else:
            cmd = [
                "git",
                "diff",
                "--no-ext-diff",
                "-U0",
                "origin/main...HEAD",
                "--",
                str(file_path),
            ]

        # S603: Safe - hardcoded git command, file_path from pre-commit (trusted)
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        sys.stderr.write(f"  Failed to get diff for {file_path}: {exc}\n")
        return set()
    else:
        if result.returncode != 0:
            return set()

        diff_output = result.stdout
        if not diff_output.strip():
            return set()

        changed_lines: set[int] = set()
        hunk_pattern = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")

        for line in diff_output.splitlines():
            match = hunk_pattern.match(line)
            if match:
                start = int(match.group(1))
                count = int(match.group(2)) if match.group(2) else 1
                changed_lines.update(range(start, start + count))

        return changed_lines


@dataclass
class FilteredViolations:
    """Violations categorized by whether they're in changed code."""

    in_diff: list[Violation]
    pre_existing: list[Violation]

    @property
    def has_blocking_criticals(self) -> bool:
        """Check if there are CRITICAL violations in changed code."""
        return any(v.severity == "CRITICAL" for v in self.in_diff)

    @property
    def has_any_in_diff(self) -> bool:
        """Check if there are any violations in changed code."""
        return len(self.in_diff) > 0


def filter_violations_by_diff(
    violations: list[Violation],
    changed_lines_by_file: dict[str, set[int]],
) -> FilteredViolations:
    """Categorize violations based on whether they're in changed code.

    Args:
        violations: List of violations from Gemini.
        changed_lines_by_file: Map of file path to set of changed line numbers.

    Returns:
        FilteredViolations with violations categorized.

    """
    in_diff: list[Violation] = []
    pre_existing: list[Violation] = []

    for v in violations:
        if v.line == 0:
            in_diff.append(v)
            continue

        changed_lines = changed_lines_by_file.get(v.file, set())

        if not changed_lines:
            file_path = Path(v.file)
            if file_path.exists():
                # S603: hardcoded git cmd, trusted pre-commit path
                cmd = ["git", "status", "--porcelain", str(file_path)]
                result = subprocess.run(
                    cmd, capture_output=True, text=True, check=False
                )
                if result.stdout.startswith("A ") or result.stdout.startswith("?? "):
                    in_diff.append(v)
                else:
                    pre_existing.append(v)
            else:
                in_diff.append(v)
            continue

        if v.line in changed_lines:
            in_diff.append(v)
        else:
            pre_existing.append(v)

    return FilteredViolations(in_diff=in_diff, pre_existing=pre_existing)


def collect_changed_lines(
    files: list[Path], *, scope: str = CheckScope.STAGED
) -> dict[str, set[int]]:
    """Collect changed line numbers for all files.

    Args:
        files: List of file paths to check.
        scope: CheckScope.STAGED for staged changes (pre-commit),
               CheckScope.BRANCH for all changes since origin/main (pre-push).

    Returns:
        Dict mapping file path (as string) to set of changed line numbers.

    """
    result: dict[str, set[int]] = {}
    for file_path in files:
        changed = get_changed_lines_for_file(file_path, scope=scope)
        result[str(file_path)] = changed
    return result


async def collect_changed_lines_async(
    files: list[Path],
    *,
    scope: str = CheckScope.STAGED,
) -> dict[str, set[int]]:
    """Async version of collect_changed_lines.

    Args:
        files: List of file paths to check.
        scope: CheckScope.STAGED or CheckScope.BRANCH.

    Returns:
        Dict mapping file path (as string) to set of changed line numbers.

    """
    return await asyncio.to_thread(collect_changed_lines, files, scope=scope)
