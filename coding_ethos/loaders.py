# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

from copy import deepcopy
from pathlib import Path
from typing import Any

import yaml

from coding_ethos.models import (
    SECTION_KINDS,
    SUPPORTED_AGENTS,
    AgentProfile,
    EthosBundle,
    Principle,
    PrincipleSection,
    RepoContext,
)
from coding_ethos.presets import (
    AGENT_PROFILES,
    build_agent_hints,
    build_merge_topics,
    build_quick_ref,
)


def _load_yaml(path: Path) -> dict[str, Any]:
    payload = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(payload, dict):
        raise ValueError(
            f"Invalid ethos YAML at {path}: expected a mapping at the document root."
        )
    return payload


def _error(source: str, message: str) -> None:
    raise ValueError(f"Invalid ethos YAML at {source}: {message}")


def _normalize_lines(value: str | list[str] | None) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [item.strip() for item in value if item and item.strip()]
    stripped = value.strip()
    return [stripped] if stripped else []


def _normalize_commands(raw: dict[str, Any] | None) -> dict[str, list[str]]:
    if not raw:
        return {}
    normalized: dict[str, list[str]] = {}
    for name, commands in raw.items():
        normalized[str(name)] = _normalize_lines(commands)
    return normalized


def _normalize_agent_notes(raw: dict[str, Any] | None) -> dict[str, list[str]]:
    notes: dict[str, list[str]] = {}
    if not raw:
        return notes
    unknown_agents = sorted(agent for agent in raw if agent not in SUPPORTED_AGENTS)
    if unknown_agents:
        raise ValueError(
            f"agent_notes contains unsupported agents: {', '.join(unknown_agents)}"
        )
    for agent in SUPPORTED_AGENTS:
        notes[agent] = _normalize_lines(raw.get(agent))
    return notes


def _normalize_agent_hints(raw: dict[str, Any] | None) -> dict[str, str]:
    if not raw:
        return {}
    if not isinstance(raw, dict):
        raise ValueError("agent_hints must be a mapping.")
    unknown_agents = sorted(agent for agent in raw if agent not in SUPPORTED_AGENTS)
    if unknown_agents:
        raise ValueError(
            f"agent_hints contains unsupported agents: {', '.join(unknown_agents)}"
        )
    return {
        agent: str(value).strip()
        for agent, value in raw.items()
        if agent in SUPPORTED_AGENTS and str(value).strip()
    }


def _body_from_item(item: dict[str, Any]) -> str:
    return str(item.get("body", "")).rstrip()


def _normalize_section_kind(raw_kind: Any) -> str:
    kind = str(raw_kind or "guidance").strip()
    if kind not in SECTION_KINDS:
        raise ValueError(f"section kind must be one of: {', '.join(SECTION_KINDS)}")
    return kind


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
    for raw_section in raw_sections:
        if not isinstance(raw_section, dict):
            _error(source, "each section must be a mapping.")
        body = _body_from_item(raw_section)
        section_id = str(raw_section.get("id", "")).strip()
        if not section_id:
            _error(source, "each section must define a non-empty `id`.")
        if section_id in seen_section_ids:
            _error(source, f"duplicate section id `{section_id}`.")
        seen_section_ids.add(section_id)
        title = str(raw_section.get("title", "")).strip()
        if not title:
            _error(source, f"section `{section_id}` must define a non-empty `title`.")
        if not body:
            _error(source, f"section `{section_id}` must define a non-empty `body`.")
        try:
            section_kind = _normalize_section_kind(raw_section.get("kind"))
        except ValueError as exc:
            _error(source, f"section `{section_id}` {exc}")
        sections.append(
            PrincipleSection(
                id=section_id,
                title=title,
                summary=str(raw_section.get("summary", "")).strip()
                or body.splitlines()[0].strip(),
                body=body,
                kind=section_kind,
            )
        )
    return sections


def _normalize_string_list(raw: Any, *, source: str, field_name: str) -> list[str]:
    values = _normalize_lines(raw)
    if raw is not None and not values:
        _error(
            source,
            f"`{field_name}` must contain at least one non-empty string when provided.",
        )
    return values


