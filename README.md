<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos

Generate consistent `ETHOS.md`, `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, and supporting detail files from a single shared engineering ethos.

`coding-ethos` is for teams that want one source of truth for AI-facing repo guidance instead of maintaining parallel prompt files by hand. You define the shared contract once in `coding_ethos.yml`, optionally layer repo-specific context on top, and render agent-specific outputs into any repository.

## Why this exists

Most repos drift into duplicated AI instructions:

- `AGENTS.md` says one thing.
- `CLAUDE.md` says something slightly different.
- `GEMINI.md` lags behind both.
- Deep rationale lives somewhere else, if it exists at all.

This project fixes that by treating the ethos as structured source data instead of prose you copy around manually.

## What it generates

| File | Purpose |
| --- | --- |
| `ETHOS.md` | Full human-readable shared ethos document |
| `AGENTS.md` | Codex-focused root guidance |
| `CLAUDE.md` | Claude-specific root file with imports |
| `.claude/ethos/MEMORY.md` | Claude-style memory index into detailed notes |
| `GEMINI.md` | Gemini-focused root guidance |
| `.agents/ethos/*.md` | Per-principle deep reference docs |
| `.agent-context/prompt-addons/*.md` | Small fallback prompt addons per supported agent |

## Features

- One structured source of truth in `coding_ethos.yml`
- Optional repo-specific overlay from `repo_ethos.yml`
- Bundled ETHOS enforcement package under `pre-commit/` with repo-local Lefthook install and Go-backed policy hooks
- Markdown seeding from an existing ethos document
- Validation for ids, ordering, directives, related links, and section kinds
- Safe merge mode for existing `AGENTS.md`, `CLAUDE.md`, and `GEMINI.md`
- Optional LLM-assisted full-document merge for existing root files
- Per-principle detail docs so root files can stay concise

## Quick start

### 1. Install dependencies

```bash
uv sync
```

After syncing, you can invoke the tool either as `uv run python main.py` or `uv run coding-ethos`.

### 2. Create or seed the primary ethos

Seed from an existing markdown ethos:

```bash
uv run python main.py --seed-from-markdown /path/to/ETHOS.md
```

Or hand-author `coding_ethos.yml` directly.

### 3. Generate files into a target repo

```bash
uv run python main.py --repo /path/to/repo
```

If the target repo already has hand-written root files and you want to preserve them:

```bash
uv run python main.py --repo /path/to/repo --merge-existing
```

## Typical workflow

```bash
# Seed once from an existing ethos document
uv run python main.py --seed-from-markdown /path/to/ETHOS.md

# Review and edit the generated structured YAML
$EDITOR coding_ethos.yml

# Generate agent-facing files into a repo
uv run python main.py --repo /path/to/repo

# Re-run whenever the ethos changes
uv run python main.py --repo /path/to/repo --merge-existing
```

## Output layout

Generated output in a target repo looks like this:

```text
repo/
├── AGENTS.md
├── CLAUDE.md
├── ETHOS.md
├── GEMINI.md
├── .agent-context/
│   └── prompt-addons/
│       ├── claude.md
│       ├── codex.md
│       └── gemini.md
├── .agents/
│   └── ethos/
│       ├── README.md
│       ├── solid-is-law.md
│       └── ...
└── .claude/
    └── ethos/
        └── MEMORY.md
```

## Merge behavior

`coding-ethos` has two strategies for existing root files.

### Inject mode

Use `--merge-existing` by itself.

- Preserves existing `AGENTS.md`, `CLAUDE.md`, and `GEMINI.md`
- Injects managed import and additive guidance blocks
- Re-running is idempotent
- Replaces `ETHOS.md` with the newly generated full ethos document

This is the default and safest mode.

### LLM merge mode

Use `--merge-existing --merge-strategy llm`.

```bash
uv run python main.py \
  --repo /path/to/repo \
  --merge-existing \
  --merge-strategy llm \
  --merge-engine gemini \
  --merge-bin /path/to/gemini
```

This mode:

- merges existing and generated root files in an isolated temporary workspace
- preserves repo-specific operational content when possible
- still writes supporting generated files directly
- requires the selected CLI (`codex`, `gemini`, or `claude`) to already be installed and authenticated

## Configuration

### `coding_ethos.yml`

This is the primary source of truth.

```yaml
version: 2
metadata:
  title: Example Ethos
  overview: Shared engineering contract.
  source_markdown: ETHOS.md

agents:
  codex:
    root_file: AGENTS.md
    notes:
      - Keep the root file concise and operational.
  claude:
    root_file: CLAUDE.md
    supporting_files:
      - .claude/ethos/MEMORY.md
  gemini:
    root_file: GEMINI.md

principles:
  - id: solid-is-law
    order: 1
    title: SOLID is Law
    summary: Structure wins over convenience.
    directive: Enforce simple SOLID designs.
    quick_ref:
      - Favor simple, explicit designs.
      - Remove speculative abstractions before adding new layers.
      - Define interfaces before binding implementations.
    merge_topics:
      - architecture decisions
      - design constraints
      - abstraction boundaries
    tags:
      - architecture
      - design
    related:
      - protocol-first-design
    agent_hints:
      codex: Prefer structural refactors over tactical patches.
      claude: Open the detailed ethos note before reviewing architecture changes.
      gemini: Keep plans coherent and resist ad hoc abstractions.
    sections:
      - id: overview
        kind: overview
        title: Overview
        summary: Structure wins over convenience.
        body: |
          We do not view the SOLID principles as academic suggestions.
      - id: simplicity-precepts
        kind: rule
        title: Simplicity Precepts
        summary: Prefer simpler designs over speculative abstractions.
        body: |
          Prefer simpler designs over speculative abstractions.
```

Notes:

- `source_markdown` is provenance only
- principle content stays inline in YAML
- section `kind` is validated
- aliases for the primary file are also supported: `coding_ethos.yaml`, `code_ethos.yml`, `code_ethos.yaml`

### `repo_ethos.yml`

This overlay is optional and defaults to `<repo>/repo_ethos.yml`.

Use it to add repo-specific commands, paths, notes, principle overrides, or extra principles without forking the shared ethos.

See [repo_ethos.example.yml](repo_ethos.example.yml) for a ready-to-copy template.

Example:

```yaml
repo:
  name: Example Service
  overview: Background processing for durable jobs.
  commands:
    install:
      - uv sync
    test:
      - uv run pytest
    lint:
      - uv run ruff check .
  paths:
    source: src/
    tests: tests/
  notes:
    - Job IDs are immutable.

agent_notes:
  codex:
    - Prefer the generated detail docs when a task is architectural.
  claude:
    - Open the relevant ethos doc before changing retry behavior.
  gemini:
    - Prefer targeted reads when the task is narrow.

principles:
  overrides:
    protocol-first-design:
      directive: Define interfaces in `src/interfaces/` before implementation work.
      quick_ref:
        - Start interface work in `src/interfaces/`.
        - Keep implementation modules downstream of the contract.
      merge_topics:
        - interface design
        - API contracts
      append: |
        In this repo, all interfaces live under `src/interfaces/`.
```

## Command reference

| Flag | Meaning |
| --- | --- |
| `--repo` | Target repository directory for generated files |
| `--primary` | Explicit path to the primary ethos YAML |
| `--repo-ethos` | Explicit path to the repo-specific overlay YAML |
| `--seed-from-markdown` | Seed or refresh the primary YAML from a markdown source |
| `--merge-existing` | Preserve existing root files and inject/merge generated guidance |
| `--merge-strategy` | `inject` or `llm` |
| `--merge-engine` | `codex`, `gemini`, or `claude` when using LLM merge mode |
| `--merge-bin` | Explicit CLI binary path for merge mode |
| `--merge-model` | Optional model override for merge mode |
| `--merge-timeout-seconds` | Per-file timeout for LLM merge mode |

## Project policies

- [Changelog](CHANGELOG.md)
- [Contributing guide](CONTRIBUTING.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security policy](SECURITY.md)

## Development

Install dependencies:

```bash
uv sync --group dev --all-packages
```

Repo-local convenience commands:

```bash
make help
make install
make test
make sync-tool-configs
make sync-gemini-prompts
make validate
make generate
```

The `Makefile` is a thin wrapper around the documented `uv` workflow above. The raw `uv` commands remain the portable interface for using `coding-ethos` outside this repository.

The repo also ships a bundled Lefthook-based pre-commit package in
`pre-commit/`. From the repo root you can validate or install it with:

```bash
make sync-tool-configs
make check-tool-configs
make sync-gemini-prompts
make check-gemini-prompts
make validate
make install-hooks
```

Backward-compatible aliases like `make hooks-validate` and
`make hooks-install` still work.

`make install-hooks` keeps Lefthook repo-local. It installs the pinned
binary into `.git/coding-ethos-hooks/` with `GOBIN=... go install` and uses
that cached binary for both manual runs and installed Git hook shims.

`make sync-tool-configs` generates repo-root `pyrightconfig.json`, `mypy.ini`,
`ruff.toml`, and `.yamllint.yml` from the shared [config.yaml](config.yaml)
plus an optional consuming-repo `repo_config.yaml`. The same
`style.python_version` value also drives the `pyupgrade` autofix target and
the hook-level version consistency checks for files like `.python-version`
and `pyproject.toml`. In other words, `config.yaml` is the enforcement source
of truth; `pre-commit/` is the execution bundle, not a second policy surface.

`make sync-gemini-prompts` generates
`.code-ethos/gemini/prompt-pack.json`, a grounded Gemini prompt pack derived
from `coding_ethos.yml`, optional `repo_ethos.yml`, and the merged
`config.yaml` plus `repo_config.yaml` policy model. The pack includes both
rendered prompt text and per-check runtime metadata such as file scope and
batch sizing. `install` and
`install-hooks` run both sync steps automatically, and the active Lefthook
Gemini job now consumes that pack through the Go hook runner. The Go runner
also honors `gemini.max_concurrent_api_calls`, repo-local response caching in
`.git/coding-ethos-hooks/gemini-cache/`, per-check `model_overrides` and
`service_tier_overrides`, explicit Gemini `cachedContents` reuse for batch
corpora that are reviewed by multiple prompts, and
`disable_safety_filters: true` for code-review requests.

The root `pyproject.toml` still includes `pre-commit/hooks` as a `uv` workspace
member for the hook toolchain environment, but Lefthook now reads Ruff,
mypy, pyright, and yamllint settings from the generated repo-root config files,
and Gemini review prefers the generated prompt pack over legacy hard-coded
prompt text. Most hook runtime and policy logic now lives in
`pre-commit/hooks/go-hooks/`; the remaining Python hook files are wrappers
around external analysis tools.

Run tests:

```bash
uv run pytest
```

## License

[MIT](LICENSE)
