# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Makefile diagnostics contract tests.

Responsibility is narrow.
Public imports stay aligned."""

from tests.makefile_contract_support import (
    _makefile_lines,
    _target_block,
)


def test_tests_and_diagnostics_do_not_build_or_install_runtime() -> None:
    makefile, _lines = _makefile_lines()

    forbidden_prerequisites = (
        "build",
        "go-tools-install",
        "managed-toolchain-install",
        "go-hook-runner-install",
        "policy-bundle-install",
        "_sync-git-hooks",
        "_sync-parent-hook-runtime",
        "sync-consumer-tool-configs",
    )
    for target in (
        "check",
        "check-tool-configs",
        "check-gemini-prompts",
        "check-agent-skills",
        "check-provider-matrix",
        "parent-check",
        "cutover-verify",
        "pre-commit",
        "pre-commit-all",
        "pre-push",
        "commit-msg",
        "hook-plan",
        "validate",
        "go-test",
        "lint",
        "fix",
        "format",
        "go-e2e-test",
        "go-tools-coverage",
    ):
        target_line = next(
            line for line in makefile.splitlines() if line.startswith(f"{target}:")
        )
        for prerequisite in forbidden_prerequisites:
            assert prerequisite not in target_line
        target_body = _target_block(makefile, target)
        for command in (
            '"$(GO)" run',
            '"$(GO)" build',
            "$(GO) run",
            "$(GO) build",
            "install-git-hooks",
            "install-managed-toolchain",
            "sync-tool-configs",
            "sync-agent-skills",
            "cp ",
        ):
            assert command not in target_body

    assert "go-tools-smoke" not in next(
        line for line in makefile.splitlines() if line.startswith("check:")
    )


def test_normal_runtime_commands_require_prebuilt_artifacts() -> None:
    makefile, _lines = _makefile_lines()

    for target in (
        "parent-install",
        "check-provider-matrix",
        "parent-check",
        "parent-lint",
        "cutover-verify",
        "pre-commit",
        "pre-commit-all",
        "pre-push",
        "commit-msg",
        "hook-plan",
        "validate",
    ):
        target_line = next(
            line for line in makefile.splitlines() if line.startswith(f"{target}:")
        )
        assert "ensure-hook-runtime" in target_line
        assert "build" not in target_line
        assert "go-tools-install" not in target_line

    runtime_guard = _target_block(makefile, "ensure-hook-runtime")
    for tool in ("coding-ethos-agent-hooks", "coding-ethos-policy"):
        assert tool in runtime_guard
    assert "Managed tool is missing" in runtime_guard
    assert "make build" in runtime_guard


def test_check_blocks_unmanaged_go_module_root_binaries() -> None:
    makefile, _lines = _makefile_lines()

    check_lines = [line for line in makefile.splitlines() if line.startswith("check:")]
    check_line = check_lines[0]
    assert "check-local-artifacts" in check_line
    assert check_line.index("check-local-artifacts") < check_line.index("test")

    local_artifact_block = _target_block(makefile, "check-local-artifacts")
    assert "GO_MODULE_ROOT_BINARY_OUTPUTS" in makefile
    assert "$(GO_TOOL_CMDS)" in makefile
    assert "coding-ethos-hook-runner" in makefile
    assert "$(GO_TOOLS_DIR)/$$name" in local_artifact_block
    assert "Unmanaged Go build artifact" in local_artifact_block
    assert "make go-tools-clean" in local_artifact_block

    clean_block = _target_block(makefile, "go-tools-clean")
    assert "$(GO_MODULE_ROOT_BINARY_OUTPUTS)" in clean_block
    assert 'rm -f "$(GO_TOOLS_DIR)/$$name"' in clean_block
