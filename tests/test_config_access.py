# SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Tests for shared config access helpers.

The config access module centralizes dotted-path lookup and typed
normalization for generated configuration surfaces. These tests protect the
shared behavior so Go migration work can remove duplicated Python helpers
without weakening remaining Python entrypoints.
"""

import pytest

from coding_ethos import config_access


def test_as_config_map_rejects_non_mappings() -> None:
    with pytest.raises(TypeError, match="repo_config must be a mapping"):
        config_access.as_config_map(["not", "a", "mapping"], "repo_config")


def test_deep_merge_recurses_without_mutating_inputs() -> None:
    base: config_access.ConfigMap = {
        "tooling": {"ruff": {"select": ["ALL"], "line_length": 88}},
        "keep": "base",
    }
    override: config_access.ConfigMap = {
        "tooling": {"ruff": {"line_length": 100}},
        "new": "override",
    }

    merged = config_access.deep_merge(base, override)

    assert merged == {
        "tooling": {"ruff": {"select": ["ALL"], "line_length": 100}},
        "keep": "base",
        "new": "override",
    }
    assert base["tooling"] == {"ruff": {"select": ["ALL"], "line_length": 88}}


def test_get_path_returns_fallback_for_missing_or_non_mapping_segments() -> None:
    config: config_access.ConfigMap = {"a": {"b": "value"}, "plain": "text"}

    assert config_access.get_path(config, "a.b", "fallback") == "value"
    assert config_access.get_path(config, "a.missing", "fallback") == "fallback"
    assert config_access.get_path(config, "plain.value", "fallback") == "fallback"


def test_string_and_list_helpers_normalize_config_values() -> None:
    config: config_access.ConfigMap = {
        "values": [" src ", "", 42],
        "blank": " ",
        "name": " service ",
    }

    assert config_access.string_list(None) == []
    assert config_access.string_list(config["values"]) == ["src", "42"]
    assert config_access.string_list(" tests ") == ["tests"]
    assert config_access.configured_list(config, "values", ["fallback"]) == [
        "src",
        "42",
    ]
    assert config_access.configured_list(config, "missing", ["fallback"]) == [
        "fallback",
    ]
    assert config_access.truthy_string(config["name"]) == "service"
    assert config_access.configured_string(config, "blank", "fallback") == "fallback"
    assert config_access.configured_string(config, "name", "fallback") == "service"


def test_configured_choice_reports_allowed_values() -> None:
    config: config_access.ConfigMap = {"mode": "required"}

    assert (
        config_access.configured_choice(
            config,
            "mode",
            "auto",
            {"auto", "required"},
        )
        == "required"
    )
    with pytest.raises(ValueError, match="Must be one of: auto, required"):
        config_access.configured_choice(
            {"mode": "off"},
            "mode",
            "auto",
            {"auto", "required"},
        )


def test_bool_and_int_helpers_parse_common_forms() -> None:
    config: config_access.ConfigMap = {
        "enabled": "yes",
        "disabled": "no",
        "count": "12",
        "bad_count": "x",
        "truthy_object": [1],
    }

    assert config_access.configured_bool(config, "enabled", fallback=False) is True
    assert config_access.configured_bool(config, "disabled", fallback=True) is False
    assert config_access.configured_bool(config, "missing", fallback=True) is True
    assert (
        config_access.configured_bool(config, "truthy_object", fallback=False) is True
    )
    assert config_access.configured_int(config, "count", 0) == 12
    assert config_access.configured_int(config, "bad_count", 7) == 7
    assert config_access.configured_int(config, "missing", 3) == 3
