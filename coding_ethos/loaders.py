# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Public loading API for normalized coding-ethos bundles.

Responsibility is narrow.
Public imports stay aligned.
"""

from coding_ethos.loader_overlay import merge_repo_ethos
from coding_ethos.loader_primary import load_primary_bundle

__all__ = ["load_primary_bundle", "merge_repo_ethos"]