def _principle_from_item(item: dict[str, Any], *, source: str) -> Principle:
    principle_id = str(item.get("id", "")).strip()
    if not principle_id:
        _error(source, "each principle must define a non-empty `id`.")

    title = str(item.get("title", "")).strip()
    if not title:
        _error(source, f"principle `{principle_id}` must define a non-empty `title`.")

    try:
        order = int(item["order"])
    except (KeyError, TypeError, ValueError) as exc:
        _error(source, f"principle `{principle_id}` must define an integer `order`.")
        raise AssertionError("unreachable") from exc

    sections = _sections_from_payload(item, source=source)
    if not sections:
        _error(
            source,
            f"principle `{principle_id}` must include at least one section or inline `body`.",
        )

    body = "\n\n".join(section.body for section in sections).rstrip()
    summary = str(item.get("summary", "")).strip() or sections[0].summary
    directive = str(item.get("directive", summary)).strip()
    if not directive:
        _error(
            source, f"principle `{principle_id}` must define a non-empty `directive`."
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
    if version != 2:
        _error(source, "`version` must be set to `2`.")

    principles = payload.get("principles")
    if not isinstance(principles, list) or not principles:
        _error(source, "`principles` must be a non-empty list.")

    normalized_principles: list[Principle] = []
    for index, item in enumerate(principles, start=1):
        if not isinstance(item, dict):
            _error(source, f"principles[{index}] must be a mapping.")
        normalized_principles.append(
            _principle_from_item(item, source=f"{source} principles[{index}]")
        )
    _validate_principle_collection(normalized_principles, source)


def _validate_principle_collection(principles: list[Principle], source: str) -> None:
    seen_ids: set[str] = set()
    seen_orders: set[int] = set()
    related_map: dict[str, list[str]] = {}
    for principle in principles:
        if principle.id in seen_ids:
            _error(source, f"duplicate principle id `{principle.id}`.")
        if principle.order in seen_orders:
            _error(source, f"duplicate principle order `{principle.order}`.")
        seen_ids.add(principle.id)
        seen_orders.add(principle.order)
        related_map[principle.id] = principle.related

    all_ids = set(related_map)
    for principle_id, related in related_map.items():
        unknown_related = sorted(item for item in related if item not in all_ids)
        if unknown_related:
            _error(
                source,
                f"principle `{principle_id}` references unknown related ids: {', '.join(unknown_related)}.",
            )


def _principles_from_payload(
    payload: dict[str, Any], *, source: str
) -> list[Principle]:
    principles: list[Principle] = []
    for index, item in enumerate(payload.get("principles", []), start=1):
        principles.append(
            _principle_from_item(item, source=f"{source} principles[{index}]")
        )
    return sorted(
        principles, key=lambda principle: (principle.order, principle.title.lower())
    )


def _agent_profiles_from_payload(payload: dict[str, Any]) -> dict[str, AgentProfile]:
    raw_profiles = dict(AGENT_PROFILES)
    raw_profiles.update(payload.get("agents", {}) or {})
    profiles: dict[str, AgentProfile] = {}
    for agent in SUPPORTED_AGENTS:
        raw = raw_profiles.get(agent, {})
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
    payload = _load_yaml(primary_path)
    _validate_primary_payload(payload, primary_path)
    metadata = payload.get("metadata", {})
    return EthosBundle(
        title=str(metadata.get("title", "Coding Ethos")).strip(),
        overview=str(metadata.get("overview", "")).strip(),
        source_markdown=str(metadata.get("source_markdown", "")).strip(),
        principles=_principles_from_payload(payload, source=str(primary_path)),
        agent_profiles=_agent_profiles_from_payload(payload),
    )


def merge_repo_ethos(bundle: EthosBundle, repo_ethos_path: Path | None) -> EthosBundle:
    if repo_ethos_path is None or not repo_ethos_path.exists():
        return bundle

    merged = deepcopy(bundle)
    payload = _load_yaml(repo_ethos_path)
    repo_payload = payload.get("repo", {})
    if repo_payload and not isinstance(repo_payload, dict):
        raise ValueError(
            f"Invalid ethos YAML at {repo_ethos_path}: `repo` must be a mapping."
        )
    merged.repo = RepoContext(
        name=str(repo_payload.get("name", "")).strip(),
        overview=str(repo_payload.get("overview", "")).strip(),
        commands=_normalize_commands(repo_payload.get("commands")),
        paths={
            str(key): str(value)
            for key, value in (repo_payload.get("paths") or {}).items()
        },
        notes=_normalize_lines(repo_payload.get("notes")),
        agent_notes=_normalize_agent_notes(payload.get("agent_notes")),
    )

    principles_by_id = {principle.id: principle for principle in merged.principles}
    principle_section = payload.get("principles", {})
    if principle_section and not isinstance(principle_section, dict):
        raise ValueError(
            f"Invalid ethos YAML at {repo_ethos_path}: `principles` must be a mapping."
        )
    overrides = principle_section.get("overrides", {}) or {}
    if overrides and not isinstance(overrides, dict):
        raise ValueError(
            f"Invalid ethos YAML at {repo_ethos_path}: `principles.overrides` must be a mapping."
        )
    unknown_override_ids = sorted(
        principle_id
        for principle_id in overrides
        if str(principle_id) not in principles_by_id
    )
    if unknown_override_ids:
        raise ValueError(
            f"Invalid ethos YAML at {repo_ethos_path}: unknown override ids: {', '.join(unknown_override_ids)}."
        )
    for principle_id, override in overrides.items():
        principle = principles_by_id.get(str(principle_id))
        if principle is None:
            continue
        if not isinstance(override, dict):
            raise ValueError(
                f"Invalid ethos YAML at {repo_ethos_path}: override `{principle_id}` must be a mapping."
            )
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
            principle.tags = [
                str(tag).strip() for tag in override["tags"] if str(tag).strip()
            ]
            recalc_merge_topics = True
            recalc_agent_hints = True
        if "related" in override:
            principle.related = [
                str(item).strip() for item in override["related"] if str(item).strip()
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
        principle.body = "\n\n".join(
            section.body for section in principle.sections
        ).rstrip()
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

    additional_ids: set[str] = set()
    additional = principle_section.get("additional", []) or []
    if additional and not isinstance(additional, list):
        raise ValueError(
            f"Invalid ethos YAML at {repo_ethos_path}: `principles.additional` must be a list."
        )
    for item in additional:
        if not isinstance(item, dict):
            raise ValueError(
                f"Invalid ethos YAML at {repo_ethos_path}: each additional principle must be a mapping."
            )
        principle = _principle_from_item(
            item,
            source=f"{repo_ethos_path} additional[{len(additional_ids) + 1}]",
        )
        if principle.id in principles_by_id or principle.id in additional_ids:
            raise ValueError(
                f"Invalid ethos YAML at {repo_ethos_path}: duplicate additional principle id `{principle.id}`."
            )
        additional_ids.add(principle.id)
        merged.principles.append(principle)

    merged.principles.sort(
        key=lambda principle: (principle.order, principle.title.lower())
    )
    _validate_principle_collection(merged.principles, str(repo_ethos_path))
    return merged
