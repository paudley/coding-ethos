<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# tools

Repository-local helper tools live here when the workflow needs tested Python
logic rather than ad hoc shell snippets. These scripts support maintenance
tasks such as OpenSSF Best Practices evidence generation and should keep their
dynamic input boundaries explicit so pyright can verify them.

Add focused tests under `tests/` for every new helper module. Keep public
operator workflows documented in `docs/SOURCE_DOCS.md` and the relevant
feature document instead of relying on script source as the only reference.
