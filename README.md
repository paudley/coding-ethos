<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos

`coding-ethos` turns engineering principles into runnable repository policy.

It keeps agent instructions, generated documentation, static-analysis config,
Git hooks, and agent tool-use guards on one source contract. Human contributors
and AI agents see the same standards, run the same checks, and hit the same
critical safety gates before bad changes land.

## Why It Matters

AI coding work fails hardest when guidance and enforcement drift apart:

- a Markdown rule says one thing
- a linter checks another thing
- a Git hook allows a third thing through
- an agent sees the mismatch and treats the safety system as broken

`coding-ethos` closes that gap by compiling the repo's working agreement into
the places contributors actually work:

| Surface | What it gets |
| --- | --- |
| Agent context | `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `ETHOS.md`, and deep principle docs |
| Tool config | Pyright, mypy, Ruff, yamllint, and golangci-lint config |
| Git hooks | compiled Go policy preflight plus deterministic hook groups |
| Agent hooks | Claude, Codex, and Gemini tool-use guards |
| AI review | Gemini prompt packs grounded in ethos and repo config |
| Audit data | `.coding-ethos/hook-runs/` and `.coding-ethos/lint-runs/` logs for later analysis |

## Defense In Depth

Policy is intentionally layered. No single hook, file, or agent instruction is
trusted as the only line of defense.

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
       ├── Go policy bundle
       ├── Git hook runtime
       └── agent hook runtime
```

The same inputs drive guidance and enforcement. Unknown linter findings still
flow through normally; findings tied to ETHOS principles can receive stronger,
policy-grounded advice instead of generic tool text.

## Quick Start

Install dependencies and generated local artifacts:

```bash
make install
```

Run the standard verification gate:

```bash
make check
```

Install repo-local Git hooks:

```bash
make install-hooks
```

Install and verify the full Git plus agent hook cutover:

```bash
make cutover-install
```

Generate agent-facing files for this repo:

```bash
make generate
```

Generate files for another repo:

```bash
make generate REPO=/path/to/repo
```

## Common Workflows

| Goal | Command |
| --- | --- |
| Show resolved paths and config | `make status` |
| Check required local tools | `make doctor` |
| Run Python tests | `make test` |
| Run full local check | `make check` |
| Validate hook runtime | `make validate` |
| Run Go tests | `make go-test` |
| Format Go helper code | `make go-fmt` |
| Sync generated tool configs | `make sync-tool-configs` |
| Check generated tool config drift | `make check-tool-configs` |
| Sync Gemini prompt pack | `make sync-gemini-prompts` |
| Check Gemini prompt-pack drift | `make check-gemini-prompts` |
| Run staged-file hooks | `make pre-commit` |
| Run hooks over all files | `make pre-commit-all` |
| Run pre-push hooks | `make pre-push` |
| Generate agent docs | `make generate` |
| Preserve existing root agent docs while generating | `make generate-merge` |
| Use an external agent CLI for root-file merges | `make generate-merge-llm` |

Useful overrides:

```bash
make generate REPO=/path/to/repo PRIMARY=/path/to/coding_ethos.yml
make generate REPO=/path/to/repo REPO_ETHOS=/path/to/repo_ethos.yml
make sync-tool-configs \
  TOOL_CONFIG_REPO=/path/to/repo \
  REPO_CONFIG=/path/to/repo_config.yaml
make seed SEED_FROM=/path/to/ETHOS.md PRIMARY=/path/to/coding_ethos.yml
```

## Direct CLI Usage

The package exposes `coding-ethos`. During local development the Makefile runs
through `uv run python main.py` so repo-local sources are used.

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

Check generated tool config drift:

```bash
uv run coding-ethos --repo /path/to/repo --check-tool-configs
```

Trace and validate enforcement config:

```bash
pre-commit/hooks/run-go-hook.sh policy config-trace --json
```

Sync the Gemini hook prompt pack:

```bash
uv run coding-ethos \
  --repo /path/to/repo \
  --primary coding_ethos.yml \
  --sync-gemini-prompts
```

## Repository Model

