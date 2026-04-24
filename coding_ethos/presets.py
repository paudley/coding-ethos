# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

import re
from typing import TypedDict

from coding_ethos.models import SUPPORTED_AGENTS


class PrinciplePreset(TypedDict, total=False):
    directive: str
    tags: list[str]
    related: list[str]


PRINCIPLE_PRESETS: dict[str, PrinciplePreset] = {
    "solid-is-law": {
        "directive": "Enforce SOLID and simplicity; remove speculative abstractions.",
        "tags": ["architecture", "design", "simplicity"],
        "related": ["protocol-first-design", "linting-as-code-quality-enforcement"],
    },
    "fail-fast-fail-hard-overview": {
        "directive": "Crash early on ambiguous startup and configuration states instead of degrading silently.",
        "tags": ["startup", "reliability", "configuration"],
        "related": ["validation-at-the-gate", "no-rationalized-shortcuts"],
    },
    "no-conditional-imports": {
        "directive": "Treat required imports as hard dependencies and fail immediately if they are missing.",
        "tags": ["dependency", "startup", "reliability"],
        "related": ["no-if-available-capability-checks", "validation-at-the-gate"],
    },
    "static-analysis-is-the-first-line-of-defense": {
        "directive": "Make ruff and mypy blocking quality gates rather than advisory tools.",
        "tags": ["tooling", "linting", "typing"],
        "related": ["linting-as-code-quality-enforcement", "testing-as-specification"],
    },
    "no-optional-types-for-required-dependencies": {
        "directive": "Model required dependencies as non-optional and default to full-strength behavior.",
        "tags": ["typing", "dependency", "defaults"],
        "related": ["no-conditional-validation", "robustness-in-motion-runtime"],
    },
    "no-conditional-validation": {
        "directive": "Run required validation unconditionally; a missing component is itself a failure.",
        "tags": ["validation", "reliability", "startup"],
        "related": [
            "validation-at-the-gate",
            "no-optional-types-for-required-dependencies",
        ],
    },
    "no-if-available-capability-checks": {
        "directive": "Validate required capabilities at startup instead of probing for them at runtime.",
        "tags": ["validation", "dependency", "startup"],
        "related": ["no-conditional-imports", "validation-at-the-gate"],
    },
    "validation-at-the-gate": {
        "directive": "Validate configuration, schema, and extensions during bootstrap rather than on first use.",
        "tags": ["validation", "configuration", "startup"],
        "related": ["fail-fast-fail-hard-overview", "robustness-in-motion-runtime"],
    },
    "no-inline-cli-environment-variables": {
        "directive": "Route configuration through validated bootstrap paths instead of inline shell environment variables.",
        "tags": ["configuration", "workflow", "tooling"],
        "related": ["validation-at-the-gate", "one-path-for-critical-operations"],
    },
    "robustness-in-motion-runtime": {
        "directive": "Treat startup misconfiguration and runtime transient failures as different classes of problems.",
        "tags": ["runtime", "reliability", "resilience"],
        "related": [
            "fail-fast-fail-hard-overview",
            "no-optional-types-for-required-dependencies",
        ],
    },
    "radical-visibility": {
        "directive": "Log important decisions with context and instrument the system with metrics.",
        "tags": ["observability", "logging", "metrics"],
        "related": [
            "exception-hierarchy-and-error-messages",
            "testing-as-specification",
        ],
    },
    "protocol-first-design": {
        "directive": "Define and verify interfaces before writing or referencing implementations.",
        "tags": ["architecture", "interfaces", "typing"],
        "related": ["solid-is-law", "documentation-as-contract"],
    },
    "universal-responsibility": {
        "directive": "Own every error or warning you touch and verify claims with evidence.",
        "tags": ["ownership", "quality", "verification"],
        "related": ["forward-motion-only", "testing-as-specification"],
    },
    "linting-as-code-quality-enforcement": {
        "directive": "Resolve lint findings with structural fixes; suppress only with documented necessity.",
        "tags": ["linting", "quality", "refactor"],
        "related": ["static-analysis-is-the-first-line-of-defense", "solid-is-law"],
    },
    "feedback-as-a-first-class-citizen": {
        "directive": "Retrieve and address all review feedback proactively.",
        "tags": ["collaboration", "feedback", "review"],
        "related": ["universal-responsibility", "forward-motion-only"],
    },
    "no-self-promotion": {
        "directive": "Let the work speak and omit self-congratulatory commentary.",
        "tags": ["communication", "collaboration"],
        "related": ["feedback-as-a-first-class-citizen", "documentation-as-contract"],
    },
    "functional-idioms": {
        "directive": "Use Python's functional tools when they make code clearer and more local.",
        "tags": ["python", "style", "simplicity"],
        "related": ["solid-is-law", "documentation-as-contract"],
    },
    "documentation-as-contract": {
        "directive": "Keep public behavior documented as part of the interface contract.",
        "tags": ["documentation", "api", "quality"],
        "related": ["protocol-first-design", "exception-hierarchy-and-error-messages"],
    },
    "one-path-for-critical-operations": {
        "directive": "Keep one explicit, validated path for critical operations.",
        "tags": ["workflow", "validation", "reliability"],
        "related": ["no-inline-cli-environment-variables", "no-rationalized-shortcuts"],
    },
    "forward-motion-only": {
        "directive": "Fix the current state instead of blaming history or prior authors.",
        "tags": ["ownership", "workflow", "collaboration"],
        "related": ["universal-responsibility", "feedback-as-a-first-class-citizen"],
    },
    "no-rationalized-shortcuts": {
        "directive": "Do not discard work or bypass safety checks in the name of pragmatism.",
        "tags": ["workflow", "safety", "git"],
        "related": ["one-path-for-critical-operations", "forward-motion-only"],
    },
    "testing-as-specification": {
        "directive": "Treat tests as executable behavioral contracts and update them with code changes.",
        "tags": ["testing", "quality", "specification"],
        "related": ["documentation-as-contract", "universal-responsibility"],
    },
    "exception-hierarchy-and-error-messages": {
        "directive": "Use precise exception types and actionable, context-rich error messages.",
        "tags": ["errors", "debugging", "api"],
        "related": ["radical-visibility", "documentation-as-contract"],
    },
    "security-by-design": {
        "directive": "Design for least privilege, validation, and safe defaults from the start.",
        "tags": ["security", "validation", "defaults"],
        "related": ["validation-at-the-gate", "no-rationalized-shortcuts"],
    },
    "sub-agent-delegation-and-context-isolation": {
        "directive": "Use specialized agents with scoped context instead of overloading one thread.",
        "tags": ["delegation", "context", "workflow"],
        "related": [
            "one-path-for-critical-operations",
            "feedback-as-a-first-class-citizen",
        ],
    },
}


