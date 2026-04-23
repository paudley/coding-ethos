"""Shared configuration helpers for coding-ethos hook scripts."""

from __future__ import annotations

import os
import subprocess
from functools import lru_cache
from pathlib import Path
from typing import Any

import yaml

_CONFIG_ENV = "CODE_ETHOS_PRECOMMIT_CONFIG"
_DEFAULT_OVERRIDE_CANDIDATES = (
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


def _deep_merge(base: Any, override: Any) -> Any:
    """Recursively merge dictionaries and replace other values."""

    if isinstance(base, dict) and isinstance(override, dict):
        merged = dict(base)
        for key, value in override.items():
            if key in merged:
                merged[key] = _deep_merge(merged[key], value)
            else:
                merged[key] = value
        return merged
    return override


def _load_yaml(path: Path) -> dict[str, Any]:
    payload = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(payload, dict):
        msg = f"Expected a mapping in {path}"
        raise ValueError(msg)
    return payload


def _script_bundle_root() -> Path:
    return Path(__file__).resolve().parent.parent


def _git_output(*args: str) -> str:
    """Run a git command and return stripped stdout or an empty string."""

    try:
        result = subprocess.run(
            list(args),
            check=True,
            capture_output=True,
            text=True,
        )
    except (OSError, subprocess.SubprocessError):
        return ""
    return result.stdout.strip()


def _is_bundle_root(path: Path) -> bool:
    return (path / "hooks").is_dir() and (path / "lefthook.yml").is_file()


def bundle_root() -> Path:
    """Return the active hook bundle root."""

    env_root = os.environ.get("CODE_ETHOS_PRECOMMIT_ROOT", "").strip()
    if env_root:
        candidate = Path(env_root).expanduser().resolve()
        if _is_bundle_root(candidate):
            return candidate

    script_root = _script_bundle_root()
    if _is_bundle_root(script_root):
        return script_root

    return script_root


def ethos_root() -> Path:
    """Return the coding-ethos repository root."""

    return bundle_root().parent


def consumer_root() -> Path:
    """Return the consuming repository root for override discovery."""

    ethos = ethos_root()
    super_root = _git_output(
        "git",
        "-C",
        str(ethos),
        "rev-parse",
        "--show-superproject-working-tree",
    )
    if super_root:
        return Path(super_root).resolve()

    repo = _git_output("git", "-C", str(ethos), "rev-parse", "--show-toplevel")
    if repo:
        return Path(repo).resolve()

    return ethos


def _override_candidates(config: dict[str, Any]) -> list[Path]:
    root = consumer_root()
    bundle = config.get("bundle", {})
    raw_names = (
        bundle.get("consumer_override_candidates", _DEFAULT_OVERRIDE_CANDIDATES)
        if isinstance(bundle, dict)
        else _DEFAULT_OVERRIDE_CANDIDATES
    )
    names = [str(item).strip() for item in raw_names if str(item).strip()]
    return [root / name for name in names]


@lru_cache
def load_config() -> dict[str, Any]:
    """Load the default hook config with an optional root-level override."""

    base_path = ethos_root() / "config.yaml"
    config = _load_yaml(base_path)

    env_path = os.environ.get(_CONFIG_ENV, "").strip()
    if env_path:
        return _deep_merge(config, _load_yaml(Path(env_path).expanduser().resolve()))

    for candidate in _override_candidates(config):
        if candidate.exists():
            return _deep_merge(config, _load_yaml(candidate))

    return config


def get(path: str, default: Any = None) -> Any:
    """Read a dotted config path."""

    current: Any = load_config()
    for segment in path.split("."):
        if not isinstance(current, dict) or segment not in current:
            return default
        current = current[segment]
    return current


def get_bool(path: str, default: bool = False) -> bool:
    value = get(path, default)
    return bool(value)


def get_list(path: str, default: list[Any] | None = None) -> list[Any]:
    value = get(path, default if default is not None else [])
    if isinstance(value, list):
        return value
    if value is None:
        return []
    return [value]


def get_str(path: str, default: str = "") -> str:
    value = get(path, default)
    return str(value)
