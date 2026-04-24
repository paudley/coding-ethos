"""Convert Markdown ethos documents into structured YAML source data.

These helpers extract principle sections, summaries, and lightweight metadata
so an existing prose ethos can seed the canonical YAML authoring surface.
They keep the seeding workflow deterministic enough for tests and regeneration.
"""

# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

import re
from pathlib import Path
from typing import Any

from coding_ethos.presets import (
    AGENT_PROFILES,
    PRINCIPLE_PRESETS,
    build_agent_hints,
    build_merge_topics,
    build_quick_ref,
)
from coding_ethos.yaml_utils import render_yaml

SECTION_RE = re.compile(r"^## \*\*(\d+)\.\s*(.+?)\*\*$", re.MULTILINE)
SUBSECTION_RE = re.compile(r"^###\s+(?:\*\*)?(.+?)(?:\*\*)?\s*$", re.MULTILINE)
MAIN_HEADING_RE = re.compile(r"^#\s+\*\*(.+?)\*\*$", re.MULTILINE)
RELATED_RE = re.compile(r"\(#\d+-([a-z0-9-]+)\)")
SECTION_KIND_MARKERS: tuple[tuple[str, tuple[str, ...]], ...] = (
    ("overview", ("overview", "summary", "core principle", "essence")),
    ("rationale", ("why", "rationale", "reason", "motivation", "importance")),
    (
        "anti_patterns",
        (
            "anti pattern",
            "anti patterns",
            "bad way",
            "wrong way",
            "what not to do",
            "not acceptable",
            "failure mode",
        ),
    ),
    (
        "correct_way",
        (
            "right way",
            "correct way",
            "preferred way",
            "do this instead",
            "good way",
        ),
    ),
    (
        "rule",
        ("rule", "rules", "policy", "practical rule", "non negotiable", "contract"),
    ),
    (
        "workflow",
        ("workflow", "process", "procedure", "steps", "operational implication"),
    ),
    ("examples", ("example", "examples")),
    ("reference", ("checklist", "quick ref", "reference")),
)


def slugify(value: str) -> str:
    """Convert an arbitrary heading into a stable ethos identifier slug."""
    cleaned = re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")
    return cleaned or "ethos"


def markdown_to_plain_text(markdown: str) -> str:
    """Strip common Markdown markup and return readable plain text."""
    text = re.sub(r"```.*?```", "", markdown, flags=re.DOTALL)
    text = re.sub(r"`([^`]+)`", r"\1", text)
    text = re.sub(r"\[(.*?)\]\([^)]+\)", r"\1", text)
    text = re.sub(r"\*\*([^*]+)\*\*", r"\1", text)
    text = re.sub(r"\*([^*]+)\*", r"\1", text)
    text = re.sub(r"^#+\s*", "", text, flags=re.MULTILINE)
    text = re.sub(r"^\s*[-*]\s+", "", text, flags=re.MULTILINE)
    return " ".join(text.split()).strip()


def trim_terminal_rule(markdown: str) -> str:
    """Remove a trailing horizontal rule from a Markdown fragment."""
    return re.sub(r"\n---\s*$", "", markdown.strip())


def summarize_markdown(markdown: str) -> str:
    """Extract a short summary sentence from a Markdown fragment."""
    text = re.sub(r"```.*?```", "", trim_terminal_rule(markdown), flags=re.DOTALL)
    for paragraph in text.split("\n\n"):
        candidate = paragraph.strip()
        if not candidate or candidate == "---" or candidate.startswith("### "):
            continue
        plain = markdown_to_plain_text(candidate)
        if not plain:
            continue
        sentence_match = re.match(r"(.+?[.!?])(?:\s|$)", plain)
        summary = sentence_match.group(1) if sentence_match else plain
        return summary[:240].rstrip()
    return "No summary available."


def _clean_heading(value: str) -> str:
    return value.replace("**", "").strip()


def _infer_section_kind(title: str, *, is_intro: bool = False) -> str:
    if is_intro:
        return "overview"

    normalized = re.sub(r"[^a-z0-9]+", " ", title.lower()).strip()
    if not normalized:
        return "guidance"

    for section_kind, markers in SECTION_KIND_MARKERS:
        if any(marker in normalized for marker in markers):
            return section_kind
    if "repo" in normalized:
        return "repo_context"
    return "guidance"


