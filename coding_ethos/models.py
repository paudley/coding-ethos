# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

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
    id: str
    title: str
    summary: str
    body: str
    kind: str = "guidance"


@dataclass(slots=True)
class Principle:
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
    name: str
    root_file: str = ""
    supporting_files: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


@dataclass(slots=True)
class RepoContext:
    name: str = ""
    overview: str = ""
    commands: dict[str, list[str]] = field(default_factory=dict)
    paths: dict[str, str] = field(default_factory=dict)
    notes: list[str] = field(default_factory=list)
    agent_notes: dict[str, list[str]] = field(default_factory=dict)


@dataclass(slots=True)
class EthosBundle:
    title: str
    overview: str
    principles: list[Principle]
    agent_profiles: dict[str, AgentProfile] = field(default_factory=dict)
    repo: RepoContext = field(default_factory=RepoContext)
    source_markdown: str = ""
