# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Clean Python fixture for managed lint e2e tests.

This module is intentionally boring Python that should pass Ruff when the
managed policy-lint path is healthy. It gives the reference repository a real
clean input without relying on fake linter binaries or mocked tool output.
"""


def greet(name: str) -> str:
    """Return a greeting for the supplied name."""
    return f"hello {name}"
