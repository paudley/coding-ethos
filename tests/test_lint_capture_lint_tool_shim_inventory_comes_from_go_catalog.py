# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Managed lint capture policy tool tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.lint_capture_support import (
    REPO_ROOT,
)


def test_lint_tool_shim_inventory_comes_from_go_catalog() -> None:
    shims = (REPO_ROOT / "go" / "internal" / "lintcli" / "shims.go").read_text(
        encoding="utf-8"
    )

    assert not (REPO_ROOT / "pre-commit" / "hooks" / "tool-capture.sh").exists()
    assert "CapturedLintTools()" in shims
    assert "CAPTURED_LINT_TOOLS" not in shims
    assert "CODING_ETHOS_POLICY_TOOL_SHIM=1" in shims
