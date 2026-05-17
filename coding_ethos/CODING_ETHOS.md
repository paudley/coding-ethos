<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# coding_ethos

`coding_ethos/` contains the Python package for rendering structured ethos data
into root agent-facing files.

The package is organized around a thin CLI layer, validated YAML loaders,
deterministic Markdown renderers, and managed merge helpers. Generated tool
configs, generated CI, Gemini prompt packs, and provider skill surfaces live in
Go under `go/internal/...` and are invoked through `go/cmd/coding-ethos-policy`.

Public imports should come from `coding_ethos.__init__` where possible so tests
and external callers stay on the supported package API instead of reaching into
internal modules.

## Module Boundaries

- `cli.py`: parses arguments, resolves paths, coordinates top-level workflows,
  and returns process-style exit codes.
- `loaders.py`: validates primary ethos YAML, applies repo overlays, and
  normalizes data into typed models.
- `models.py`: defines the dataclasses shared by loaders and renderers.
- `renderers.py`: renders deterministic Markdown for root agent docs, detail
  docs, memory files, and prompt addons.
- `markdown_seed.py`: converts an existing Markdown ethos into the structured
  `version: 2` YAML shape.
- `merging.py`: preserves existing root agent files through managed blocks or
  external LLM merge commands.
- `yaml_utils.py`: provides YAML formatting helpers that preserve comments and
  fold long prose.

## Public API

The supported package exports are:

- `main`
- `load_primary_bundle`
- `parse_ethos_markdown`
- `seed_primary_from_markdown`
- `render_yaml`
- `format_yaml_file`

New exports should be intentional, documented here, and covered by tests.

## Development Rules

Keep `cli.py` as orchestration only. Validation belongs in `loaders.py`, output
formatting belongs in `renderers.py`, merge behavior belongs in `merging.py`,
and generated enforcement artifacts belong in Go packages under `go/internal/`.

When a change affects flags, output layout, overlay behavior, generated config
content, skill surfaces, or prompt-pack content, update README guidance and the
relevant tests in the same change.
