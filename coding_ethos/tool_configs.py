# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

from __future__ import annotations

import configparser
import json
from pathlib import Path
from typing import Any

import yaml

GENERATED_TOOL_CONFIGS: tuple[str, ...] = (
    "pyrightconfig.json",
    "mypy.ini",
    "ruff.toml",
    ".yamllint.yml",
)

_DEFAULT_REPO_CONFIG_NAMES: tuple[str, ...] = (
    "repo_config.yaml",
    "repo_config.yml",
    "code-ethos.repo.yaml",
    "code-ethos.repo.yml",
    "coding-ethos.repo.yaml",
    "coding-ethos.repo.yml",
    "code-ethos.pre-commit.yaml",
    "code-ethos.pre-commit.yml",
    "coding-ethos.pre-commit.yaml",
    "coding-ethos.pre-commit.yml",
)


def _ethos_root() -> Path:
    return Path(__file__).resolve().parent.parent


def _load_yaml(path: Path) -> dict[str, Any]:
    payload = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(payload, dict):
        raise ValueError(
            f"Invalid config YAML at {path}: expected a mapping at the document root."
        )
    return payload


def _deep_merge(base: Any, override: Any) -> Any:
    if isinstance(base, dict) and isinstance(override, dict):
        merged = dict(base)
        for key, value in override.items():
            if key in merged:
                merged[key] = _deep_merge(merged[key], value)
            else:
                merged[key] = value
        return merged
    return override


def _get(config: dict[str, Any], path: str, default: Any = None) -> Any:
    current: Any = config
    for segment in path.split("."):
        if not isinstance(current, dict) or segment not in current:
            return default
        current = current[segment]
    return current


def _string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    stripped = str(value).strip()
    return [stripped] if stripped else []


def _configured_list(
    config: dict[str, Any], path: str, fallback: list[str]
) -> list[str]:
    values = _string_list(_get(config, path, []))
    return values or list(fallback)


def _configured_string(config: dict[str, Any], path: str, fallback: str) -> str:
    configured = _truthy_string(_get(config, path, ""))
    return configured or fallback


def _truthy_string(value: Any) -> str:
    return str(value).strip()


def _python_version(config: dict[str, Any]) -> str:
    return _truthy_string(_get(config, "style.python_version", "3.13")) or "3.13"


def _line_length(config: dict[str, Any]) -> int:
    return int(_get(config, "style.line_length", 88))


def _ruff_target_version(config: dict[str, Any]) -> str:
    configured = _truthy_string(_get(config, "tooling.ruff.target_version", ""))
    if configured:
        return configured
    return f"py{_python_version(config).replace('.', '')}"


def _source_paths(config: dict[str, Any]) -> list[str]:
    paths = _string_list(_get(config, "python.source_paths", []))
    if paths:
        return paths
    return _string_list(_get(config, "python.direct_imports.packages", []))


def _test_paths(config: dict[str, Any]) -> list[str]:
    return _string_list(_get(config, "python.test_paths", ["tests"]))


def _stub_paths(config: dict[str, Any]) -> list[str]:
    return _string_list(_get(config, "python.stub_paths", []))


def _extra_paths(config: dict[str, Any]) -> list[str]:
    return _string_list(_get(config, "python.extra_paths", ["."]))


def resolve_repo_config(
    repo_root: Path,
    explicit_repo_config: Path | None = None,
    *,
    base_config: dict[str, Any] | None = None,
) -> Path | None:
    if explicit_repo_config is not None:
        return explicit_repo_config.expanduser().resolve()

    resolved_base = base_config or _load_yaml(_ethos_root() / "config.yaml")
    configured_names = _string_list(
        _get(
            resolved_base,
            "bundle.consumer_override_candidates",
            list(_DEFAULT_REPO_CONFIG_NAMES),
        )
    )
    candidate_names = configured_names or list(_DEFAULT_REPO_CONFIG_NAMES)

    for name in candidate_names:
        candidate = repo_root / name
        if candidate.exists():
            return candidate.resolve()
    return None


def load_enforcement_config(
    repo_root: Path,
    repo_config_path: Path | None = None,
) -> tuple[dict[str, Any], Path | None]:
    base_config = _load_yaml(_ethos_root() / "config.yaml")
    resolved_repo_config = resolve_repo_config(
        repo_root,
        repo_config_path,
        base_config=base_config,
    )
    if resolved_repo_config is None or not resolved_repo_config.exists():
        return base_config, resolved_repo_config
    return _deep_merge(
        base_config, _load_yaml(resolved_repo_config)
    ), resolved_repo_config


def _toml_string(value: str) -> str:
    return json.dumps(value)


