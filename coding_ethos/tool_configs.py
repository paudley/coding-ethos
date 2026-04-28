# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Generate repo-root linter and type-checker config files from policy YAML.

This module merges bundle defaults with optional consumer overrides and renders
deterministic tool config files for editors, hooks, and local CLI workflows.
It keeps cross-tool settings like Python version and line length synchronized.
"""

import configparser
import json
from pathlib import Path
from typing import Any, cast

import yaml

from coding_ethos.yaml_utils import render_yaml

GENERATED_TOOL_CONFIGS: tuple[str, ...] = (
    "pyrightconfig.json",
    "mypy.ini",
    "ruff.toml",
    ".pylintrc",
    ".yamllint.yml",
    ".golangci.yml",
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
HASH_SPDX_HEADER = (
    "# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. "
    "<paudley@blackcat.ca>\n"
    "# SPDX-License-Identifier: MIT\n\n"
)


def _ethos_root() -> Path:
    return Path(__file__).resolve().parent.parent


ConfigMap = dict[str, Any]


def _load_yaml(path: Path) -> ConfigMap:
    payload: object = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(payload, dict):
        msg = f"Invalid config YAML at {path}: expected a mapping at the document root."
        raise TypeError(msg)
    return cast(ConfigMap, payload)


def _with_hash_spdx_header(content: str) -> str:
    return f"{HASH_SPDX_HEADER}{content.lstrip()}"


def _as_config_map(value: object, label: str) -> ConfigMap:
    if not isinstance(value, dict):
        msg = f"{label} must be a mapping."
        raise TypeError(msg)
    return cast(ConfigMap, value)


def _deep_merge(base: ConfigMap, override: ConfigMap) -> ConfigMap:
    merged: ConfigMap = dict(base)
    for key, value in override.items():
        base_value = merged.get(key)
        if isinstance(base_value, dict) and isinstance(value, dict):
            merged[key] = _deep_merge(
                cast(ConfigMap, base_value),
                cast(ConfigMap, value),
            )
        else:
            merged[key] = value
    return merged


def _get(config: ConfigMap, path: str, default: object = "") -> object:
    current: object = config
    for segment in path.split("."):
        if not isinstance(current, dict):
            return default
        mapping = _as_config_map(cast(object, current), path)
        if segment not in mapping:
            return default
        current = mapping[segment]
    return current


def _string_list(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        items = cast(list[object], value)
        return [str(item).strip() for item in items if str(item).strip()]
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


def _truthy_string(value: object) -> str:
    return str(value).strip()


def _bool_setting(config: dict[str, Any], path: str, *, default: bool) -> bool:
    return bool(_get(config, path, default))


def _int_setting(config: dict[str, Any], path: str, default: int) -> int:
    value = _get(config, path, default)
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        return int(value)
    return default


def _python_version(config: dict[str, Any]) -> str:
    return _truthy_string(_get(config, "style.python_version", "3.13")) or "3.13"


def _line_length(config: dict[str, Any]) -> int:
    return _int_setting(config, "style.line_length", 88)


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
    explicit_repo_config: object = "",
    *,
    base_config: object = "",
) -> Path:
    """Resolve the optional consumer override config path for a repo root."""
    if isinstance(explicit_repo_config, Path):
        return explicit_repo_config.expanduser().resolve()

    resolved_base: ConfigMap = (
        cast(ConfigMap, base_config)
        if isinstance(base_config, dict)
        else _load_yaml(_ethos_root() / "config.yaml")
    )
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
    return repo_root / candidate_names[0]


def load_enforcement_config(
    repo_root: Path,
    repo_config_path: object = "",
) -> tuple[dict[str, Any], Path]:
    """Load merged enforcement policy for one repo root.

    Args:
        repo_root: Consumer repo where generated configs will be written.
        repo_config_path: Optional explicit override YAML path.

    Returns:
        The merged config mapping and the resolved repo override path, if any.

    """
    base_config = _load_yaml(_ethos_root() / "config.yaml")
    resolved_repo_config = resolve_repo_config(
        repo_root,
        repo_config_path,
        base_config=base_config,
    )
    if not resolved_repo_config.exists():
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
    if not _bool_setting(config, "python.sql_centralization.enabled", default=False):
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
    configured: object = _get(config, "tooling.ruff.extra_per_file_ignores", {}) or {}
    if configured and not isinstance(configured, dict):
        msg = "tooling.ruff.extra_per_file_ignores must be a mapping."
        raise TypeError(msg)
    configured_map = cast(dict[object, object], configured)

    ignores: dict[str, list[str]] = {}
    for pattern in _path_patterns(_stub_paths(config)):
        ignores[pattern] = list(stub_codes)
    for pattern in _path_patterns(_test_paths(config)):
        ignores[pattern] = list(test_codes)
    ignores.update(_sql_ignore_patterns(config))

    for raw_pattern, codes in configured_map.items():
        ignores[str(raw_pattern)] = _string_list(codes)
    return {pattern: codes for pattern, codes in ignores.items() if codes}


def render_pyrightconfig(config: dict[str, Any]) -> str:
    """Render `pyrightconfig.json` from merged policy."""
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
    """Render `mypy.ini` from merged policy."""
    parser = configparser.ConfigParser()
    parser["mypy"] = {
        "strict": "True"
        if _bool_setting(config, "tooling.mypy.strict", default=True)
        else "False",
        "warn_unused_configs": "True"
        if _bool_setting(config, "tooling.mypy.warn_unused_configs", default=True)
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
    return _with_hash_spdx_header("\n".join(lines).rstrip() + "\n")


def render_ruff_toml(config: dict[str, Any]) -> str:
    """Render `ruff.toml` from merged policy."""
    select_codes = _string_list(_get(config, "tooling.ruff.select", ["ALL"]))
    ignore_codes = _string_list(_get(config, "tooling.ruff.ignore", []))
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
            f"select = {_toml_list(select_codes)}",
            f"ignore = {_toml_list(ignore_codes)}",
            "",
            "[lint.pylint]",
            f"max-args = {_int_setting(config, 'tooling.ruff.max_args', 6)}",
        ]
    )

    per_file_ignores = _ruff_per_file_ignores(config)
    if per_file_ignores:
        lines.extend(["", "[lint.per-file-ignores]"])
        lines.extend(
            f"{_toml_string(pattern)} = {_toml_list(per_file_ignores[pattern])}"
            for pattern in sorted(per_file_ignores)
        )

    banned_api: object = _get(config, "tooling.ruff.banned_api", {}) or {}
    if banned_api and not isinstance(banned_api, dict):
        msg = "tooling.ruff.banned_api must be a mapping."
        raise TypeError(msg)
    banned_api_map = cast(dict[object, object], banned_api)
    if banned_api_map:
        lines.extend(["", "[lint.flake8-tidy-imports.banned-api]"])
        for raw_module_name in sorted(banned_api_map, key=str):
            module_name = str(raw_module_name)
            message = _truthy_string(banned_api_map[raw_module_name])
            if message:
                lines.append(
                    f"{_toml_string(module_name)} = {{ msg = {_toml_string(message)} }}"
                )

    return _with_hash_spdx_header("\n".join(lines).rstrip() + "\n")


def render_pylintrc(config: dict[str, Any]) -> str:
    """Render `.pylintrc` from merged policy."""
    parser = configparser.ConfigParser()
    parser["MAIN"] = {
        "jobs": str(_int_setting(config, "tooling.pylint.jobs", 0)),
        "persistent": _pylint_bool(
            value=_bool_setting(config, "tooling.pylint.persistent", default=False)
        ),
    }

    ignore = _string_list(_get(config, "tooling.pylint.ignore", []))
    if ignore:
        parser["MAIN"]["ignore"] = ",".join(ignore)

    ignore_paths = _string_list(_get(config, "tooling.pylint.ignore_paths", []))
    if ignore_paths:
        parser["MAIN"]["ignore-paths"] = ",".join(ignore_paths)

    parser["MESSAGES CONTROL"] = {
        "disable": ",".join(_string_list(_get(config, "tooling.pylint.disable", [])))
    }
    parser["REPORTS"] = {
        "reports": _pylint_bool(
            value=_bool_setting(config, "tooling.pylint.reports", default=False)
        ),
        "score": _pylint_bool(
            value=_bool_setting(config, "tooling.pylint.score", default=False)
        ),
    }
    parser["FORMAT"] = {
        "max-line-length": str(
            _int_setting(config, "tooling.pylint.max_line_length", _line_length(config))
        )
    }
    parser["DESIGN"] = {
        "max-args": str(_int_setting(config, "tooling.pylint.max_args", 6))
    }

    good_names = _string_list(_get(config, "tooling.pylint.good_names", []))
    if good_names:
        parser["BASIC"] = {"good-names": ",".join(good_names)}

    lines: list[str] = []
    for section in parser.sections():
        lines.append(f"[{section}]")
        for key, value in parser[section].items():
            lines.append(f"{key} = {value}")
        lines.append("")
    return _with_hash_spdx_header("\n".join(lines).rstrip() + "\n")


def _pylint_bool(*, value: bool) -> str:
    return "yes" if value else "no"


def render_yamllint_config(config: dict[str, Any]) -> str:
    """Render `.yamllint.yml` from merged policy."""
    payload: dict[str, Any] = {
        "extends": _truthy_string(_get(config, "tooling.yamllint.extends", "default"))
        or "default",
        "rules": _get(config, "tooling.yamllint.rules", {}),
    }
    if not isinstance(payload["rules"], dict):
        msg = "tooling.yamllint.rules must be a mapping."
        raise TypeError(msg)

    rules = cast(dict[str, Any], payload["rules"]).copy()
    line_length = cast(dict[str, Any], rules.get("line-length", {})).copy()
    line_length["max"] = _line_length(config)
    rules["line-length"] = line_length
    payload["rules"] = rules

    return _with_hash_spdx_header(render_yaml(payload))


def render_golangci_config(config: dict[str, Any]) -> str:
    """Render `.golangci.yml` from merged policy."""
    configured = _get(config, "tooling.golangci_lint", {})
    if not isinstance(configured, dict):
        msg = "tooling.golangci_lint must be a mapping."
        raise TypeError(msg)
    payload = _deep_merge({}, cast(ConfigMap, configured))

    payload["version"] = str(payload.get("version", "2"))

    linters = payload.get("linters", {})
    if not isinstance(linters, dict):
        msg = "tooling.golangci_lint.linters must be a mapping."
        raise TypeError(msg)
    linters_map = cast(ConfigMap, linters)

    settings = linters_map.get("settings", {})
    if not isinstance(settings, dict):
        msg = "tooling.golangci_lint.linters.settings must be a mapping."
        raise TypeError(msg)
    settings_map = cast(ConfigMap, settings)

    lll = settings_map.get("lll", {})
    if not isinstance(lll, dict):
        msg = "tooling.golangci_lint.linters.settings.lll must be a mapping."
        raise TypeError(msg)
    lll_map = cast(ConfigMap, lll)
    lll_map["line-length"] = _line_length(config)
    settings_map["lll"] = lll_map
    linters_map["settings"] = settings_map
    payload["linters"] = linters_map
    return _with_hash_spdx_header(render_yaml(payload))


def render_tool_configs(config: dict[str, Any]) -> dict[str, str]:
    """Render all supported repo-root tool config files."""
    return {
        "pyrightconfig.json": render_pyrightconfig(config),
        "mypy.ini": render_mypy_ini(config),
        "ruff.toml": render_ruff_toml(config),
        ".pylintrc": render_pylintrc(config),
        ".yamllint.yml": render_yamllint_config(config),
        ".golangci.yml": render_golangci_config(config),
    }


def sync_tool_configs(repo_root: Path, repo_config_path: object = "") -> list[Path]:
    """Write the generated tool configs into a repo root."""
    config, _ = load_enforcement_config(repo_root, repo_config_path)
    rendered = render_tool_configs(config)
    written: list[Path] = []
    for relative_path, content in rendered.items():
        absolute_path = repo_root / relative_path
        absolute_path.write_text(content, encoding="utf-8")
        written.append(absolute_path)
    return written


def check_tool_configs(repo_root: Path, repo_config_path: object = "") -> list[Path]:
    """Return generated tool config paths that are missing or out of sync."""
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
