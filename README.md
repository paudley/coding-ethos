<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos

Generate consistent `ETHOS.md`, `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, agent
detail files, repo-root tool configs, and hook prompt packs from one structured
engineering ethos.

`coding-ethos` is for repositories that need AI-facing instructions and local
quality gates to share the same contract. The shared ethos lives in
`coding_ethos.yml`, repo-specific guidance can be layered through
`repo_ethos.yml`, and enforcement settings can be layered through
`repo_config.yaml`.

## What it does

- Renders root agent documents for Codex, Claude, and Gemini.
- Renders per-principle deep reference docs under `.agents/ethos/`.
- Renders Claude memory and lightweight prompt-addon files for fallback agent
  contexts.
- Preserves existing root agent files with managed injection blocks or an
  explicit LLM merge strategy.
- Syncs generated repo-root tool configs for Pyright, mypy, Ruff, yamllint,
  and golangci-lint.
- Syncs the grounded Gemini prompt pack consumed by the bundled Go hook runner.
- Provides a bundled Go pre-commit and pre-push enforcement package.
- Seeds structured YAML from an existing Markdown ethos.

## Repository model

The project has four related surfaces:

- Agent docs: `coding_ethos.yml` plus optional `repo_ethos.yml` render
  `ETHOS.md`, root agent docs, `.agents/ethos/`, `.claude/ethos/`, and
  `.agent-context/`.
- Enforcement config: `config.yaml` plus optional `repo_config.yaml` render
  Pyright, mypy, Ruff, yamllint, and golangci-lint config files.
- Gemini hook prompts: ethos YAML, repo overlays, enforcement config, and
  `pre-commit/prompts/` templates render
  `.code-ethos/gemini/prompt-pack.json`.
- Hook runtime: `pre-commit/`, generated configs, and the generated prompt pack
  run through repo-local Go hook shims and Go-backed policy checks.

Generated Markdown files are derived artifacts. Change the YAML source or
renderer first, then regenerate and review the generated diff.

## Quick start

Install dependencies and generated local artifacts:

```bash
make install
```

Run the Python test suite:

```bash
make test
```

Run the current verification gate:

```bash
make check
```

Check the local toolchain and resolved hook paths:

```bash
make doctor
```

Generate this repo's checked-in agent files:

```bash
make generate
```

Generate agent files into another repository:

```bash
make generate REPO=/path/to/repo
```

Preserve existing root agent files in a target repo:

```bash
make generate-merge REPO=/path/to/repo
```

## Direct CLI usage

The package exposes the `coding-ethos` command. During local development, the
Makefile runs it through `uv run python main.py` so the repo-local sources are
used.

Generate agent docs:

```bash
uv run coding-ethos --repo /path/to/repo --primary coding_ethos.yml
```

Seed a primary YAML file from Markdown:

```bash
uv run coding-ethos \
  --primary coding_ethos.yml \
  --seed-from-markdown /path/to/ETHOS.md
```

Sync generated tool configs:

```bash
uv run coding-ethos --repo /path/to/repo --sync-tool-configs
```

Check generated tool configs for drift:

```bash
uv run coding-ethos --repo /path/to/repo --check-tool-configs
```

Sync the Gemini hook prompt pack:

```bash
uv run coding-ethos \
  --repo /path/to/repo \
  --primary coding_ethos.yml \
  --sync-gemini-prompts
