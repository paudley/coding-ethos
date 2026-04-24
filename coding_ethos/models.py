"""Typed models shared across ethos loading, rendering, and merge flows.

These dataclasses define the stable in-memory contract for the structured
ethos bundle so generators and hook tooling can share one vocabulary.
They keep serialization concerns out of the renderer and CLI layers.
"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from dataclasses import dataclass, field

SUPPORTED_AGENTS = ("codex", "claude", "gemini")
SECTION_KINDS = (
    "overview",
    "guidance",
    "rule",
    "policy",
    "workflow",
    "anti_patterns",
    "correct_way",
    "rationale",
    "examples",
    "reference",
    "repo_context",
)


@dataclass(slots=True)
class PrincipleSection:
    """One rendered section within a principle detail document."""

    id: str
    title: str
    summary: str
    body: str
    kind: str = "guidance"


@dataclass(slots=True)
class Principle:
    """One normalized ethos principle with summary, detail, and hints."""

    id: str
    order: int
    title: str
    summary: str
    body: str
    sections: list[PrincipleSection] = field(default_factory=list)
    directive: str = ""
    quick_ref: list[str] = field(default_factory=list)
    merge_topics: list[str] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    related: list[str] = field(default_factory=list)
    agent_hints: dict[str, str] = field(default_factory=dict)


@dataclass(slots=True)
class AgentProfile:
    """Agent-specific root-file and note configuration."""

    name: str
    root_file: str = ""
    supporting_files: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


@dataclass(slots=True)
class RepoContext:
    """Repo-local commands, paths, and additive notes for generated outputs."""

    name: str = ""
    overview: str = ""
    commands: dict[str, list[str]] = field(default_factory=dict)
    paths: dict[str, str] = field(default_factory=dict)
    notes: list[str] = field(default_factory=list)
    agent_notes: dict[str, list[str]] = field(default_factory=dict)


@dataclass(slots=True)
class EthosBundle:
    """Complete normalized ethos payload used by all renderers and generators."""

    title: str
    overview: str
    principles: list[Principle]
    agent_profiles: dict[str, AgentProfile] = field(default_factory=dict)
    repo: RepoContext = field(default_factory=RepoContext)
    source_markdown: str = ""
