"""Console launcher for the coding-ethos package entrypoint.

This thin wrapper exists so the repo can run the CLI directly with `python
main.py` during development and Makefile automation.
It should remain a tiny shim over the public package API.

See Also:
    coding_ethos/CODING_ETHOS.md: Package overview and generation workflow.

"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from coding_ethos import main

if __name__ == "__main__":
    raise SystemExit(main())
