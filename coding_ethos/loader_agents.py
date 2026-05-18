# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Agent-specific note and hint normalization helpers.

Responsibility is narrow.
Public imports stay aligned.
"""

from typing import Any, cast

from coding_ethos.loader_common import normalize_lines
from coding_ethos.models import SUPPORTED_AGENTS


def normalize_agent_notes(raw: object) -> dict[str, list[str]]:
    """Provide focused helper behavior for the split module."""
    notes: dict[str, list[str]] = {}
    if not raw:
        return notes
    if not isinstance(raw, dict):
        msg = "agent_notes must be a mapping."
        raise TypeError(msg)
    raw_notes = cast(dict[str, Any], raw)
    unknown_agents = sorted(
        agent for agent in raw_notes if agent not in SUPPORTED_AGENTS
    )
    if unknown_agents:
        msg = f"agent_notes contains unsupported agents: {', '.join(unknown_agents)}"
        raise ValueError(msg)
    for agent in SUPPORTED_AGENTS:
        notes[agent] = normalize_lines(raw_notes.get(agent))
    return notes


def normalize_agent_hints(raw: object) -> dict[str, str]:
    """Provide focused helper behavior for the split module."""
    if not raw:
        return {}
    if not isinstance(raw, dict):
        msg = "agent_hints must be a mapping."
        raise TypeError(msg)
    raw_hints = cast(dict[str, Any], raw)
    unknown_agents = sorted(
        agent for agent in raw_hints if agent not in SUPPORTED_AGENTS
    )
    if unknown_agents:
        msg = f"agent_hints contains unsupported agents: {', '.join(unknown_agents)}"
        raise ValueError(msg)
    return {
        agent: str(value).strip()
        for agent, value in raw_hints.items()
        if agent in SUPPORTED_AGENTS and str(value).strip()
    }
