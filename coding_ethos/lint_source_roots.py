# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""CLI helper for printing repo-contained lint source roots.

The shell lint-capture wrapper calls this module to keep policy parsing inside
the supported Python package API. It intentionally prints only validated
repo-relative roots so captured linter targets cannot escape the consumer repo.
"""

import sys
from pathlib import Path

from coding_ethos import ConfiguredLintRootError, resolve_lint_source_roots

EXPECTED_ARG_COUNT = 2


def main(argv: list[str]) -> int:
    """Print lint source roots for a consumer repository."""
    if len(argv) != EXPECTED_ARG_COUNT:
        print(
            "usage: python -m coding_ethos.lint_source_roots <repo-root>",
            file=sys.stderr,
        )
        return 2

    repo_root = Path(argv[1]).resolve()
    try:
        roots = resolve_lint_source_roots(repo_root)
    except ConfiguredLintRootError as err:
        print(f"FATAL: {err}", file=sys.stderr)
        return 1

    for root in roots:
        print(root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
