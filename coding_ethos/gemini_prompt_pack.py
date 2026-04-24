# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Generate grounded Gemini prompt packs from ethos and repo policy."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader, StrictUndefined, select_autoescape

from coding_ethos.loaders import load_primary_bundle, merge_repo_ethos
from coding_ethos.models import EthosBundle, Principle
from coding_ethos.tool_configs import load_enforcement_config

GENERATED_GEMINI_PROMPT_FILES: tuple[str, ...] = (
    ".code-ethos/gemini/prompt-pack.json",
)

_CHECK_SPECS: dict[str, dict[str, object]] = {
    "code_ethos": {
        "file_scope": "code",
        "batch_size": 3,
        "max_file_size_kb": 50,
        "selector": {
            "include_extensions": [
                ".py",
                ".pyi",
                ".sh",
                ".bash",
                ".go",
                ".rs",
                ".ts",
                ".js",
            ],
            "exclude_substrings": [
                "test_",
                "_test.",
                ".test.",
                "/tests/",
                "/test/",
                "/__pycache__/",
                "/node_modules/",
                "/vendor/",
                "/.venv/",
                "/venv/",
                "/migrations/",
            ],
            "exclude_prefixes": [
                ".venv/",
                "venv/",
                "__pycache__/",
                "node_modules/",
            ],
            "allow_extensionless_in_scripts": False,
            "shebang_markers": ["python", "bash", "sh"],
        },
    },
    "shell_review": {
        "file_scope": "shell",
        "batch_size": 5,
        "max_file_size_kb": 50,
        "selector": {
            "include_extensions": [".sh", ".bash"],
            "exclude_substrings": [],
            "exclude_prefixes": [],
            "allow_extensionless_in_scripts": True,
            "shebang_markers": ["bash", "sh"],
        },
    },
    "shell_ethos": {
        "file_scope": "shell",
        "batch_size": 5,
        "max_file_size_kb": 30,
        "selector": {
            "include_extensions": [".sh", ".bash"],
            "exclude_substrings": [],
            "exclude_prefixes": [],
            "allow_extensionless_in_scripts": True,
            "shebang_markers": ["bash", "sh"],
        },
    },
    "shell_documentation": {
        "file_scope": "shell",
        "batch_size": 5,
        "max_file_size_kb": 50,
        "selector": {
            "include_extensions": [".sh", ".bash"],
            "exclude_substrings": [],
            "exclude_prefixes": [],
            "allow_extensionless_in_scripts": True,
            "shebang_markers": ["bash", "sh"],
        },
    },
    "shellcheck_suppression": {
        "file_scope": "shell",
        "batch_size": 8,
        "max_file_size_kb": 50,
        "selector": {
            "include_extensions": [".sh", ".bash"],
            "exclude_substrings": [],
            "exclude_prefixes": [],
            "allow_extensionless_in_scripts": True,
            "shebang_markers": ["bash", "sh"],
        },
    },
    "shell_placeholder": {
        "file_scope": "shell",
        "batch_size": 10,
        "max_file_size_kb": 50,
        "selector": {
            "include_extensions": [".sh", ".bash"],
            "exclude_substrings": [],
            "exclude_prefixes": [],
            "allow_extensionless_in_scripts": True,
            "shebang_markers": ["bash", "sh"],
        },
    },
}

_TEMPLATE_FILES: dict[str, str] = {
    "code_ethos": "code_ethos.j2",
    "shell_review": "shell_review.j2",
    "shell_ethos": "shell_ethos.j2",
    "shell_documentation": "shell_documentation.j2",
    "shellcheck_suppression": "shellcheck_suppression.j2",
    "shell_placeholder": "shell_placeholder.j2",
}


def _ethos_root() -> Path:
    return Path(__file__).resolve().parent.parent


def resolve_repo_ethos(
    repo_root: Path, explicit_repo_ethos: Path | None = None
) -> Path | None:
    """Resolve the optional repo-specific ethos overlay for a target repo."""
    if explicit_repo_ethos is not None:
        return explicit_repo_ethos.expanduser().resolve()
    for name in ("repo_ethos.yml", "repo_ethos.yaml"):
        candidate = repo_root / name
        if candidate.exists():
            return candidate.resolve()
    return None