AGENT_PROFILES: dict[str, dict[str, object]] = {
    "codex": {
        "root_file": "AGENTS.md",
        "notes": [
            "Keep the root file concise, operational, and repo-specific.",
            "Put durable project rules in AGENTS.md and link to deep docs instead of pasting long prose.",
        ],
    },
    "claude": {
        "root_file": "CLAUDE.md",
        "supporting_files": [".claude/ethos/MEMORY.md"],
        "notes": [
            "Use CLAUDE.md as a short import hub, not a giant policy dump.",
            "Mirror Claude memory style with one deep linked note per ethos entry.",
        ],
    },
    "gemini": {
        "root_file": "GEMINI.md",
        "notes": [
            "Keep GEMINI.md brief and hierarchical.",
            "Prefer targeted reads of linked detail docs instead of loading the full corpus every time.",
        ],
    },
}

TAG_TO_MERGE_TOPIC = {
    "architecture": "architecture decisions",
    "design": "design constraints",
    "simplicity": "abstraction boundaries",
    "startup": "startup behavior",
    "reliability": "failure handling",
    "configuration": "configuration flow",
    "dependency": "dependency policy",
    "tooling": "tooling gates",
    "linting": "lint policy",
    "typing": "type contracts",
    "defaults": "default behavior",
    "validation": "validation rules",
    "workflow": "workflow rules",
    "runtime": "runtime behavior",
    "resilience": "runtime degradation",
    "observability": "observability requirements",
    "logging": "structured logging",
    "metrics": "metrics coverage",
    "interfaces": "interface design",
    "ownership": "ownership rules",
    "quality": "quality gates",
    "verification": "verification standards",
    "refactor": "refactor expectations",
    "collaboration": "collaboration workflow",
    "feedback": "review handling",
    "review": "review process",
    "communication": "communication norms",
    "python": "python style",
    "documentation": "documentation contract",
    "api": "API contracts",
    "safety": "safety constraints",
    "git": "git safety",
    "testing": "test requirements",
    "specification": "behavioral specification",
    "errors": "error contracts",
    "debugging": "debugging clarity",
    "security": "security constraints",
    "delegation": "delegation workflow",
    "context": "context isolation",
}

