# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Agent profile loading for primary ethos payloads.

Responsibility is narrow.
Public imports stay aligned.
"""

from collections.abc import Mapping
from typing import Any, cast

from coding_ethos.loader_common import as_mapping, normalize_lines
from coding_ethos.models import SUPPORTED_AGENTS, AgentProfile
from coding_ethos.presets import AGENT_PROFILES


def agent_profiles_from_payload(payload: dict[str, Any]) -> dict[str, AgentProfile]:
    """Provide focused helper behavior for the split module."""
    raw_profiles: dict[str, Any] = dict(AGENT_PROFILES)
    raw_agents = cast(object, payload.get("agents", {}) or {})
    if isinstance(raw_agents, Mapping):
        raw_profiles.update(cast(Mapping[str, Any], raw_agents))
    profiles: dict[str, AgentProfile] = {}
    for agent in SUPPORTED_AGENTS:
        raw = as_mapping(raw_profiles.get(agent, {}) or {}, f"agents.{agent}")
        profiles[agent] = AgentProfile(
            name=agent,
            root_file=str(raw.get("root_file", "")).strip(),
            supporting_files=[
                str(item).strip()
                for item in raw.get("supporting_files", [])
                if str(item).strip()
            ],
            notes=normalize_lines(raw.get("notes")),
        )
    return profiles
