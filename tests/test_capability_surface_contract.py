# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Verify capability surface governance remains documented.

The tests guard the decision guide and templates that route new public
capabilities to their owning surfaces. They intentionally check durable text
markers so contributors update the public governance contract when templates
or docs move.
"""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def _read_repo_text(*parts: str) -> str:
    return ROOT.joinpath(*parts).read_text(encoding="utf-8")


def test_capability_surface_guide_defines_required_routes() -> None:
    guide = " ".join(_read_repo_text("docs", "CAPABILITY_SURFACE_DECISION.md").split())

    for route in (
        "CEL/policy/hook",
        "generated skills",
        "MCP",
        "CLI",
        "SARIF/code-intel/outputsurface",
        "provider registry",
    ):
        assert route in guide


def test_templates_require_capability_surface_grounding() -> None:
    pr_template = _read_repo_text(".github", "pull_request_template.md")
    assert "## Capability Surface" in pr_template
    assert "chosen surface" in pr_template
    assert "why this surface owns the behavior" in pr_template

    issue_templates = {
        "feature_request.yml": "surface_choice",
        "mcp_tool_request.yml": "surface_grounding",
        "policy_rule.yml": "surface_grounding",
    }

    for filename, field_id in issue_templates.items():
        template = _read_repo_text(".github", "ISSUE_TEMPLATE", filename)
        assert field_id in template
        assert "Capability surface" in template
