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


def test_go_e2e_package_timeout_has_bounded_full_gate_budget() -> None:
    makefile, _lines = _makefile_lines()

    assert "GO_TEST_TIMEOUT ?= 3m" in makefile
    for target in ("go-e2e-test", "go-tools-coverage"):
        assert '-timeout="$(GO_TEST_TIMEOUT)"' in _target_block(makefile, target)


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


def test_purrdf_extractor_uses_repository_target_directory() -> None:
    makefile, _lines = _makefile_lines()

    for target in ("purrdf-extractor-check", "purrdf-extractor-install"):
        target_body = _target_block(makefile, target)
        assert (
            "--config 'build.build-dir=\"$(PURRDF_EXTRACTOR_DIR)/target\"'"
            in target_body
        )
        assert '--target-dir "$(PURRDF_EXTRACTOR_DIR)/target"' in target_body


def test_agent_hook_sync_uses_explicit_writable_state_roots() -> None:
    makefile, _lines = _makefile_lines()

    assert "AGENT_HOOK_STATE_ROOT ?= $(LOCAL_BUILD_DIR)/agent-hooks-state" in makefile
    assert (
        "CONSUMER_AGENT_HOOK_STATE_ROOT ?= "
        "$(LOCAL_BUILD_DIR)/consumer-agent-hooks-state" in makefile
    )

    local_sync = _target_block(makefile, "_sync-agent-hooks")
    assert 'CODEX_HOME="$(AGENT_HOOK_STATE_ROOT)/codex-home"' in local_sync
    assert '--repo-root "$(REPO)"' in local_sync
    assert '--state-root "$(AGENT_HOOK_STATE_ROOT)"' in local_sync

    consumer_sync = _target_block(makefile, "_sync-consumer-agent-hooks")
    assert 'CODEX_HOME="$(CONSUMER_AGENT_HOOK_STATE_ROOT)/codex-home"' in consumer_sync
    assert '--repo-root "$(HOOK_CONSUMER_ROOT)"' in consumer_sync
    assert '--state-root "$(CONSUMER_AGENT_HOOK_STATE_ROOT)"' in consumer_sync


def test_parent_hook_runtime_executables_use_atomic_compiled_sync() -> None:
    makefile, _lines = _makefile_lines()

    runtime_sync_target = next(
        line
        for line in makefile.splitlines()
        if line.startswith("_sync-parent-hook-runtime:")
    )
    runtime_sync = _target_block(makefile, "_sync-parent-hook-runtime")
    git_hook_sync = _target_block(makefile, "_sync-git-hooks")

    assert "go-hook-runner-install" in runtime_sync_target
    assert (
        '"$(GO_HOOK)" parent-runtime-sync --repo "$(HOOK_CONSUMER_ROOT)"'
        in runtime_sync
    )
    assert (
        "$(call install_git_hooks,$(LOCAL_HOOKS_DIR),"
        "$(PARENT_HOOK_BIN_DIR)/coding-ethos-run)" in git_hook_sync
    )

    for forbidden in (
        'cp "$(GO_TOOLS_BIN_DIR)"/coding-ethos-*',
        'cp "$(GO_TOOLS_BIN_DIR)/cerun"',
        'cp "$(GO_TOOLS_BIN_DIR)/lint"',
        'cp "$(GO_TOOLS_BIN_DIR)/coding-ethos-git-hook"',
        "$(call install_git_hooks,$(LOCAL_HOOKS_DIR),$(GO_HOOK))",
    ):
        assert forbidden not in makefile
