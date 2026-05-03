# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Load, validate, and merge structured ethos YAML into runtime models.

This module owns normalization of the primary shared ethos document and the
optional repo overlay so renderers can operate on consistent typed bundles.
It is the schema boundary between raw YAML and the rest of the application.
"""

from collections.abc import Mapping, Sequence
from copy import deepcopy
from pathlib import Path
from typing import Any, NoReturn, cast

import yaml

from coding_ethos.bundle_validator import (
    validate_bundle,
    validate_principle_collection,
)
from coding_ethos.models import (
    SECTION_KINDS,
    SUPPORTED_AGENTS,
    AgentProfile,
    EthosBundle,
    EthosSkill,
    Principle,
    PrincipleAxiom,
    PrincipleSection,
    RepoContext,
)
from coding_ethos.presets import (
    AGENT_PROFILES,
    build_agent_hints,
    build_merge_topics,
    build_quick_ref,
)

ETHOS_SCHEMA_VERSION = 2


def _load_yaml(path: Path) -> dict[str, Any]:
    payload = cast(object, yaml.safe_load(path.read_text(encoding="utf-8")))
    if payload is None:
        return {}
    if not isinstance(payload, dict):
        msg = f"Invalid ethos YAML at {path}: expected a mapping at the document root."
        raise TypeError(msg)
    return cast(dict[str, Any], payload)


def _as_mapping(value: object, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        msg = f"{label} must be a mapping."
        raise TypeError(msg)
    return cast(dict[str, Any], value)


def _empty_mapping() -> dict[str, Any]:
    return {}


def _as_sequence(value: object, label: str) -> Sequence[object]:
    if not isinstance(value, list):
        msg = f"{label} must be a list."
        raise TypeError(msg)
    return cast(Sequence[object], value)


def _error(source: str, message: str) -> NoReturn:
    msg = f"Invalid ethos YAML at {source}: {message}"
    raise ValueError(msg)


def _normalize_lines(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [
            str(item).strip()
            for item in cast(Sequence[object], value)
            if str(item).strip()
        ]
    stripped = str(value).strip()
    return [stripped] if stripped else []


def _normalize_commands(raw: object) -> dict[str, list[str]]:
    if not raw:
        return {}
    if not isinstance(raw, dict):
        msg = "commands must be a mapping."
        raise TypeError(msg)
    normalized: dict[str, list[str]] = {}
    for name, commands in _as_mapping(cast(object, raw), "commands").items():
        normalized[str(name)] = _normalize_lines(commands)
    return normalized


def _normalize_agent_notes(raw: object) -> dict[str, list[str]]:
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
        notes[agent] = _normalize_lines(raw_notes.get(agent))
    return notes


def _normalize_agent_hints(raw: object) -> dict[str, str]:
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


def _normalize_skill_id(raw: object, *, source: str, field_name: str) -> str:
    value = str(raw or "").strip()
    if not value:
        _error(source, f"skill `{field_name}` must be non-empty.")
    if value.startswith("-") or value.endswith("-") or "--" in value:
        _error(source, f"skill `{field_name}` must be a valid skill slug.")
    if any(not (char.islower() or char.isdigit() or char == "-") for char in value):
        _error(
            source,
            (
                f"skill `{field_name}` may only contain lowercase letters, "
                "digits, and hyphens."
            ),
        )
    return value


def _skill_from_item(item: dict[str, Any], *, source: str) -> EthosSkill:
    skill_id = _normalize_skill_id(item.get("id"), source=source, field_name="id")
    title = str(item.get("title", "")).strip()
    if not title:
        _error(source, f"skill `{skill_id}` must define a non-empty `title`.")
    description = str(item.get("description", "")).strip()
    if not description:
        _error(source, f"skill `{skill_id}` must define a non-empty `description`.")
    principle_ids = _normalize_string_list(
        item.get("principle_ids"),
        source=source,
        field_name="principle_ids",
    )
    if not principle_ids:
        _error(source, f"skill `{skill_id}` must reference at least one principle.")
    return EthosSkill(
        id=skill_id,
        title=title,
        description=description,
        principle_ids=principle_ids,
        trigger_terms=_normalize_lines(item.get("trigger_terms")),
        short_hint=str(item.get("short_hint", "")).strip(),
        focus=str(item.get("focus", "")).strip(),
        remediation_steps=_normalize_lines(item.get("remediation_steps")),
    )


def _body_from_item(item: dict[str, Any]) -> str:
    return str(item.get("body", "")).rstrip()


def _normalize_section_kind(raw_kind: object) -> str:
    kind = str(raw_kind or "guidance").strip()
    if kind not in SECTION_KINDS:
        msg = f"section kind must be one of: {', '.join(SECTION_KINDS)}"
        raise ValueError(msg)
    return kind


def _section_from_raw(
    raw_section: object,
    *,
    source: str,
    seen_section_ids: set[str],
) -> PrincipleSection:
    if not isinstance(raw_section, dict):
        _error(source, "each section must be a mapping.")
    section = cast(dict[str, Any], raw_section)
    body = _body_from_item(section)
    section_id = str(section.get("id", "")).strip()
    if not section_id:
        _error(source, "each section must define a non-empty `id`.")
    if section_id in seen_section_ids:
        _error(source, f"duplicate section id `{section_id}`.")
    seen_section_ids.add(section_id)
    title = str(section.get("title", "")).strip()
    if not title:
        _error(source, f"section `{section_id}` must define a non-empty `title`.")
    if not body:
        _error(source, f"section `{section_id}` must define a non-empty `body`.")
    try:
        section_kind = _normalize_section_kind(section.get("kind"))
    except ValueError as exc:
        _error(source, f"section `{section_id}` {exc}")
    return PrincipleSection(
        id=section_id,
        title=title,
        summary=str(section.get("summary", "")).strip() or body.splitlines()[0].strip(),
        body=body,
        kind=section_kind,
    )


def _sections_from_payload(
    item: dict[str, Any], *, source: str
) -> list[PrincipleSection]:
    raw_sections = item.get("sections", [])
    sections: list[PrincipleSection] = []
    if not raw_sections:
        body = _body_from_item(item)
        if body:
            sections.append(
                PrincipleSection(
                    id="overview",
                    title="Overview",
                    summary=str(item.get("summary", "")).strip()
                    or body.splitlines()[0].strip(),
                    body=body,
                    kind="overview",
                )
            )
        return sections

    if not isinstance(raw_sections, list):
        _error(source, "`sections` must be a list.")

    seen_section_ids: set[str] = set()
    sections.extend(
        _section_from_raw(
            raw_section,
            source=source,
            seen_section_ids=seen_section_ids,
        )
        for raw_section in _as_sequence(cast(object, raw_sections), "`sections`")
    )
    return sections


def _axioms_from_payload(item: dict[str, Any], *, source: str) -> list[PrincipleAxiom]:
    raw_axioms = item.get("axioms", [])
    if not raw_axioms:
        return []
    if not isinstance(raw_axioms, list):
        _error(source, "`axioms` must be a list.")

    axioms: list[PrincipleAxiom] = []
    for raw_axiom in _as_sequence(cast(object, raw_axioms), "`axioms`"):
        if not isinstance(raw_axiom, dict):
            _error(source, "each axiom must be a mapping.")
        axiom = cast(dict[str, Any], raw_axiom)
        text = str(axiom.get("axiom", "")).strip()
        if not text:
            _error(source, "each axiom must define a non-empty `axiom`.")
        axioms.append(
            PrincipleAxiom(
                axiom=text,
                action=str(axiom.get("action", "")).strip(),
            )
        )

    return axioms


def _normalize_string_list(raw: object, *, source: str, field_name: str) -> list[str]:
    values = _normalize_lines(raw)
    if raw is not None and not values:
        _error(
            source,
            f"`{field_name}` must contain at least one non-empty string when provided.",
        )
    return values


def _require_principle_id(item: dict[str, Any], *, source: str) -> str:
    principle_id = str(item.get("id", "")).strip()
    if not principle_id:
        _error(source, "each principle must define a non-empty `id`.")
    return principle_id


def _require_principle_title(
    item: dict[str, Any], *, source: str, principle_id: str
) -> str:
    title = str(item.get("title", "")).strip()
    if not title:
        _error(source, f"principle `{principle_id}` must define a non-empty `title`.")
    return title


def _require_principle_order(
    item: dict[str, Any], *, source: str, principle_id: str
) -> int:
    try:
        return int(item["order"])
    except (KeyError, TypeError, ValueError):
        _error(source, f"principle `{principle_id}` must define an integer `order`.")
        message = "unreachable"
        raise AssertionError(message) from None


def _require_principle_sections(
    item: dict[str, Any], *, source: str, principle_id: str
) -> list[PrincipleSection]:
    sections = _sections_from_payload(item, source=source)
    if not sections:
        _error(
            source,
            (
                f"principle `{principle_id}` must include at least one "
                "section or inline `body`."
            ),
        )
    return sections


def _resolve_principle_directive(
    item: dict[str, Any],
    *,
    source: str,
    principle_id: str,
    summary: str,
) -> str:
    directive = str(item.get("directive", summary)).strip()
    if not directive:
        _error(
            source, f"principle `{principle_id}` must define a non-empty `directive`."
        )
    return directive


def _principle_from_item(item: dict[str, Any], *, source: str) -> Principle:
    principle_id = _require_principle_id(item, source=source)
    title = _require_principle_title(item, source=source, principle_id=principle_id)
    order = _require_principle_order(item, source=source, principle_id=principle_id)
    sections = _require_principle_sections(
        item, source=source, principle_id=principle_id
    )

    body = "\n\n".join(section.body for section in sections).rstrip()
    summary = str(item.get("summary", "")).strip() or sections[0].summary
    directive = _resolve_principle_directive(
        item,
        source=source,
        principle_id=principle_id,
        summary=summary,
    )

    tags = [str(tag).strip() for tag in item.get("tags", []) if str(tag).strip()]
    related = [
        str(related).strip()
        for related in item.get("related", [])
        if str(related).strip()
    ]
    quick_ref = _normalize_string_list(
        item.get("quick_ref"), source=source, field_name="quick_ref"
    )
    if not quick_ref:
        quick_ref = build_quick_ref(
            summary=summary,
            directive=directive,
            section_summaries=[section.summary for section in sections],
        )

    merge_topics = _normalize_string_list(
        item.get("merge_topics"), source=source, field_name="merge_topics"
    )
    if not merge_topics:
        merge_topics = build_merge_topics(title=title, tags=tags)

    agent_hints = _normalize_agent_hints(item.get("agent_hints"))
    if not agent_hints:
        agent_hints = build_agent_hints(tags=tags)

    return Principle(
        id=principle_id,
        order=order,
        title=title,
        summary=summary,
        body=body,
        sections=sections,
        axioms=_axioms_from_payload(item, source=source),
        directive=directive,
        quick_ref=quick_ref,
        merge_topics=merge_topics,
        tags=tags,
        related=related,
        agent_hints=agent_hints,
    )


def _validate_primary_payload(payload: dict[str, Any], primary_path: Path) -> None:
    source = str(primary_path)
    version = payload.get("version")
    if version != ETHOS_SCHEMA_VERSION:
        _error(source, "`version` must be set to `2`.")

    principles = payload.get("principles")
    if not isinstance(principles, list) or not principles:
        _error(source, "`principles` must be a non-empty list.")

    normalized_principles: list[Principle] = []
    for index, item in enumerate(cast(Sequence[object], principles), start=1):
        if not isinstance(item, dict):
            _error(source, f"principles[{index}] must be a mapping.")
        normalized_principles.append(
            _principle_from_item(
                cast(dict[str, Any], item),
                source=f"{source} principles[{index}]",
            )
        )
    _validate_principle_collection(normalized_principles, source)
    _validate_skill_collection(payload, normalized_principles, source)


def _validate_principle_collection(principles: list[Principle], source: str) -> None:
    validate_principle_collection(principles, source=source)


def _principles_from_payload(
    payload: dict[str, Any], *, source: str
) -> list[Principle]:
    principles: list[Principle] = []
    raw_principles = payload.get("principles", [])
    for index, item in enumerate(_as_sequence(raw_principles, "`principles`"), start=1):
        if not isinstance(item, dict):
            _error(source, f"principles[{index}] must be a mapping.")
        principles.append(
            _principle_from_item(
                cast(dict[str, Any], item),
                source=f"{source} principles[{index}]",
            )
        )
    return sorted(
        principles, key=lambda principle: (principle.order, principle.title.lower())
    )


def _skills_from_payload(payload: dict[str, Any], *, source: str) -> list[EthosSkill]:
    raw_skills = payload.get("skills")
    if raw_skills is None:
        return []

    skill_items = _as_sequence(raw_skills, "`skills`")
    if not skill_items:
        return []

    skills: list[EthosSkill] = []
    for index, item in enumerate(skill_items, start=1):
        if not isinstance(item, dict):
            _error(source, f"skills[{index}] must be a mapping.")
        skills.append(
            _skill_from_item(
                cast(dict[str, Any], item),
                source=f"{source} skills[{index}]",
            )
        )
    return skills


def _validate_skill_collection(
    payload: dict[str, Any], principles: list[Principle], source: str
) -> None:
    raw_skills = payload.get("skills")
    if raw_skills is None:
        return

    skill_items = _as_sequence(raw_skills, "`skills`")
    if not skill_items:
        return

    principle_ids = {principle.id for principle in principles}
    seen_skill_ids: set[str] = set()
    for index, item in enumerate(skill_items, start=1):
        if not isinstance(item, dict):
            _error(source, f"skills[{index}] must be a mapping.")
        skill = _skill_from_item(
            cast(dict[str, Any], item),
            source=f"{source} skills[{index}]",
        )
        if skill.id in seen_skill_ids:
            _error(source, f"duplicate skill id `{skill.id}`.")
        seen_skill_ids.add(skill.id)
        unknown_principles = sorted(
            principle_id
            for principle_id in skill.principle_ids
            if principle_id not in principle_ids
        )
        if unknown_principles:
            _error(
                source,
                (
                    f"skill `{skill.id}` references unknown principle ids: "
                    f"{', '.join(unknown_principles)}."
                ),
            )


def _agent_profiles_from_payload(payload: dict[str, Any]) -> dict[str, AgentProfile]:
    raw_profiles: dict[str, Any] = dict(AGENT_PROFILES)
    raw_agents = cast(object, payload.get("agents", {}) or {})
    if isinstance(raw_agents, Mapping):
        raw_profiles.update(cast(Mapping[str, Any], raw_agents))
    profiles: dict[str, AgentProfile] = {}
    for agent in SUPPORTED_AGENTS:
        raw = _as_mapping(raw_profiles.get(agent, {}) or {}, f"agents.{agent}")
        profiles[agent] = AgentProfile(
            name=agent,
            root_file=str(raw.get("root_file", "")).strip(),
            supporting_files=[
                str(item).strip()
                for item in raw.get("supporting_files", [])
                if str(item).strip()
            ],
            notes=_normalize_lines(raw.get("notes")),
        )
    return profiles


def load_primary_bundle(primary_path: Path) -> EthosBundle:
    """Load and validate the primary structured ethos definition.

    Args:
        primary_path: Path to the canonical ethos YAML file.

    Returns:
        A validated :class:`EthosBundle` ready for rendering or repo overlays.

    Raises:
        ValueError: The YAML document is malformed or violates the expected
            ethos schema.

    """
    payload = _load_yaml(primary_path)
    _validate_primary_payload(payload, primary_path)
    metadata = _as_mapping(payload.get("metadata", {}) or {}, "metadata")
    return EthosBundle(
        title=str(metadata.get("title", "Coding Ethos")).strip(),
        overview=str(metadata.get("overview", "")).strip(),
        source_markdown=str(metadata.get("source_markdown", "")).strip(),
        principles=_principles_from_payload(payload, source=str(primary_path)),
        agent_profiles=_agent_profiles_from_payload(payload),
        skills=_skills_from_payload(payload, source=str(primary_path)),
    )


def _overlay_error(repo_ethos_path: Path, message: str) -> None:
    _error(str(repo_ethos_path), message)


def _load_repo_context(payload: dict[str, Any], repo_ethos_path: Path) -> RepoContext:
    repo_payload = cast(object, payload.get("repo", {}))
    if repo_payload and not isinstance(repo_payload, dict):
        _overlay_error(repo_ethos_path, "`repo` must be a mapping.")
    repo = cast(dict[str, Any], repo_payload) if repo_payload else _empty_mapping()
    raw_paths = cast(object, repo.get("paths") or {})
    if raw_paths and not isinstance(raw_paths, dict):
        _overlay_error(repo_ethos_path, "`repo.paths` must be a mapping.")
    paths = cast(dict[str, Any], raw_paths) if raw_paths else _empty_mapping()
    return RepoContext(
        name=str(repo.get("name", "")).strip(),
        overview=str(repo.get("overview", "")).strip(),
        commands=_normalize_commands(repo.get("commands")),
        paths={str(key): str(value) for key, value in paths.items()},
        notes=_normalize_lines(repo.get("notes")),
        agent_notes=_normalize_agent_notes(payload.get("agent_notes")),
    )


def _overlay_principle_section(
    payload: dict[str, Any], repo_ethos_path: Path
) -> dict[str, Any]:
    principle_section = payload.get("principles", {})
    if principle_section and not isinstance(principle_section, dict):
        _overlay_error(repo_ethos_path, "`principles` must be a mapping.")
    return cast(dict[str, Any], principle_section)


def _overlay_overrides(
    principle_section: dict[str, Any],
    principles_by_id: dict[str, Principle],
    repo_ethos_path: Path,
) -> dict[str, Any]:
    overrides = cast(object, principle_section.get("overrides", {}) or {})
    if overrides and not isinstance(overrides, dict):
        _overlay_error(repo_ethos_path, "`principles.overrides` must be a mapping.")
    override_map = cast(dict[str, Any], overrides) if overrides else _empty_mapping()
    unknown_override_ids = sorted(
        principle_id
        for principle_id in override_map
        if str(principle_id) not in principles_by_id
    )
    if unknown_override_ids:
        unknown_ids = ", ".join(unknown_override_ids)
        _overlay_error(
            repo_ethos_path,
            f"unknown override ids: {unknown_ids}.",
        )
    return override_map


def _apply_principle_override(
    principle: Principle,
    *,
    override: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    (
        explicit_agent_hints,
        recalc_quick_ref,
        recalc_merge_topics,
        recalc_agent_hints,
    ) = _apply_override_fields(principle, override, repo_ethos_path)
    recalc_quick_ref = _apply_override_sections(
        principle,
        override,
        recalc_quick_ref=recalc_quick_ref,
    )
    principle.body = "\n\n".join(
        section.body for section in principle.sections
    ).rstrip()
    _finalize_override(
        principle,
        override,
        explicit_agent_hints=explicit_agent_hints,
        recalc_quick_ref=recalc_quick_ref,
        recalc_merge_topics=recalc_merge_topics,
        recalc_agent_hints=recalc_agent_hints,
    )


def _apply_override_fields(
    principle: Principle,
    override: dict[str, Any],
    repo_ethos_path: Path,
) -> tuple[dict[str, str], bool, bool, bool]:
    explicit_agent_hints: dict[str, str] = {}
    recalc_quick_ref = False
    recalc_merge_topics = False
    recalc_agent_hints = False
    if "summary" in override:
        principle.summary = str(override["summary"]).strip()
        recalc_quick_ref = True
    if "directive" in override:
        principle.directive = str(override["directive"]).strip()
        recalc_quick_ref = True
    if "tags" in override:
        raw_tags = _as_sequence(override["tags"], "override.tags")
        principle.tags = [str(tag).strip() for tag in raw_tags if str(tag).strip()]
        recalc_merge_topics = True
        recalc_agent_hints = True
    if "related" in override:
        raw_related = _as_sequence(override["related"], "override.related")
        principle.related = [
            str(item).strip() for item in raw_related if str(item).strip()
        ]
    if "quick_ref" in override:
        principle.quick_ref = _normalize_string_list(
            override["quick_ref"],
            source=f"{repo_ethos_path} override `{principle.id}`",
            field_name="quick_ref",
        )
    if "merge_topics" in override:
        principle.merge_topics = _normalize_string_list(
            override["merge_topics"],
            source=f"{repo_ethos_path} override `{principle.id}`",
            field_name="merge_topics",
        )
    if "agent_hints" in override:
        explicit_agent_hints = _normalize_agent_hints(override["agent_hints"])
        recalc_agent_hints = True
    return (
        explicit_agent_hints,
        recalc_quick_ref,
        recalc_merge_topics,
        recalc_agent_hints,
    )


def _apply_override_sections(
    principle: Principle,
    override: dict[str, Any],
    *,
    recalc_quick_ref: bool,
) -> bool:
    prepend = str(override.get("prepend", "")).strip()
    append = str(override.get("append", "")).strip()
    if prepend:
        principle.sections.insert(
            0,
            PrincipleSection(
                id="repo-preface",
                title="Repo Preface",
                summary=prepend.splitlines()[0].strip(),
                body=prepend,
                kind="repo_context",
            ),
        )
        recalc_quick_ref = True
    if append:
        principle.sections.append(
            PrincipleSection(
                id="repo-addendum",
                title="Repo Addendum",
                summary=append.splitlines()[0].strip(),
                body=append,
                kind="repo_context",
            ),
        )
        recalc_quick_ref = True
    return recalc_quick_ref


def _finalize_override(
    principle: Principle,
    override: dict[str, Any],
    *,
    explicit_agent_hints: dict[str, str],
    recalc_quick_ref: bool,
    recalc_merge_topics: bool,
    recalc_agent_hints: bool,
) -> None:
    if recalc_quick_ref and "quick_ref" not in override:
        principle.quick_ref = build_quick_ref(
            summary=principle.summary,
            directive=principle.directive,
            section_summaries=[section.summary for section in principle.sections],
        )
    if recalc_merge_topics and "merge_topics" not in override:
        principle.merge_topics = build_merge_topics(
            title=principle.title, tags=principle.tags
        )
    if recalc_agent_hints and "agent_hints" not in override:
        principle.agent_hints = build_agent_hints(tags=principle.tags)
    elif recalc_agent_hints and explicit_agent_hints:
        derived_hints = build_agent_hints(tags=principle.tags)
        derived_hints.update(explicit_agent_hints)
        principle.agent_hints = derived_hints


def _apply_overrides(
    merged: EthosBundle,
    overrides: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    principles_by_id = {principle.id: principle for principle in merged.principles}
    for principle_id, override in overrides.items():
        principle = principles_by_id.get(str(principle_id))
        if principle is None:
            continue
        if not isinstance(override, dict):
            _overlay_error(
                repo_ethos_path,
                f"override `{principle_id}` must be a mapping.",
            )
        _apply_principle_override(
            principle,
            override=cast(dict[str, Any], override),
            repo_ethos_path=repo_ethos_path,
        )


def _append_additional_principles(
    merged: EthosBundle,
    principle_section: dict[str, Any],
    repo_ethos_path: Path,
) -> None:
    principles_by_id = {principle.id for principle in merged.principles}
    additional_ids: set[str] = set()
    additional = cast(object, principle_section.get("additional", []) or [])
    if additional and not isinstance(additional, list):
        _overlay_error(repo_ethos_path, "`principles.additional` must be a list.")
    for item in cast(Sequence[object], additional):
        if not isinstance(item, dict):
            _overlay_error(
                repo_ethos_path,
                "each additional principle must be a mapping.",
            )
        principle = _principle_from_item(
            cast(dict[str, Any], item),
            source=f"{repo_ethos_path} additional[{len(additional_ids) + 1}]",
        )
        if principle.id in principles_by_id or principle.id in additional_ids:
            _overlay_error(
                repo_ethos_path,
                f"duplicate additional principle id `{principle.id}`.",
            )
        additional_ids.add(principle.id)
        merged.principles.append(principle)


def merge_repo_ethos(bundle: EthosBundle, repo_ethos_path: Path) -> EthosBundle:
    """Apply a repo-specific overlay on top of a shared ethos bundle.

    Args:
        bundle: The already validated shared ethos bundle.
        repo_ethos_path: Optional path to the repo-local overlay YAML.

    Returns:
        A new bundle containing repo context, agent notes, and principle
        overrides from the overlay when one is present.

    Raises:
        ValueError: The overlay payload is structurally invalid or references
            unknown principles.

    """
    if not repo_ethos_path.exists():
        return bundle

    merged = deepcopy(bundle)
    payload = _load_yaml(repo_ethos_path)
    merged.repo = _load_repo_context(payload, repo_ethos_path)
    principles_by_id = {principle.id: principle for principle in merged.principles}
    principle_section = _overlay_principle_section(payload, repo_ethos_path)
    overrides = _overlay_overrides(
        principle_section,
        principles_by_id,
        repo_ethos_path,
    )
    _apply_overrides(merged, overrides, repo_ethos_path)
    _append_additional_principles(merged, principle_section, repo_ethos_path)

    merged.principles.sort(
        key=lambda principle: (principle.order, principle.title.lower())
    )
    validate_bundle(merged, source=str(repo_ethos_path))
    return merged
