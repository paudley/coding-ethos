# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture policy tool tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.lint_capture_support import (
    REPO_ROOT,
)


def test_lint_target_source_roots_come_from_policy_config() -> None:
    runtime_config = (REPO_ROOT / "go" / "lintcapture" / "config.go").read_text(
        encoding="utf-8"
    )
    resolver = (REPO_ROOT / "go" / "lintcapture" / "targets.go").read_text(
        encoding="utf-8"
    )

    assert "LoadRuntimeConfig" in runtime_config
    assert '"python", "source_paths"' in runtime_config
    assert '"python", "extra_paths"' in runtime_config
    assert "containedSourceRoots" in runtime_config
    assert "errLintSourceRootEscapesRepo" in resolver
    assert "pyrightconfig" not in runtime_config