TAG_TO_AGENT_HINTS = {
    "architecture": {
        "codex": "Prefer structural refactors over tactical patches when this principle is implicated.",
        "claude": "Open the detailed ethos note before changing architecture or interfaces.",
        "gemini": "Use this rule to keep plans structurally coherent and avoid ad hoc abstractions.",
    },
    "validation": {
        "codex": "Treat missing prerequisites as blocking validation failures, not optional branches.",
        "claude": "Surface skipped checks and missing prerequisites explicitly during review.",
        "gemini": "Keep validation guidance strict and explicit rather than permissive.",
    },
    "workflow": {
        "codex": "Preserve deterministic workflows and avoid introducing alternate critical paths.",
        "claude": "Keep process guidance actionable and aligned with the existing repo workflow.",
        "gemini": "Summarize workflow expectations as concrete steps, not general advice.",
    },
    "testing": {
        "codex": "Treat tests as required follow-through for code changes, not optional cleanup.",
        "claude": "Call out testing gaps and mismatched behavioral contracts explicitly.",
        "gemini": "Use this principle to keep output anchored to observable behavior.",
    },
    "security": {
        "codex": "Prefer least-privilege and boundary-enforcing changes over soft runtime checks.",
        "claude": "Highlight trust-boundary and secret-handling implications during review.",
        "gemini": "Keep summaries explicit about security constraints and forbidden shortcuts.",
    },
}

DEFAULT_AGENT_HINTS = {
    "codex": "Apply this rule directly in code edits and review decisions.",
    "claude": "Reference this rule explicitly when reviewing or merging repo guidance.",
    "gemini": "Use this rule to keep high-level guidance concise, concrete, and enforceable.",
}


def _slug_to_phrase(value: str) -> str:
    return re.sub(r"[-_]+", " ", value).strip()


def build_quick_ref(
    *,
    summary: str,
    directive: str,
    section_summaries: list[str],
) -> list[str]:
    items: list[str] = []
    for candidate in [directive, *section_summaries, summary]:
        cleaned = candidate.strip()
        if cleaned and cleaned not in items:
            items.append(cleaned)
        if len(items) == 3:
            break
    return items


def build_merge_topics(*, title: str, tags: list[str]) -> list[str]:
    items = [_slug_to_phrase(title.lower())]
    for tag in tags:
        topic = TAG_TO_MERGE_TOPIC.get(tag, _slug_to_phrase(tag))
        if topic not in items:
            items.append(topic)
        if len(items) == 3:
            break
    return items


def build_agent_hints(*, tags: list[str]) -> dict[str, str]:
    hints = dict(DEFAULT_AGENT_HINTS)
    for tag in tags:
        tag_hints = TAG_TO_AGENT_HINTS.get(tag)
        if not tag_hints:
            continue
        for agent in SUPPORTED_AGENTS:
            hints[agent] = tag_hints.get(agent, hints[agent])
    return hints