def _toml_list(values: list[Any]) -> str:
    if not values:
        return "[]"
    rendered = ", ".join(
        "true"
        if value is True
        else "false"
        if value is False
        else str(value)
        if isinstance(value, int)
        else _toml_string(str(value))
        for value in values
    )
    return f"[{rendered}]"


def _path_patterns(paths: list[str]) -> list[str]:
    patterns: list[str] = []
    for raw in paths:
        path = raw.strip().strip("/")
        if not path:
            continue
        if path.endswith(("*.py", "*.pyi")):
            patterns.append(path)
        else:
            patterns.append(f"{path}/**")
    return patterns


def _sql_ignore_patterns(config: dict[str, Any]) -> dict[str, list[str]]:
    if not bool(_get(config, "python.sql_centralization.enabled", False)):
        return {}
    ignore_codes = _string_list(
        _get(config, "tooling.ruff.sql_per_file_ignores", ["S608"])
    )
    if not ignore_codes:
        return {}

    patterns: dict[str, list[str]] = {}
    for raw in _string_list(
        _get(config, "python.sql_centralization.central_paths", [])
    ):
        path = raw.strip().strip("/")
        if not path:
            continue
        pattern = path if "." in Path(path).name else f"{path}/**"
        patterns[pattern] = list(ignore_codes)
    return patterns


def _ruff_per_file_ignores(config: dict[str, Any]) -> dict[str, list[str]]:
    stub_codes = _string_list(_get(config, "tooling.ruff.stub_per_file_ignores", []))
    test_codes = _string_list(_get(config, "tooling.ruff.test_per_file_ignores", []))
    configured = _get(config, "tooling.ruff.extra_per_file_ignores", {}) or {}
    if configured and not isinstance(configured, dict):
        raise ValueError("tooling.ruff.extra_per_file_ignores must be a mapping.")

    ignores: dict[str, list[str]] = {}
    for pattern in _path_patterns(_stub_paths(config)):
        ignores[pattern] = list(stub_codes)
    for pattern in _path_patterns(_test_paths(config)):
        ignores[pattern] = list(test_codes)
    ignores.update(_sql_ignore_patterns(config))

    for pattern, codes in configured.items():
        ignores[str(pattern)] = _string_list(codes)
    return {pattern: codes for pattern, codes in ignores.items() if codes}


def render_pyrightconfig(config: dict[str, Any]) -> str:
    payload: dict[str, Any] = {
        "typeCheckingMode": _truthy_string(
            _get(config, "tooling.pyright.type_checking_mode", "strict")
        )
        or "strict",
        "include": _configured_list(
            config, "tooling.pyright.include", _source_paths(config)
        ),
        "exclude": _string_list(
            _get(
                config,
                "tooling.pyright.exclude",
                ["**/tests/**", "**/*_test.py", "**/test_*.py", "**/.venv/**"],
            )
        ),
        "extraPaths": _configured_list(
            config, "tooling.pyright.extra_paths", _extra_paths(config)
        ),
        "venvPath": _configured_string(
            config,
            "tooling.pyright.venv_path",
            _truthy_string(_get(config, "python.venv_path", ".")),
        ),
        "venv": _configured_string(
            config,
            "tooling.pyright.venv",
            _truthy_string(_get(config, "python.venv", ".venv")),
        ),
        "pythonVersion": _python_version(config),
    }

    stub_path = _configured_string(
        config,
        "tooling.pyright.stub_path",
        _stub_paths(config)[0] if _stub_paths(config) else "",
    )
    if stub_path:
        payload["stubPath"] = stub_path

    return json.dumps(payload, indent=4) + "\n"


def _mypy_exclude_regex(config: dict[str, Any]) -> str:
    patterns = _string_list(
        _get(
            config,
            "tooling.mypy.exclude_patterns",
            [
                r"(^|/)tests/",
                r"(^|/).*_test\.py$",
                r"(^|/)test_.*\.py$",
                r"(^|/)\.venv/",
            ],
        )
    )
    return "|".join(patterns)


