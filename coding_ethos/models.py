# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Typed models shared across ethos loading, rendering, and merge flows.

These dataclasses define the stable in-memory contract for the structured
ethos bundle so generators and hook tooling can share one vocabulary.
They keep serialization concerns out of the renderer and CLI layers.
"""

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


def _empty_str_list() -> list[str]:
    return []


def _empty_str_map() -> dict[str, str]:
    return {}


def _empty_command_map() -> dict[str, list[str]]:
    return {}


def _empty_agent_notes() -> dict[str, list[str]]:
    return {}


def _empty_sections() -> list["PrincipleSection"]:
    return []


def _empty_axioms() -> list["PrincipleAxiom"]:
    return []


def _empty_agent_profiles() -> dict[str, "AgentProfile"]:
    return {}


def _empty_skills() -> list["EthosSkill"]:
    return []


@dataclass(slots=True)
class PrincipleSection:
    """One rendered section within a principle detail document."""

    id: str
    title: str
    summary: str
    body: str
    kind: str = "guidance"


@dataclass(slots=True)
class PrincipleAxiom:
    """One concise reminder axiom owned by an ETHOS principle."""

    axiom: str
    action: str = ""


@dataclass(slots=True)
class Principle:
    """One normalized ethos principle with summary, detail, and hints."""

    id: str
    order: int
    title: str
    summary: str
    body: str
    sections: list[PrincipleSection] = field(default_factory=_empty_sections)
    axioms: list[PrincipleAxiom] = field(default_factory=_empty_axioms)
    directive: str = ""
    quick_ref: list[str] = field(default_factory=_empty_str_list)
    merge_topics: list[str] = field(default_factory=_empty_str_list)
    tags: list[str] = field(default_factory=_empty_str_list)
    related: list[str] = field(default_factory=_empty_str_list)
    agent_hints: dict[str, str] = field(default_factory=_empty_str_map)


@dataclass(slots=True)
class AgentProfile:
    """Agent-specific root-file and note configuration."""

    name: str
    root_file: str = ""
    supporting_files: list[str] = field(default_factory=_empty_str_list)
    notes: list[str] = field(default_factory=_empty_str_list)


@dataclass(slots=True)
class RepoContext:
    """Repo-local commands, paths, and additive notes for generated outputs."""

    name: str = ""
    overview: str = ""
    commands: dict[str, list[str]] = field(default_factory=_empty_command_map)
    paths: dict[str, str] = field(default_factory=_empty_str_map)
    notes: list[str] = field(default_factory=_empty_str_list)
    agent_notes: dict[str, list[str]] = field(default_factory=_empty_agent_notes)


@dataclass(slots=True)
class EthosSkill:
    """One generated agent skill sourced from ETHOS principles."""

    id: str
    title: str
    description: str
    principle_ids: list[str]
    trigger_terms: list[str] = field(default_factory=_empty_str_list)
    short_hint: str = ""
    focus: str = ""
    remediation_steps: list[str] = field(default_factory=_empty_str_list)


@dataclass(slots=True)
class EthosBundle:
    """Complete normalized ethos payload used by all renderers and generators."""

    title: str
    overview: str
    principles: list[Principle]
    agent_profiles: dict[str, AgentProfile] = field(
        default_factory=_empty_agent_profiles
    )
    skills: list[EthosSkill] = field(default_factory=_empty_skills)
    repo: RepoContext = field(default_factory=RepoContext)
    source_markdown: str = ""
