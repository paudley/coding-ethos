# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Dependabot policy contract tests.

This module verifies repository automation cannot reintroduce unsigned
version-update pull requests. The contract stays intentionally narrow because
the signed-commit requirement belongs to repository policy, not package manager
behavior.
"""

from pathlib import Path

import yaml


def test_dependabot_version_update_pull_requests_are_disabled() -> None:
    """Require Dependabot ecosystems to avoid unsigned version-update PRs."""
    config_path = Path(".github/dependabot.yml")
    config = yaml.safe_load(config_path.read_text(encoding="utf-8"))

    updates = config["updates"]

    assert updates
    assert all(update["open-pull-requests-limit"] == 0 for update in updates)
