# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Packaged resource path helpers for PyPI-installed coding-ethos.

The source checkout keeps default contracts at the repository root. Built
packages place the same files under ``coding_ethos/resources``. This module
centralizes that lookup for the Python documentation generator.
"""

from pathlib import Path


def resource_path(*parts: str) -> Path:
    """Return a filesystem path to a source or packaged coding-ethos resource."""
    source_root = Path(__file__).resolve().parent.parent
    source_candidates: dict[tuple[str, ...], Path] = {
        ("coding_ethos.yml",): source_root / "coding_ethos.yml",
        ("config.yaml",): source_root / "config.yaml",
        ("repo_config.example.yaml",): source_root / "repo_config.example.yaml",
        ("repo_ethos.example.yml",): source_root / "repo_ethos.example.yml",
    }
    source_path = source_candidates.get(parts)
    if source_path is not None and source_path.exists():
        return source_path
    return Path(__file__).resolve().parent.joinpath("resources", *parts)
