import asyncio
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final

from google import genai

GEMINI_MODEL: Final[str]
MAX_RETRIES: Final[int]
TIMEOUT_SECONDS: Final[int]
INITIAL_BACKOFF_SECONDS: Final[float]
RETRYABLE_EXCEPTIONS: tuple[type[Exception], ...]

class CheckScope:
    STAGED: str
    BRANCH: str

@dataclass
class Violation:
    severity: str
    file: str
    line: int
    message: str
    ethos_section: str = ""

@dataclass
class GeminiResponse:
    verdict: str
    violations: list[Violation]
    suggestions: list[dict[str, Any]]
    raw_response: str

class GeminiClientError(Exception):
    retryable: bool
    def __init__(self, message: str, *, retryable: bool = False) -> None: ...

@dataclass
class FilteredViolations:
    in_diff: list[Violation]
    pre_existing: list[Violation]
    @property
    def has_blocking_criticals(self) -> bool: ...
    @property
    def has_any_in_diff(self) -> bool: ...

def get_api_key() -> str: ...
def require_api_key() -> None: ...
def create_client() -> genai.Client: ...
async def generate_with_retry_async(
    client: genai.Client,
    prompt: str,
    semaphore: asyncio.Semaphore,
    *,
    model: str = ...,
    max_retries: int = ...,
) -> str: ...
def read_file_safely(path: Path, max_size_kb: int = 100) -> str: ...
def parse_json_response(response_text: str) -> GeminiResponse: ...
def filter_modal_allowed_violations(
    violations: list[Violation],
) -> list[Violation]: ...
def load_ethos_quick() -> str: ...
def format_severity_icon(severity: str) -> str: ...
def get_changed_lines_for_file(file_path: Path, *, scope: str = ...) -> set[int]: ...
def filter_violations_by_diff(
    violations: list[Violation],
    changed_lines_by_file: dict[str, set[int]],
) -> FilteredViolations: ...
def collect_changed_lines(
    files: list[Path], *, scope: str = ...
) -> dict[str, set[int]]: ...
async def collect_changed_lines_async(
    files: list[Path],
    *,
    scope: str = ...,
) -> dict[str, set[int]]: ...
