# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Tests for generated CI workflow renderers.

The generated CI files are managed configuration artifacts, so renderer
defaults need focused coverage outside the large CLI integration test module.
These tests check the reusable GitHub SARIF contract and GitLab optional job
knobs directly. They deliberately import through the package public API so the
tests exercise the supported interface.
"""

from coding_ethos import render_github_sarif_workflow, render_gitlab_sarif_config


def test_github_sarif_workflow_defaults_to_reusable_only() -> None:
    workflow = render_github_sarif_workflow({"generated_config": {"ci": {}}})

    assert "workflow_call:" in workflow
    assert "pull_request:" not in workflow
    assert "workflow_dispatch:" not in workflow
    assert "timeout-minutes: 30" in workflow
    assert "permissions:\n  contents: read" in workflow
    assert "      security-events: write" in workflow
    assert "CODING_ETHOS_SARIF_CATEGORY: policy" in workflow
    assert '--sarif-category "$CODING_ETHOS_SARIF_CATEGORY"' in workflow


def test_github_sarif_workflow_can_enable_standalone_triggers() -> None:
    workflow = render_github_sarif_workflow(
        {"generated_config": {"ci": {"github_actions": {"standalone_triggers": True}}}}
    )

    assert "pull_request:" in workflow
    assert "workflow_dispatch:" in workflow


def test_github_sarif_workflow_parses_false_string_triggers() -> None:
    workflow = render_github_sarif_workflow(
        {
            "generated_config": {
                "ci": {"github_actions": {"standalone_triggers": "false"}}
            }
        }
    )

    assert "pull_request:" not in workflow
    assert "workflow_dispatch:" not in workflow


def test_gitlab_config_renders_optional_test_and_build_jobs() -> None:
    gitlab_ci = render_gitlab_sarif_config(
        {
            "generated_config": {
                "ci": {
                    "gitlab": {
                        "test_command": "uv run pytest",
                        "build_command": "uv build",
                        "package_check_command": "uvx twine check dist/*",
                    }
                }
            }
        }
    )

    assert "stage: policy" in gitlab_ci
    assert "coding_ethos_test:" in gitlab_ci
    assert "coding_ethos_build:" in gitlab_ci
    assert "interruptible: true" in gitlab_ci
    assert "uvx twine check dist/*" in gitlab_ci