def _split_sections(body: str) -> list[dict[str, str]]:
    cleaned_body = trim_terminal_rule(body)
    matches = list(SUBSECTION_RE.finditer(cleaned_body))
    sections: list[dict[str, str]] = []

    if not matches:
        return [
            {
                "id": "overview",
                "kind": "overview",
                "title": "Overview",
                "summary": summarize_markdown(cleaned_body),
                "body": cleaned_body,
            }
        ]

    intro = cleaned_body[: matches[0].start()].strip()
    if intro:
        sections.append(
            {
                "id": "overview",
                "kind": "overview",
                "title": "Overview",
                "summary": summarize_markdown(intro),
                "body": intro,
            }
        )

    for index, match in enumerate(matches):
        title = _clean_heading(match.group(1))
        section_start = match.end()
        section_end = (
            matches[index + 1].start()
            if index + 1 < len(matches)
            else len(cleaned_body)
        )
        section_body = trim_terminal_rule(cleaned_body[section_start:section_end])
        sections.append(
            {
                "id": slugify(title),
                "kind": _infer_section_kind(title),
                "title": title,
                "summary": summarize_markdown(section_body),
                "body": section_body,
            }
        )

    return sections


def _extract_related(
    principle_id: str, body: str, preset_related: list[str]
) -> list[str]:
    related = {item for item in preset_related if item != principle_id}
    related.update(match for match in RELATED_RE.findall(body) if match != principle_id)
    return sorted(related)


def parse_ethos_markdown(markdown: str) -> dict[str, Any]:
    """Parse an ETHOS-style Markdown document into the YAML payload shape.

    Args:
        markdown: Full Markdown source for an existing ethos document.

    Returns:
        A serializable dictionary matching the primary ethos YAML schema.

    """
    title_match = MAIN_HEADING_RE.search(markdown)
    title = title_match.group(1) if title_match else "Coding Ethos"
    table_of_contents = markdown.find("## Table of Contents")
    if table_of_contents == -1:
        overview = ""
    else:
        overview = markdown[:table_of_contents]
        if title_match:
            overview = overview[title_match.end() :]
        overview = trim_terminal_rule(overview)

    matches = list(SECTION_RE.finditer(markdown))
    principles: list[dict[str, Any]] = []
    for index, match in enumerate(matches):
        order = int(match.group(1))
        raw_title = match.group(2).strip()
        if raw_title.lower() in {"table of contents", "in summary"}:
            continue

        principle_id = slugify(raw_title)
        section_start = match.end()
        section_end = (
            matches[index + 1].start() if index + 1 < len(matches) else len(markdown)
        )
        body = trim_terminal_rule(markdown[section_start:section_end])
        preset = PRINCIPLE_PRESETS.get(principle_id, {})
        sections = _split_sections(body)
        summary = summarize_markdown(body)
        directive = preset.get("directive", summary)
        tags = preset.get("tags", [])
        principles.append(
            {
                "id": principle_id,
                "order": order,
                "title": raw_title,
                "summary": summary,
                "directive": directive,
                "quick_ref": build_quick_ref(
                    summary=summary,
                    directive=directive,
                    section_summaries=[section["summary"] for section in sections],
                ),
                "merge_topics": build_merge_topics(title=raw_title, tags=tags),
                "tags": tags,
                "related": _extract_related(
                    principle_id, body, preset.get("related", [])
                ),
                "agent_hints": build_agent_hints(tags=tags),
                "sections": sections,
            }
        )

    return {
        "version": 2,
        "metadata": {
            "title": title,
            "overview": overview,
        },
        "agents": AGENT_PROFILES,
        "principles": principles,
    }


def seed_primary_from_markdown(source: Path, destination: Path) -> Path:
    """Generate a primary ethos YAML file from Markdown source.

    Args:
        source: Existing Markdown ethos document to parse.
        destination: Target YAML path to write.

    Returns:
        The destination path after writing the seeded YAML document.

    """
    payload = parse_ethos_markdown(source.read_text(encoding="utf-8"))
    payload["metadata"]["source_markdown"] = "ETHOS.md"
    destination.write_text(render_yaml(payload), encoding="utf-8")
    return destination