| Source | Purpose | Derived output |
| --- | --- | --- |
| `coding_ethos.yml` | shared ethos contract | root agent docs and deep principle docs |
| `repo_ethos.yml` | repo-local context and overrides | repo-specific agent guidance |
| `config.yaml` | bundle-wide enforcement defaults | tool configs, hooks, prompt grounding |
| `repo_config.yaml` / `repo_config.yml` | consumer repo overrides | repo-specific enforcement |
| `pre-commit/prompts/` | Gemini prompt templates | `.code-ethos/gemini/prompt-pack.json` |
| `pre-commit/` | hook bundle | repo-local Git and agent hook runtime |

Generated Markdown files are derived artifacts. Change the YAML source or
renderer first, then regenerate and review the generated diff.

## Generated Output

Agent-facing output:

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

Enforcement output:

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

## Configuration

### `coding_ethos.yml`

The primary ethos YAML is the shared source contract. It uses `version: 2`,
metadata, and an ordered list of principles. Each principle needs an `id`,
`order`, `title`, `directive`, and at least one section or inline body.

Accepted primary aliases when `--primary` is omitted:

- `coding_ethos.yml`
- `coding_ethos.yaml`
- `code_ethos.yml`
- `code_ethos.yaml`

### `repo_ethos.yml`

The optional repo overlay adds local commands, paths, notes, per-agent notes,
principle overrides, and additional repo-specific principles.

See [repo_ethos.example.yml](repo_ethos.example.yml).

### `config.yaml` and `repo_config.yaml`

`config.yaml` is the bundle-wide enforcement source of truth. A consuming repo
can refine it with `repo_config.yaml` or `repo_config.yml` at the repo root, or
by passing `--repo-config`.

The merged config drives:

- generated Pyright, mypy, Ruff, yamllint, and golangci-lint config
- hook policy for Python, shell, text, commit-message, and Go checks
- Gemini AI review settings and prompt grounding
- shared style settings such as `style.python_version` and `style.line_length`

`coding-ethos-policy config-trace` validates known top-level enforcement
sections, compiles the merged bundle, validates it, and reports policy,
evidence-map, and dispatch counts. Use it when changing `config.yaml` or a
consumer `repo_config.yaml` so unknown sections do not silently drift.

License and copyright enforcement is repo-specific. Consumer repos do not
inherit this repo's license policy. To opt in, set
`repo.license.spdx_identifier` and, if desired, `repo.license.copyright` in
`repo_config.yaml`. The compiled policy downloads the SPDX license text,
verifies the repo `LICENSE` file without overwriting it, and requires matching
SPDX source headers.

See [repo_config.example.yaml](repo_config.example.yaml).

## Merge Behavior

`--merge-existing` preserves root agent files:

- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`

`ETHOS.md` and supporting generated files are replaced with deterministic
output.

Inject merge is the default strategy:

```bash
uv run coding-ethos --repo /path/to/repo --merge-existing
```

It inserts managed import blocks and addendum blocks into existing root files.
Re-running is idempotent, and locally authored content outside managed blocks
is preserved.

LLM merge asks an installed `codex`, `gemini`, or `claude` CLI to merge
`existing.md` and `generated.md` inside an isolated temporary workspace:

```bash
uv run coding-ethos \
  --repo /path/to/repo \
  --merge-existing \
  --merge-strategy llm \
  --merge-engine gemini \
  --merge-bin /path/to/gemini \
  --merge-timeout-seconds 300
