# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Python fixture overwritten by e2e tests to create a real Ruff finding.

This checked-in file stays lint-clean. The e2e suite writes a temporary unused
import into its sandbox copy so the repository does not ship known lint
violations while still exercising real Ruff diagnostics.
"""


def answer() -> int:
    """Return a stable value."""
    return 42
