# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Contract checks for Makefile hook-runtime ownership behavior.

The Makefile is the operator surface for installing hooks and repairing local
runtime artifacts. These tests pin important ordering guarantees that protect
active consumer repos from generated-config drift during hook rebuilds.
"""

from pathlib import Path


def _makefile_lines() -> tuple[str, list[str]]:
    makefile = Path("Makefile").read_text(encoding="utf-8")
    return makefile, makefile.splitlines()


def _build_target_line(makefile: str) -> str:
    return next(line for line in makefile.splitlines() if line.startswith("build:"))


def _target_block(makefile: str, target: str) -> str:
    lines = makefile.splitlines()
    start = next(
        index for index, line in enumerate(lines) if line.startswith(f"{target}:")
    )
    end = len(lines)
    for index in range(start + 1, len(lines)):
        line = lines[index]
        if line and not line.startswith(("\t", " ", "#")) and ":" in line:
            end = index
            break
    return "\n".join(lines[start:end])


def _assert_build_target_order(build_line: str) -> None:
    prerequisites = (
        "sync-consumer-tool-configs",
        "_sync-agent-skills",
        "_sync-consumer-agent-skills",
        "_sync-git-hooks",
    )
    for prerequisite in prerequisites:
        assert build_line.index(prerequisite) < build_line.index(
            "managed-toolchain-install"
        )
    assert build_line.index("policy-bundle-install") < build_line.index(
        "_sync-parent-hook-runtime"
    )


def _assert_internal_target(makefile: str, lines: list[str], target: str) -> None:
    assert f"{target}:" in makefile
    assert f"{target}: ensure-uv ##" not in lines
    assert f"{target}: ensure-go go-tools-install ##" not in lines


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


def test_tool_config_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    for target in (
        "sync-tool-configs",
        "sync-consumer-tool-configs",
        "fix-configs",
    ):
        assert f"{target}: ensure-go" in makefile
        assert f"{target}: ensure-uv" not in lines
    assert "check-tool-configs: ensure-hook-runtime" in makefile

    tool_config_block = makefile.split("sync-gemini-prompts:", maxsplit=1)[0].split(
        "sync-tool-configs:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in tool_config_block
    assert "--sync-tool-configs" not in tool_config_block
    assert "--check-tool-configs" not in tool_config_block
    check_tool_config_block = makefile.split("sync-gemini-prompts:", maxsplit=1)[
        0
    ].split("check-tool-configs:", maxsplit=1)[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_tool_config_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_tool_config_block


def test_gemini_prompt_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    assert "sync-gemini-prompts: ensure-go" in makefile
    assert "check-gemini-prompts: ensure-hook-runtime" in makefile
    assert "sync-gemini-prompts: ensure-uv" not in lines
    assert "check-gemini-prompts: ensure-uv" not in lines

    gemini_block = makefile.split("_sync-agent-skills:", maxsplit=1)[0].split(
        "sync-gemini-prompts:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in gemini_block
    assert "--sync-gemini-prompts" not in gemini_block
    assert "--check-gemini-prompts" not in gemini_block
    check_gemini_block = makefile.split("_sync-agent-skills:", maxsplit=1)[0].split(
        "check-gemini-prompts:",
        maxsplit=1,
    )[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_gemini_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_gemini_block


def test_agent_skill_sync_is_not_user_facing() -> None:
    makefile, lines = _makefile_lines()

    phony_block = makefile.split("##@ Help", maxsplit=1)[0]

    assert "\tsync-agent-skills \\" not in phony_block
    assert "\t_sync-agent-skills \\" not in phony_block
    assert "\tsync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-consumer-agent-skills \\" not in phony_block
    assert "\t_sync-git-hooks \\" not in phony_block
    assert "\t_sync-parent-hook-runtime \\" not in phony_block
    assert "_sync-agent-skills: ensure-go\n" in makefile
    assert "_sync-consumer-agent-skills: ensure-go\n" in makefile
    assert "_sync-git-hooks: ensure-go go-tools-install\n" in makefile
    assert (
        "_sync-parent-hook-runtime: ensure-go go-tools-install policy-bundle-install\n"
        in makefile
    )
    for target in (
        "_sync-agent-skills",
        "_sync-consumer-agent-skills",
        "_sync-git-hooks",
        "_sync-parent-hook-runtime",
    ):
        _assert_internal_target(makefile, lines, target)


def test_agent_skill_sync_uses_go_policy_command() -> None:
    makefile, lines = _makefile_lines()

    assert "_sync-agent-skills: ensure-go" in makefile
    assert "check-agent-skills: ensure-hook-runtime" in makefile
    assert "_sync-agent-skills: ensure-uv" not in lines
    assert "check-agent-skills: ensure-uv" not in lines

    skill_block = makefile.split("_sync-agent-hooks:", maxsplit=1)[0].split(
        "_sync-agent-skills:",
        maxsplit=1,
    )[1]
    assert '"$(GO)" run ./cmd/coding-ethos-policy' in skill_block
    assert "--sync-agent-skills" not in skill_block
    assert "--check-agent-skills" not in skill_block
    check_skill_block = makefile.split("build:", maxsplit=1)[0].split(
        "check-agent-skills:",
        maxsplit=1,
    )[1]
    assert '"$(GO_TOOLS_BIN_DIR)/coding-ethos-policy"' in check_skill_block
    assert '"$(GO)" run ./cmd/coding-ethos-policy' not in check_skill_block


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