```

## Make targets

The Makefile is the preferred operator interface.

- `make install`: sync dev dependencies, generated tool configs, and Gemini
  prompt pack.
- `make install-runtime`: sync runtime dependencies and generated local
  artifacts.
- `make status`: print resolved paths and generation settings.
- `make doctor`: check required local tools and important resolved paths.
- `make test`: run `uv run pytest`.
- `make check`: run tests plus generated config and prompt-pack drift checks.
- `make validate`: validate the bundled Go hook runtime.
- `make go-test`: run tests for the Go hook runner.
- `make go-fmt`: format all Go hook helper source files.
- `make go-tidy`: format Go hook helper sources and run `go mod tidy`.
- `make fmt`: run repo-owned source formatters currently exposed by Make.
- `make install-hooks`: install repo-local Git hook shims.
- `make pre-commit`: run staged-file pre-commit hooks.
- `make pre-commit-all`: run pre-commit hooks over all files.
- `make pre-push`: run pre-push hooks.
- `make sync-tool-configs`: write generated repo-root tool configs.
- `make check-tool-configs`: fail if generated repo-root tool configs drift.
- `make sync-gemini-prompts`: write `.code-ethos/gemini/prompt-pack.json`.
- `make check-gemini-prompts`: fail if the prompt pack drifts.
- `make seed`: seed or refresh `PRIMARY` from `SEED_FROM`.
- `make generate`: generate agent-facing files into `REPO`.
- `make generate-merge`: generate while preserving existing root agent docs.
- `make generate-merge-llm`: use an external agent CLI for root-file merges.

Useful overrides:

```bash
make generate REPO=/path/to/repo PRIMARY=/path/to/coding_ethos.yml
make generate REPO=/path/to/repo REPO_ETHOS=/path/to/repo_ethos.yml
make sync-tool-configs \
  TOOL_CONFIG_REPO=/path/to/repo \
  REPO_CONFIG=/path/to/repo_config.yaml
make go-test GO=/path/to/go
make go-fmt GOFMT=/path/to/gofmt
make seed SEED_FROM=/path/to/ETHOS.md PRIMARY=/path/to/coding_ethos.yml
```

## Source files

### `coding_ethos.yml`

The primary ethos YAML is the shared source contract. It must use
`version: 2`, include metadata, and define a non-empty ordered list of
principles. Each principle needs an `id`, `order`, `title`, `directive`, and at
least one section or inline body.

Supported section kinds are:

```text
overview, guidance, rule, policy, workflow, anti_patterns, correct_way,
rationale, examples, reference, repo_context
```

The loader validates duplicate ids, duplicate orders, unknown related ids,
unsupported agents, malformed sections, and empty required prose.

Primary file aliases are also accepted when `--primary` is omitted:

- `coding_ethos.yml`
- `coding_ethos.yaml`
- `code_ethos.yml`
- `code_ethos.yaml`

### `repo_ethos.yml`

The optional repo overlay adds local commands, paths, notes, per-agent notes,
principle overrides, and additional repo-specific principles. By default the CLI
looks for `repo_ethos.yml` or `repo_ethos.yaml` inside the target repo.

Overlay capabilities:

- `repo.name`, `repo.overview`, `repo.commands`, `repo.paths`, and `repo.notes`
- `agent_notes.codex`, `agent_notes.claude`, and `agent_notes.gemini`
- `principles.overrides.<id>` for summary, directive, tags, related ids,
  quick refs, merge topics, agent hints, prepend text, and append text
- `principles.additional` for new repo-local principles

See [repo_ethos.example.yml](repo_ethos.example.yml).

### `config.yaml` and `repo_config.yaml`

`config.yaml` is the bundle-wide enforcement source of truth. A consuming repo
can refine it with `repo_config.yaml` or `repo_config.yml` at the repo root, or
by passing `--repo-config`.

The merged config drives:

- generated Pyright, mypy, Ruff, yamllint, and golangci-lint config files
- hook policy for Python, shell, text, commit-message, and Go checks
- Gemini AI review runtime settings and prompt grounding
- shared style settings such as `style.python_version` and `style.line_length`

See [repo_config.example.yaml](repo_config.example.yaml).

## Output layout

Generated agent output in a target repo looks like this:

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

Generated enforcement output in a target repo can include:

```text
repo/
├── pyrightconfig.json
├── mypy.ini
├── ruff.toml
├── .yamllint.yml
├── .golangci.yml
└── .code-ethos/
    └── gemini/
        └── prompt-pack.json
