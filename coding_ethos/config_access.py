# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Shared access helpers for nested coding-ethos config mappings.

Generated tool, CI, and prompt configuration code all read the same nested
policy mappings. This module centralizes dotted-path lookup, scalar/list
normalization, recursive merge behavior, and conservative bool/int parsing so
provider renderers do not drift. The helpers intentionally fail on malformed
mapping boundaries instead of silently accepting ambiguous config structure.
"""

from typing import Any, cast

ConfigMap = dict[str, Any]


def as_config_map(value: object, label: str) -> ConfigMap:
    """Return a typed config mapping or fail with a source-specific label."""
    if not isinstance(value, dict):
        msg = f"{label} must be a mapping."
        raise TypeError(msg)
    return cast(ConfigMap, value)


def deep_merge(base: ConfigMap, override: ConfigMap) -> ConfigMap:
    """Return `base` recursively merged with `override` values."""
    merged: ConfigMap = dict(base)
    for key, value in override.items():
        base_value = merged.get(key)
        if isinstance(base_value, dict) and isinstance(value, dict):
            merged[key] = deep_merge(
                cast(ConfigMap, base_value),
                cast(ConfigMap, value),
            )
        else:
            merged[key] = value
    return merged


def get_path(config: ConfigMap, path: str, fallback: object = "") -> object:
    """Return a dotted-path config value or `fallback` when absent."""
    current: object = config
    for segment in path.split("."):
        if not isinstance(current, dict):
            return fallback
        mapping = as_config_map(cast(object, current), path)
        if segment not in mapping:
            return fallback
        current = mapping[segment]
    return current


def string_list(value: object) -> list[str]:
    """Normalize a scalar or list value into trimmed non-empty strings."""
    if value is None:
        return []
    if isinstance(value, list):
        items = cast(list[object], value)
        return [str(item).strip() for item in items if str(item).strip()]
    stripped = str(value).strip()
    return [stripped] if stripped else []


def configured_list(config: ConfigMap, path: str, fallback: list[str]) -> list[str]:
    """Return a configured string list or a copy of `fallback`."""
    values = string_list(get_path(config, path, []))
    return values or list(fallback)


def truthy_string(value: object) -> str:
    """Return a stripped string representation for config values."""
    return str(value).strip()


def configured_string(config: ConfigMap, path: str, fallback: str) -> str:
    """Return a configured non-empty string or `fallback`."""
    configured = truthy_string(get_path(config, path, ""))
    return configured or fallback


def configured_choice(
    config: ConfigMap,
    path: str,
    fallback: str,
    choices: set[str],
) -> str:
    """Return a configured string constrained to an explicit allowed set."""
    configured = configured_string(config, path, fallback)
    if configured not in choices:
        allowed = ", ".join(sorted(choices))
        msg = f"Invalid {path}: {configured}. Must be one of: {allowed}."
        raise ValueError(msg)
    return configured


def configured_bool(config: ConfigMap, path: str, *, fallback: bool) -> bool:
    """Return a configured bool with common string truth values supported."""
    configured = get_path(config, path, fallback)
    if isinstance(configured, bool):
        return configured
    if isinstance(configured, str):
        return configured.strip().lower() in {"1", "true", "yes", "on"}
    return bool(configured)


def configured_int(config: ConfigMap, path: str, fallback: int) -> int:
    """Return a configured int or `fallback` when parsing fails."""
    configured = get_path(config, path, fallback)
    if isinstance(configured, int):
        return configured
    if isinstance(configured, str):
        try:
            return int(configured.strip())
        except ValueError:
            return fallback
    return fallback
