<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding_ethos

`coding_ethos/` contains the core Python package for rendering structured ethos
data into agent-facing files and derived enforcement artifacts.

The package is organized around a thin CLI layer, validated YAML loaders,
deterministic Markdown renderers, and generators for repo-root tool configs and
Gemini prompt packs.

Public imports should come from `coding_ethos.__init__` where possible so tests
and external callers stay on the supported package API instead of reaching into
internal modules.