def render_mypy_ini(config: dict[str, Any]) -> str:
    parser = configparser.ConfigParser()
    parser.optionxform = str
    parser["mypy"] = {
        "strict": "True"
        if bool(_get(config, "tooling.mypy.strict", True))
        else "False",
        "warn_unused_configs": "True"
        if bool(_get(config, "tooling.mypy.warn_unused_configs", True))
        else "False",
        "python_version": _python_version(config),
    }

    files = _configured_list(config, "tooling.mypy.files", _source_paths(config))
    if files:
        parser["mypy"]["files"] = ", ".join(files)

    plugins = _string_list(_get(config, "tooling.mypy.plugins", ["pydantic.mypy"]))
    if plugins:
        parser["mypy"]["plugins"] = ", ".join(plugins)

    mypy_path = _configured_string(
        config, "tooling.mypy.mypy_path", ",".join(_stub_paths(config))
    )
    if mypy_path:
        parser["mypy"]["mypy_path"] = mypy_path

    exclude = _mypy_exclude_regex(config)
    if exclude:
        parser["mypy"]["exclude"] = exclude

    if "pydantic.mypy" in plugins:
        parser["pydantic-mypy"] = {
            "init_forbid_extra": "True",
            "init_typed": "True",
            "warn_required_dynamic_aliases": "True",
        }

    lines: list[str] = []
    for section in parser.sections():
        lines.append(f"[{section}]")
        for key, value in parser[section].items():
            lines.append(f"{key} = {value}")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def render_ruff_toml(config: dict[str, Any]) -> str:
    lines = [
        f'target-version = "{_ruff_target_version(config)}"',
        f"line-length = {_line_length(config)}",
    ]

    exclude = _string_list(
        _get(
            config,
            "tooling.ruff.exclude",
            [
                ".git",
                ".venv",
                ".mypy_cache",
                ".ruff_cache",
                "__pycache__",
                "*.egg-info",
                ".eggs",
                "build",
                "dist",
                "node_modules",
            ],
        )
    )
    if exclude:
        lines.append(f"exclude = {_toml_list(exclude)}")

    lines.extend(
        [
            "",
            "[lint]",
            f"select = {_toml_list(_string_list(_get(config, 'tooling.ruff.select', ['ALL'])))}",
            f"ignore = {_toml_list(_string_list(_get(config, 'tooling.ruff.ignore', [])))}",
            "",
            "[lint.pylint]",
            f"max-args = {int(_get(config, 'tooling.ruff.max_args', 6))}",
        ]
    )

    per_file_ignores = _ruff_per_file_ignores(config)
    if per_file_ignores:
        lines.extend(["", "[lint.per-file-ignores]"])
        for pattern in sorted(per_file_ignores):
            lines.append(
                f"{_toml_string(pattern)} = {_toml_list(per_file_ignores[pattern])}"
            )

    banned_api = _get(config, "tooling.ruff.banned_api", {}) or {}
    if banned_api and not isinstance(banned_api, dict):
        raise ValueError("tooling.ruff.banned_api must be a mapping.")
    if banned_api:
        lines.extend(["", "[lint.flake8-tidy-imports.banned-api]"])
        for module_name in sorted(banned_api):
            message = _truthy_string(banned_api[module_name])
            if message:
                lines.append(
                    f"{_toml_string(module_name)} = {{ msg = {_toml_string(message)} }}"
                )

    return "\n".join(lines).rstrip() + "\n"


def render_yamllint_config(config: dict[str, Any]) -> str:
    payload: dict[str, Any] = {
        "extends": _truthy_string(_get(config, "tooling.yamllint.extends", "default"))
        or "default",
        "rules": _get(config, "tooling.yamllint.rules", {}),
    }
    if not isinstance(payload["rules"], dict):
        raise ValueError("tooling.yamllint.rules must be a mapping.")

    rules = dict(payload["rules"])
    line_length = dict(rules.get("line-length", {}))
    line_length["max"] = _line_length(config)
    rules["line-length"] = line_length
    payload["rules"] = rules

    return yaml.safe_dump(payload, sort_keys=False)


def render_tool_configs(config: dict[str, Any]) -> dict[str, str]:
    return {
        "pyrightconfig.json": render_pyrightconfig(config),
        "mypy.ini": render_mypy_ini(config),
        "ruff.toml": render_ruff_toml(config),
        ".yamllint.yml": render_yamllint_config(config),
    }


def sync_tool_configs(
    repo_root: Path, repo_config_path: Path | None = None
) -> list[Path]:
    config, _ = load_enforcement_config(repo_root, repo_config_path)
    rendered = render_tool_configs(config)
    written: list[Path] = []
    for relative_path, content in rendered.items():
        absolute_path = repo_root / relative_path
        absolute_path.write_text(content, encoding="utf-8")
        written.append(absolute_path)
    return written


def check_tool_configs(
    repo_root: Path, repo_config_path: Path | None = None
) -> list[Path]:
    config, _ = load_enforcement_config(repo_root, repo_config_path)
    rendered = render_tool_configs(config)
    mismatched: list[Path] = []
    for relative_path, expected in rendered.items():
        absolute_path = repo_root / relative_path
        current = (
            absolute_path.read_text(encoding="utf-8")
            if absolute_path.exists()
            else None
        )
        if current != expected:
            mismatched.append(absolute_path)
    return mismatched
