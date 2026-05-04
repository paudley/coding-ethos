# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Render generated GitLab CI configuration for coding-ethos gates.

This module owns the GitLab-specific template so the GitHub renderer stays
small enough to remain maintainable. It renders policy, optional test, and
optional build jobs from the merged enforcement config. The template preserves
SARIF and trace artifacts while leaving runner image selection to each repo.
"""

from typing import Any

from coding_ethos.config_access import (
    configured_choice,
    configured_int,
    configured_string,
)

HASH_SPDX_HEADER = (
    "# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. "
    "<paudley@blackcat.ca>\n"
    "# SPDX-License-Identifier: MIT\n\n"
)
SANDBOX_MODES = {"auto", "off", "required"}


def _with_hash_spdx_header(content: str) -> str:
    return f"{HASH_SPDX_HEADER}{content.strip()}\n"


def _render_test_job(
    test_command: str,
    timeout_minutes: int,
    artifact_expire_in: str,
) -> str:
    if not test_command:
        return ""
    return f"""
coding_ethos_test:
  stage: test
  interruptible: true
  timeout: {timeout_minutes}m
  script:
    - {test_command}
  artifacts:
    when: always
    expire_in: {artifact_expire_in}
    paths:
      - .coding-ethos/
"""


def _render_build_job(
    build_command: str,
    package_check_command: str,
    timeout_minutes: int,
    artifact_expire_in: str,
    dist_artifact_path: str,
) -> str:
    if not build_command:
        return ""
    package_check_step = ""
    if package_check_command:
        package_check_step = f"    - {package_check_command}\n"
    return f"""
coding_ethos_build:
  stage: build
  interruptible: true
  timeout: {timeout_minutes}m
  script:
    - {build_command}
{package_check_step}  artifacts:
    when: always
    expire_in: {artifact_expire_in}
    paths:
      - {dist_artifact_path}
"""


def render_gitlab_sarif_config(config: dict[str, Any]) -> str:
    """Render a consumer GitLab CI job for coding-ethos SARIF."""
    coding_ethos_path = configured_string(
        config,
        "generated_config.ci.gitlab.coding_ethos_path",
        ".",
    )
    repo_root = configured_string(
        config,
        "generated_config.ci.gitlab.repo_root",
        ".",
    )
    gate_command = configured_string(
        config,
        "generated_config.ci.gitlab.gate_command",
        "make check",
    )
    test_command = configured_string(
        config,
        "generated_config.ci.gitlab.test_command",
        "",
    )
    build_command = configured_string(
        config,
        "generated_config.ci.gitlab.build_command",
        "",
    )
    package_check_command = configured_string(
        config,
        "generated_config.ci.gitlab.package_check_command",
        "",
    )
    sarif_path = configured_string(
        config,
        "generated_config.ci.gitlab.sarif_path",
        "coding-ethos.sarif",
    )
    sandbox_mode = configured_choice(
        config,
        "generated_config.ci.gitlab.sandbox_mode",
        "required",
        SANDBOX_MODES,
    )
    timeout_minutes = configured_int(
        config,
        "generated_config.ci.gitlab.timeout_minutes",
        30,
    )
    artifact_expire_in = configured_string(
        config,
        "generated_config.ci.gitlab.artifact_expire_in",
        "7 days",
    )
    dist_artifact_path = configured_string(
        config,
        "generated_config.ci.gitlab.dist_artifact_path",
        "dist/",
    )
    test_stage = "  - test\n" if test_command else ""
    build_stage = "  - build\n" if build_command else ""
    test_job = _render_test_job(test_command, timeout_minutes, artifact_expire_in)
    build_job = _render_build_job(
        build_command,
        package_check_command,
        timeout_minutes,
        artifact_expire_in,
        dist_artifact_path,
    )
    return _with_hash_spdx_header(
        f"""
workflow:
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'

stages:
  - policy
{test_stage}{build_stage}

coding_ethos_sarif:
  stage: policy
  interruptible: true
  timeout: {timeout_minutes}m
  variables:
    CODING_ETHOS_PATH: {coding_ethos_path}
    CODING_ETHOS_REPO_ROOT: {repo_root}
    CODING_ETHOS_GATE_COMMAND: {gate_command}
    CODING_ETHOS_SARIF_PATH: {sarif_path}
    CODING_ETHOS_SANDBOX_MODE: {sandbox_mode}
    CODING_ETHOS_FILES: ""
  script:
    - make -C "$CODING_ETHOS_PATH" build
    - |
      if [ -n "$CODING_ETHOS_GATE_COMMAND" ]; then
        ethos_path="$(cd "$CODING_ETHOS_PATH" && pwd)"
        export PATH="$ethos_path/bin:$PATH"
        cd "$CODING_ETHOS_REPO_ROOT"
        bash -c "$CODING_ETHOS_GATE_COMMAND"
        cd "$CI_PROJECT_DIR"
      fi
    - |
      "$CODING_ETHOS_PATH/bin/coding-ethos-run" ci-sarif --provider gitlab
  artifacts:
    when: always
    expire_in: {artifact_expire_in}
    paths:
      - "$CODING_ETHOS_SARIF_PATH"
      - .coding-ethos/lint-runs/
      - .coding-ethos/hook-runs/
{test_job}{build_job}
"""
    )
