# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile build contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _assert_build_target_order,
    _build_target_line,
    _makefile_lines,
)


def test_build_syncs_consumer_generated_outputs_before_runtime_install() -> None:
    makefile, lines = _makefile_lines()

    assert "sync-consumer-tool-configs:" in makefile
    assert "sync-agent-skills: ensure-uv" not in lines
    assert "sync-consumer-agent-skills: ensure-uv" not in lines
    build_line = _build_target_line(makefile)

    assert "sync-tool-configs" in build_line
    assert "sync-consumer-tool-configs" in build_line
    assert "_sync-agent-skills" in build_line
    assert "_sync-consumer-agent-skills" in build_line
    assert "_sync-git-hooks" in build_line
    assert "_sync-parent-hook-runtime" in build_line
    assert "check-local-artifacts" not in build_line
    assert "go-test-binaries-install" not in build_line
    assert "go-e2e-test-binary-install" not in build_line
    assert "GO_TEST_BIN" not in makefile
    assert "GO_E2E_TEST_BINARY" not in makefile
    _assert_build_target_order(build_line)
