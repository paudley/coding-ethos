<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos

`coding-ethos` turns an engineering ethos into runnable repository policy.

It keeps agent instructions, generated documentation, static-analysis config,
Git hooks, and agent tool-use guards on the same source contract. The result is
defense in depth for human and AI contributors: the repo tells agents what good
work looks like, gives developers the same standards in normal tool output, and
blocks critical bypasses before bad changes land.

## Why it exists

AI coding agents fail most dangerously when advice, tooling, and enforcement
disagree. A Markdown rule says one thing, a linter checks another thing, and a
hook lets a third thing through. `coding-ethos` closes that gap by compiling the
repo's working agreement into every place contributors interact with the code:

- **Agent context**: `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `ETHOS.md`, and
  deep per-principle reference docs.
- **Tool configuration**: Pyright, mypy, Ruff, yamllint, and golangci-lint
  config generated from the same policy inputs.
- **Git enforcement**: repo-local Git hooks backed by Go policy evaluators and
  deterministic failure output.
- **Agent tool-use enforcement**: Claude hook settings and runtime policy that
  can rewrite safe Git commands, block bypass attempts, capture continuation
  context, and preserve audit logs.
- **AI review grounding**: Gemini prompt packs generated from ethos, repo
  overlays, enforcement config, and checked-in prompt templates.

## Defense in depth

`coding-ethos` does not rely on one hook or one instruction file. Policy is
layered so failures are caught at multiple points:

1. **Source policy** lives in YAML: `coding_ethos.yml` for shared principles,
   `repo_ethos.yml` for repo-local context, `config.yaml` for enforcement, and
   optional `repo_config.yaml` for consumer overrides.
2. **Generated guidance** makes the same contract visible to agents and humans
   through root agent files and deep reference docs.
3. **Generated tool config** aligns standard linters and type checkers with the
   policy instead of relying on hand-maintained config drift.
4. **Git hooks** run compiled policy preflight, static checks, AI review checks,
   and repo-specific gates before commits and pushes.
5. **Agent hooks** protect high-risk tool paths during the work session, before
   a commit even exists.
6. **Audit output** is written under `.coding-ethos/` so noisy or expensive hook
   runs can be analyzed later without flooding the calling agent.

Unknown linter output still flows through normally. Findings tied to ETHOS
principles can receive stronger, policy-grounded advice instead of generic
tool messages.

## How policy flows

```text
coding_ethos.yml      repo_ethos.yml
       │                    │
       ├──── merged ethos ──┤
       │                    │
       ▼                    ▼
AGENTS.md / CLAUDE.md / GEMINI.md / ETHOS.md
.agents/ethos/ deep docs
.agent-context/ prompt addons

config.yaml          repo_config.yaml
       │                    │
       ├── merged enforcement config
       │
       ├── generated tool configs
       ├── Gemini prompt pack
       ├── Go hook policy bundle
       ├── Git hook runner
       └── agent hook runtime
```

The same inputs drive both guidance and enforcement. If a rule changes, update
the source YAML or renderer, regenerate, and review the derived diff.

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

The Go hook runner owns hook output policy. Reports honor
`hooks.output_format` (`auto`, `human`, `json`, or `toon`), with `auto`
selecting TOON when known agent/LLM environment markers are present. Successful
groups are silent by default through `hooks.success_output: silent`; set it to
`verbose` only when operator-facing pass summaries are useful. Enabled hook
groups run in parallel when `hooks.parallel_groups: true`, with group output
captured and replayed deterministically on failure. Default failure output is
intentionally narrow: show the failing checks and their actionable findings,
not pass tables, internal group names, or timings that do not help fix code.

The agent hook path is local-only: `pre-commit/hooks/run-go-hook.sh agent-hook`
compiles a policy bundle under `.git/coding-ethos-hooks/policy/` and runs the
new Go policy runtime. Gemini review checks remain pre-commit/pre-push checks;
they are not invoked from agent hooks. Agent hook evaluators are runtime-covered,
but Claude hook installation and cutover are still tracked in
[HOOK_REPLACEMENT_PLAN.md](HOOK_REPLACEMENT_PLAN.md).

Installed Git hook shims compile the policy bundle and enter
`coding-ethos-git-hook`, the compiled-policy-owned Git hook runtime. That runtime
runs policy preflight and then executes the bundled hook groups as the active
quality gate. `make install-hooks` also installs `post-commit`, `post-merge`,
and `post-checkout` shims that delegate to Git LFS when it is available.
The standalone `coding-ethos-lint` path can now execute compiled smoke/full
policies for generated tool-config freshness and the configured pytest gate, so
those checks are no longer inert policy metadata.
External command failures can carry normalized diagnostics in the lint JSON
result. The initial parser registry covers Ruff, Pyright, mypy, Pylint,
golangci-lint, and generic `file:line:column` text output.
Known diagnostic codes are enriched from compiled `policy.evidence_maps`, so
ETHOS-significant findings can carry `policy_id`, `principle_ids`, confidence,
meaning, and repair advice while unmapped tool findings still flow through
unchanged.

Render or verify Claude agent hook settings without touching global files:

```bash
pre-commit/hooks/run-go-hook.sh agent-hooks print
pre-commit/hooks/run-go-hook.sh agent-hooks sync --settings .claude/settings.local.json
pre-commit/hooks/run-go-hook.sh agent-hooks doctor --settings .claude/settings.local.json
```

The sync command only writes the explicit `--settings` path. The current
settings renderer supports `--provider claude` and consumes the shared
provider-neutral hook spec list before emitting Claude settings. Generated
Claude settings cover `PreToolUse`, `PostToolUse`, `PreCompact`, and
`SessionStart` compact replay. Continuation state is stored under
`.git/coding-ethos-hooks/continuation/`; hook execution never calls Gemini or
another model from the agent-hook path.

For work directly on this `coding-ethos` repository, an admin may authorize a
specific agent session by placing an approved process PID in
`/etc/coding-ethos-admin.pids`. In that repo-local, admin-supervised case only,
the git wrapper accepts `--admin-approved` before the git subcommand, such as
`pre-commit/hooks/run-go-hook.sh policy-git --admin-approved commit -m "..."`.
The flag only changes `git.staged_admin_files` from block to record; it does not
disable any other policy and it is invalid outside this repository.

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