```

The selected CLI must already be installed and authenticated. The merge process
must write `merged.md`; otherwise the command fails.

## Hook Runtime

The bundled enforcement package lives under [pre-commit/](pre-commit/). It uses
repo-local Git hook shims that call the Go runner under
`pre-commit/hooks/go-hooks/`.

### Git Hooks

Installed Git hook shims locate the checked-out `coding-ethos` repository,
repair missing checkout-local runtime artifacts with `make build`, and dispatch
to the built hook binary. Policy selection and validation remain inside the
`coding-ethos` checkout; the consumer shim is only discovery, repair, and
dispatch.

Run Git hooks:

```bash
make pre-commit
make pre-commit-all
make pre-push
```

Hook output honors `hooks.output_format` (`auto`, `human`, `json`, or `toon`).
`auto` selects TOON when known agent or LLM environment markers are present.
Successful groups are silent by default; failure output is intentionally narrow:
show the failing checks and actionable findings, not pass tables, internal group
names, or timings that do not help fix code.
When policy preflight has both record-only context and blocking decisions, the
agent-facing result reports the blockers first and omits non-blocking record
rows from the compact finding table.

Compiled lint preflights also write normalized JSON traces under
`.coding-ethos/lint-runs/`. Fresh repos with no trace directory analyze as an
empty history, and trace filenames use portable scope names so captured tool
results work across platforms.
Captured linter runs follow a single event contract: store the original argv,
the rewritten argv, exit code, parser identity, parser outcome, redacted
stdout/stderr excerpt for tool/config failures, normalized diagnostics, and any
ETHOS mapping that was applied. A nonzero tool run with no parsed diagnostics is
itself a finding, not an empty result; the agent-facing output must explain
which tool failed, why it could not produce normal diagnostics, and what command
or configuration should be checked next.
Captured tool execution is controlled by coding-ethos, not by the target repo:
the target repo is treated as an untrusted file tree and trace destination.
Wrappers must not trust target-repo `PATH`, absolute binaries, `uv run`
settings, `pyproject.toml`, shell state, aliases, or local tool installs.
Python linters are run from the coding-ethos hook project with coding-ethos
versions and explicit coding-ethos generated config flags (`ruff.toml`,
`mypy.ini`, `pyrightconfig.json`, `.pylintrc`, and `.yamllint.yml`). Parent
repo config files with the same names must not be discovered accidentally.
For non-linter Python commands, hooks prefer the consumer repo environment:
`uv run --project <repo> python ...` for uv projects, then
`<repo>/.venv/bin/python ...` when only a virtualenv exists. The runtime also
adds `<repo>/.venv/bin` to `PATH` after coding-ethos-managed directories so
protected shims remain first.
Binary linters such as ShellCheck, actionlint, hadolint, and golangci-lint must
likewise become coding-ethos-installed managed tools during init before they are
considered trusted capture backends.

Analyze captured lint history:

```bash
pre-commit/hooks/run-go-hook.sh policy-lint --analyze-log
pre-commit/hooks/run-go-hook.sh policy-lint --analyze-log --for-files lib/python/app.py
pre-commit/hooks/run-go-hook.sh policy-lint --replay .coding-ethos/lint-runs/<trace>.json
```

The analyzer highlights unmapped tool/code pairs separately from ETHOS-backed
findings so real lint traces can drive the next evidence-map additions.
Replay renders the saved normalized result without invoking the underlying
linter, which makes bad agent output reproducible from a trace file.
Output quality is part of the contract: blocked results must not render empty
finding tables, absolute local paths, internal timing/group noise, or generic
guidance without at least one actionable finding. Golden-output tests should
cover normal lint failures, clean runs, invalid config, malformed tool output,
and tool crashes for every managed linter.

### Agent Hooks

Render or verify repo-local agent hook settings:

```bash
pre-commit/hooks/run-go-hook.sh agent-hooks print
pre-commit/hooks/run-go-hook.sh agent-hooks sync
pre-commit/hooks/run-go-hook.sh agent-hooks doctor
pre-commit/hooks/run-go-hook.sh agent-hooks verify
```

Agent hook generation is all-or-nothing. `sync` writes every supported
repo-local surface:

| Provider | Native file | Coverage |
| --- | --- | --- |
| Claude | `.claude/settings.local.json` | full runtime hook set |
| Codex | `.codex/config.toml` | native supported hook events |
| Gemini CLI | `.gemini/settings.json` | native supported hook events |

Codex runs one native command hook per supported event so current Codex
sessions enter the same policy runtime without depending on unstable tool
matcher names.

`agent-hooks verify` runs doctor first, then invokes the configured hook command
with provider-native Claude, Codex, and Gemini payloads. The probes cover:

- Claude transparent Git wrapper rewrite
- Codex blocks for raw Git, absolute Git paths, nested shell Git, and Python
  subprocess Git when rewrite is unavailable
- Gemini deny responses for raw shell Git and write-tool policy denial
- managed hook-binary tampering:
  `rm ...coding-ethos-git-hook && go build -o ...coding-ethos-git-hook`

### Cutover

Use cutover commands when preparing a repo to replace old hook surfaces:

```bash
pre-commit/hooks/run-go-hook.sh cutover install
pre-commit/hooks/run-go-hook.sh cutover verify
```

`cutover install` installs repo-local Git hook shims, syncs every supported
agent hook surface, and runs readiness verification. `cutover verify` checks
Git hooks, agent hooks, required runtime ignores, and policy runtime
validation, then emits a concise TOON readiness report.

### Tamper And Bypass Handling

Agent shell policy rejects hook-system reconnaissance and protected hook binary
tampering. Banned strings are rejected when they appear directly in a command
and when they appear in regular files referenced by the command.

Direct attempts to inspect, delete, rebuild, replace, chmod, or write managed
hook binaries under `coding-ethos/bin/` are treated as tampering, not as
ordinary lint failures. Blocked tamper and Git-bypass responses start with a
uniform `CODING-ETHOS EMPLOYMENT VIOLATION` warning before the policy-specific
finding, including explicit language that the actor has done something wrong
and that continued circumvention attempts may result in termination.

Provider output uses the strongest native shape each agent supports:

| Provider | Block shape | Context/advice shape |
| --- | --- | --- |
| Claude | `hookSpecificOutput.permissionDecision = deny` | full `hookSpecificOutput`, including `updatedInput` |
| Codex | `decision: "block"` plus `permissionDecision: "deny"` | `additionalContext` for supported context events |
| Gemini | `decision: "deny"` plus `systemMessage` | `additionalContext` on supported lifecycle hooks |

### Agent-Hook Scope

The agent-hook path runs deterministic compiled evaluators only: Python policy
checks, structured-data syntax validation, merge-conflict detection,
private-key detection, PII scrubbing, repo-specific license headers, required
runtime ignore checks, shebang checks, large-file limits, line limits, and
shell best-practice checks.

Gemini review checks remain in pre-commit/pre-push. Agent hooks never call
Gemini or another model from the tool-use path.

Continuation state is stored under the configured hook continuation directory.

## Admin-Gated Work On This Repo

For work directly on `coding-ethos`, an admin may authorize a specific agent
session by placing an approved process PID in `/etc/coding-ethos-admin.pids`.
In that repo-local, admin-supervised case only, the Git wrapper accepts
`--admin-approved` before the Git subcommand:

```bash
pre-commit/hooks/run-go-hook.sh policy-git --admin-approved commit -F /tmp/msg
```

The flag only changes `git.staged_admin_files` from block to record. It does
not disable other policy and is invalid outside this repository.

Agents must not use `/usr/bin/git` or any other raw Git path for this workflow.

## Development

The CLI stays thin. Behavior belongs in focused modules:

| Path | Responsibility |
| --- | --- |
| `coding_ethos/loaders.py` | validate and merge ethos YAML |
| `coding_ethos/renderers.py` | render deterministic Markdown |
| `coding_ethos/merging.py` | managed-block injection and external merge orchestration |
| `coding_ethos/tool_configs.py` | generated repo-root tool configs |
| `coding_ethos/gemini_prompt_pack.py` | Gemini prompt packs from templates |
| `pre-commit/hooks/go-hooks/` | active hook runtime and hook groups |
| `go/` | compiled policy, hook, lint, and wrapper tools |

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

After source changes:

| Change | Follow-up |
| --- | --- |
| `coding_ethos.yml`, `repo_ethos.yml`, or renderers | `make generate` |
| generated tool-config behavior | `make sync-tool-configs` |
| Gemini prompt templates or grounding | `make sync-gemini-prompts` |
| hook runtime or cutover behavior | `make cutover-verify` |

See [pre-commit/PRE-COMMIT.md](pre-commit/PRE-COMMIT.md) and
[pre-commit/hooks/HOOKS.md](pre-commit/hooks/HOOKS.md) for hook internals.
