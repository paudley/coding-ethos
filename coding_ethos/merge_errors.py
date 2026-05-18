# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Exceptions raised by external merge engine execution.

Responsibility is narrow.
Public imports stay aligned.
"""


class UnsupportedMergeEngineError(ValueError):
    """Raised when a configured merge engine name is not supported."""

    def __init__(self, engine: str) -> None:
        """Initialize the error with the unsupported engine name."""
        super().__init__(f"Unsupported merge engine: {engine}")


class MergeTimeoutError(RuntimeError):
    """Raised when an external merge engine exceeds its timeout."""

    def __init__(
        self, engine: str, target_name: str, timeout_seconds: int, details: str
    ) -> None:
        """Initialize the error with timeout context and captured output."""
        super().__init__(
            f"{engine.title()} merge timed out for {target_name} after "
            f"{timeout_seconds} seconds.{details}"
        )


class MergeCommandFailedError(RuntimeError):
    """Raised when an external merge engine exits non-zero."""

    def __init__(
        self, engine: str, target_name: str, return_code: int, details: str
    ) -> None:
        """Initialize the error with exit-code context and captured output."""
        super().__init__(
            f"{engine.title()} merge failed for {target_name} with exit code "
            f"{return_code}.{details}"
        )


class MissingMergedOutputError(RuntimeError):
    """Raised when an external merge engine does not write `merged.md`."""

    def __init__(self, engine: str, target_name: str, details: str) -> None:
        """Initialize the error with the missing-output context."""
        super().__init__(
            f"{engine.title()} merge did not produce merged.md for "
            f"{target_name}.{details}"
        )