```

## Merge behavior

`--merge-existing` only preserves root agent files:

- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`

`ETHOS.md` and supporting generated files are replaced with deterministic
output.

### Inject merge

Inject merge is the default strategy:

```bash
uv run coding-ethos --repo /path/to/repo --merge-existing
```

It inserts managed import blocks and managed addendum blocks into existing root
files. Re-running is idempotent, and locally authored content outside managed
blocks is preserved.

### LLM merge

LLM merge asks an installed agent CLI to merge `existing.md` and `generated.md`
inside an isolated temporary workspace:

```bash
uv run coding-ethos \
  --repo /path/to/repo \
  --merge-existing \
  --merge-strategy llm \
  --merge-engine gemini \
  --merge-bin /path/to/gemini \
  --merge-timeout-seconds 300
```

Supported merge engines are `codex`, `gemini`, and `claude`. The selected CLI
must already be installed and authenticated. The merge process must write
`merged.md`; otherwise the command fails.

## Hook bundle

The bundled ETHOS enforcement package lives under [pre-commit/](pre-commit/).
It uses repo-local Git hook shims that call the Go runner under
`pre-commit/hooks/go-hooks/`.

The Go hook runner owns hook output policy. Failure reports honor
`hooks.output_format` (`auto`, `human`, `json`, or `toon`), with `auto`
selecting TOON when known agent/LLM environment markers are present. Successful
groups are silent by default through `hooks.success_output: silent`; set it to
`verbose` only when operator-facing pass summaries are useful. Enabled hook
groups run in parallel when `hooks.parallel_groups: true`, with group output
captured and replayed deterministically on failure. Failed hook runs print a
runner-owned summary with group status, duration, failed groups, and command
timing where the runner has in-process command visibility.

The agent hook path is local-only: `pre-commit/hooks/run-go-hook.sh agent-hook`
compiles a policy bundle under `.git/coding-ethos-hooks/policy/` and runs the
new Go policy runtime. Gemini review checks remain pre-commit/pre-push checks;
they are not invoked from agent hooks. Agent hook evaluators are runtime-covered,
but Claude hook installation and cutover are still tracked in
[HOOK_REPLACEMENT_PLAN.md](HOOK_REPLACEMENT_PLAN.md).

Installed Git hook shims also compile the policy bundle and run executable
compiled-policy preflight before delegating to the current Go hook group runner.
This keeps compiled policy in the active Git hook path while the remaining hook
groups are moved over.

Install hooks:

```bash
make install-hooks
```

Run hooks:

```bash
make pre-commit
make pre-commit-all
make pre-push
make hook-plan
```

See [pre-commit/PRE-COMMIT.md](pre-commit/PRE-COMMIT.md) and
[pre-commit/hooks/HOOKS.md](pre-commit/hooks/HOOKS.md).

## Development notes

The CLI stays thin. Behavior belongs in focused modules:

- `coding_ethos/loaders.py` validates and merges ethos YAML.
- `coding_ethos/renderers.py` renders deterministic Markdown.
- `coding_ethos/merging.py` owns managed-block injection and external merge
  orchestration.
- `coding_ethos/tool_configs.py` renders generated repo-root tool configs.
- `coding_ethos/gemini_prompt_pack.py` renders hook prompt packs from templates.
- `pre-commit/hooks/go-hooks/` owns active hook runtime and policy checks.

When flags, output layout, merge behavior, overlay semantics, or enforcement
config behavior change, update this README, the relevant example YAML, and the
tests in the same change.

## Verification

Canonical local verification:

```bash
make check
```

Broader verification for hook work:

```bash
make validate
make go-test
make go-tools-test
make go-tools-smoke
make pre-commit-all
```

Run `make generate` after changing `coding_ethos.yml`, `repo_ethos.yml`, or
renderer behavior. Run `make sync-tool-configs` after changing generated
tool-config behavior. Run `make sync-gemini-prompts` after changing prompt
templates, ethos grounding, or Gemini prompt-pack behavior.