def _jinja_environment() -> Environment:
    template_root = _ethos_root() / "pre-commit" / "prompts"
    return Environment(
        loader=FileSystemLoader(str(template_root)),
        autoescape=select_autoescape(
            disabled_extensions=("j2",),
            default_for_string=False,
            default=False,
        ),
        undefined=StrictUndefined,
        trim_blocks=True,
        lstrip_blocks=True,
        keep_trailing_newline=True,
    )


def _relative_path(path: Path | None, *, relative_to: Path) -> str:
    if path is None:
        return ""
    try:
        return str(path.relative_to(relative_to))
    except ValueError:
        return str(path)


def _string_list(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    stripped = str(value).strip()
    return [stripped] if stripped else []


def _project_name(bundle: EthosBundle, config: dict[str, Any], repo_root: Path) -> str:
    if bundle.repo.name:
        return bundle.repo.name
    if repo_root.name:
        return repo_root.name
    configured = str(config.get("project", {}).get("name", "")).strip()
    if configured:
        return configured
    return "repository"


def _project_context(bundle: EthosBundle, config: dict[str, Any]) -> str:
    if bundle.repo.overview:
        return bundle.repo.overview
    configured = str(config.get("project", {}).get("review_context", "")).strip()
    if configured:
        return configured
    return "shared repository automation and engineering tooling"


def _dedupe_preserve_order(items: list[str]) -> list[str]:
    seen: set[str] = set()
    deduped: list[str] = []
    for item in items:
        stripped = item.strip()
        if not stripped or stripped in seen:
            continue
        seen.add(stripped)
        deduped.append(stripped)
    return deduped


def _principle_payload(principle: Principle) -> dict[str, Any]:
    return {
        "id": principle.id,
        "order": principle.order,
        "title": principle.title,
        "directive": principle.directive,
        "summary": principle.summary,
        "quick_ref": list(principle.quick_ref[:3]),
        "agent_hint": principle.agent_hints.get("gemini", "").strip(),
    }


def _repo_commands_payload(bundle: EthosBundle) -> list[dict[str, Any]]:
    return [
        {"name": name, "examples": list(commands)}
        for name, commands in bundle.repo.commands.items()
        if commands
    ]


def _repo_paths_payload(bundle: EthosBundle) -> list[dict[str, str]]:
    return [
        {"name": name, "path": path}
        for name, path in bundle.repo.paths.items()
        if str(path).strip()
    ]


def _mapping_section(value: object) -> dict[str, object]:
    if isinstance(value, dict):
        return {str(key): item for key, item in value.items()}
    return {}


def _style_notes(style: dict[str, object], python: dict[str, object]) -> list[str]:
    notes: list[str] = []

    line_length = style.get("line_length")
    if line_length:
        notes.append(f"Shared line length is {line_length} characters.")

    python_version = str(style.get("python_version", "")).strip()
    if python_version:
        notes.append(f"Target Python version is {python_version}.")

    source_paths = _string_list(python.get("source_paths"))
    if source_paths:
        notes.append(f"Primary source paths: {', '.join(source_paths)}.")

    test_paths = _string_list(python.get("test_paths"))
    if test_paths:
        notes.append(f"Primary test paths: {', '.join(test_paths)}.")

    stub_paths = _string_list(python.get("stub_paths"))
    if stub_paths:
        notes.append(f"Stub paths: {', '.join(stub_paths)}.")
    return notes


def _direct_import_note(python: dict[str, object]) -> str:
    direct_imports = _mapping_section(python.get("direct_imports"))
    if not direct_imports.get("enabled"):
        return ""
    packages = _string_list(direct_imports.get("packages"))
    if not packages:
        return ""
    return (
        "Direct internal imports are restricted for packages: "
        + ", ".join(packages)
        + "."
    )


def _util_centralization_note(python: dict[str, object]) -> str:
    util_centralization = _mapping_section(python.get("util_centralization"))
    if not util_centralization.get("enabled"):
        return ""

    banned_entries: list[str] = []
    banned = util_centralization.get("banned_modules", [])
    if not isinstance(banned, list):
        return ""

    for item in banned:
        if isinstance(item, dict):
            module = str(item.get("module", "")).strip()
            alternative = str(item.get("alternative", "")).strip()
            if module and alternative:
                banned_entries.append(f"{module} -> {alternative}")
            elif module:
                banned_entries.append(module)
            continue

        stripped = str(item).strip()
        if stripped:
            banned_entries.append(stripped)

    if not banned_entries:
        return ""

    return (
        "Utility centralization is enabled; banned direct imports: "
        + "; ".join(banned_entries)
        + "."
    )


def _sql_centralization_note(python: dict[str, object]) -> str:
    sql_centralization = _mapping_section(python.get("sql_centralization"))
    if not sql_centralization.get("enabled"):
        return ""
    module_name = str(sql_centralization.get("module_name", "")).strip()
    central_paths = _string_list(sql_centralization.get("central_paths"))
    bits: list[str] = []
    if module_name:
        bits.append(f"module {module_name}")
    if central_paths:
        bits.append(f"paths {', '.join(central_paths)}")
    if not bits:
        return ""
    return (
        "SQL centralization is enabled; keep raw query strings in "
        + " and ".join(bits)
        + "."
    )


def _plan_completion_note(python: dict[str, object]) -> str:
    plan_completion = _mapping_section(python.get("plan_completion"))
    if not plan_completion.get("enabled"):
        return ""
    root_markers = _string_list(plan_completion.get("root_markers"))
    metadata_filename = str(plan_completion.get("metadata_filename", "")).strip()
    details: list[str] = []
    if root_markers:
        details.append(f"plan roots {', '.join(root_markers)}")
    if metadata_filename:
        details.append(f"metadata file {metadata_filename}")
    if not details:
        return ""
    return "Plan workflow enforcement is enabled; " + ", ".join(details) + "."


def _pytest_gate_note(python: dict[str, object]) -> str:
    pytest_gate = _mapping_section(python.get("pytest_gate"))
    if not pytest_gate.get("enabled"):
        return ""
    test_command = _string_list(pytest_gate.get("test_command"))
    if not test_command:
        return ""
    return f"Pytest gate command: {' '.join(test_command)}."


def _gemini_allowlist_note(gemini: dict[str, object]) -> str:
    modal_allowlist_files = _string_list(gemini.get("modal_allowlist_files"))
    if not modal_allowlist_files:
        return ""
    return "Gemini modal-path allowlist: " + ", ".join(modal_allowlist_files) + "."


def _enforcement_notes(config: dict[str, Any]) -> list[str]:
    """Build concise enforcement notes for prompt grounding."""
    python = _mapping_section(config.get("python"))
    style = _mapping_section(config.get("style"))
    gemini = _mapping_section(config.get("gemini"))
    notes = _style_notes(style, python)
    notes.extend(
        note
        for note in (
            _direct_import_note(python),
            _util_centralization_note(python),
            _sql_centralization_note(python),
            _plan_completion_note(python),
            _pytest_gate_note(python),
            _gemini_allowlist_note(gemini),
        )
        if note
    )
    return notes


def _build_template_context(
    bundle: EthosBundle,
    config: dict[str, Any],
    repo_root: Path,
) -> dict[str, Any]:
    principles = [_principle_payload(principle) for principle in bundle.principles]
    profile_notes = (
        bundle.agent_profiles.get("gemini").notes
        if bundle.agent_profiles.get("gemini")
        else []
    )
    gemini_notes = _dedupe_preserve_order(
        profile_notes + bundle.repo.agent_notes.get("gemini", [])
    )

    return {
        "project_name": _project_name(bundle, config, repo_root),
        "project_context": _project_context(bundle, config),
        "repo_overview": bundle.repo.overview.strip(),
        "repo_commands": _repo_commands_payload(bundle),
        "repo_paths": _repo_paths_payload(bundle),
        "repo_notes": list(bundle.repo.notes),
        "gemini_notes": gemini_notes,
        "principles": principles,
        "enforcement_notes": _enforcement_notes(config),
        "code_content_placeholder": "{code_content}",
    }


def _render_prompts(context: dict[str, Any]) -> dict[str, str]:
    env = _jinja_environment()
    prompts: dict[str, str] = {}
    for check_name, template_name in _TEMPLATE_FILES.items():
        template = env.get_template(f"checks/{template_name}")
        prompts[check_name] = template.render(**context).rstrip() + "\n"
    return prompts


def _render_check_specs() -> dict[str, dict[str, object]]:
    return {
        name: {
            "file_scope": str(spec["file_scope"]),
            "batch_size": int(spec["batch_size"]),
            "max_file_size_kb": int(spec["max_file_size_kb"]),
            "selector": {
                "include_extensions": list(spec["selector"]["include_extensions"]),
                "exclude_substrings": list(spec["selector"]["exclude_substrings"]),
                "exclude_prefixes": list(spec["selector"]["exclude_prefixes"]),
                "allow_extensionless_in_scripts": bool(
                    spec["selector"]["allow_extensionless_in_scripts"]
                ),
                "shebang_markers": list(spec["selector"]["shebang_markers"]),
            },
        }
        for name, spec in _CHECK_SPECS.items()
    }


def render_gemini_prompt_pack(
    repo_root: Path,
    primary_path: Path,
    repo_ethos_path: Path | None = None,
    repo_config_path: Path | None = None,
) -> str:
    """Render the deterministic Gemini prompt pack for a target repo."""
    bundle = merge_repo_ethos(load_primary_bundle(primary_path), repo_ethos_path)
    config, resolved_repo_config = load_enforcement_config(repo_root, repo_config_path)
    context = _build_template_context(bundle, config, repo_root)
    prompts = _render_prompts(context)

    payload = {
        "version": 1,
        "sources": {
            "primary": _relative_path(primary_path, relative_to=_ethos_root()),
            "repo_ethos": _relative_path(repo_ethos_path, relative_to=repo_root),
            "repo_config": _relative_path(resolved_repo_config, relative_to=repo_root),
        },
        "project": {
            "name": context["project_name"],
            "context": context["project_context"],
            "repo_overview": context["repo_overview"],
        },
        "grounding": {
            "principles": context["principles"],
            "repo_commands": context["repo_commands"],
            "repo_paths": context["repo_paths"],
            "repo_notes": context["repo_notes"],
            "gemini_notes": context["gemini_notes"],
            "enforcement_notes": context["enforcement_notes"],
        },
        "checks": _render_check_specs(),
        "prompts": prompts,
    }
    return json.dumps(payload, indent=2, sort_keys=True) + "\n"


def _prompt_pack_paths(repo_root: Path) -> list[Path]:
    return [repo_root / relative for relative in GENERATED_GEMINI_PROMPT_FILES]


def sync_gemini_prompt_pack(
    repo_root: Path,
    primary_path: Path,
    repo_ethos_path: Path | None = None,
    repo_config_path: Path | None = None,
) -> list[Path]:
    """Write the rendered Gemini prompt pack into the target repo."""
    rendered = render_gemini_prompt_pack(
        repo_root=repo_root,
        primary_path=primary_path,
        repo_ethos_path=repo_ethos_path,
        repo_config_path=repo_config_path,
    )
    written: list[Path] = []
    for path in _prompt_pack_paths(repo_root):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(rendered, encoding="utf-8")
        written.append(path)
    return written


def check_gemini_prompt_pack(
    repo_root: Path,
    primary_path: Path,
    repo_ethos_path: Path | None = None,
    repo_config_path: Path | None = None,
) -> list[Path]:
    """Return generated prompt-pack paths that are missing or out of sync."""
    expected = render_gemini_prompt_pack(
        repo_root=repo_root,
        primary_path=primary_path,
        repo_ethos_path=repo_ethos_path,
        repo_config_path=repo_config_path,
    )
    return [
        path
        for path in _prompt_pack_paths(repo_root)
        if not path.exists() or path.read_text(encoding="utf-8") != expected
    ]
