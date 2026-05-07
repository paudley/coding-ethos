<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Repository Analysis

`coding-ethos` is a Python agent-doc generator plus a Go enforcement and
generated-artifact runtime. Its core job is to keep generated agent
documentation, generated tool configuration, and hook prompt grounding aligned
with a shared structured ethos.

## System responsibilities

- `coding_ethos/cli.py`: CLI argument parsing and top-level workflow
  orchestration.
- `coding_ethos/loaders.py`: primary ethos validation and repo overlay merge
  semantics.
- `coding_ethos/models.py`: typed in-memory contract shared by Python
  documentation renderers.
- `coding_ethos/renderers.py`: deterministic Markdown output for agent-facing
  files.
- `coding_ethos/markdown_seed.py`: Markdown-to-YAML seeding.
- `coding_ethos/merging.py`: managed-block injection and external LLM merge
  orchestration.
- `go/internal/toolconfigs`: generated tool config and CI sync, drift checks,
  and manifest handling.
- `go/internal/geminiprompts`: generated Gemini hook prompt pack.
- `go/internal/agentskills`: generated provider skill surfaces.
- `pre-commit/prompts/`: prompt templates for Gemini review checks.
- `go/internal/hookrunnercli`: active hook groups, hook reports, Gemini review
  orchestration, and hook runner CLI behavior.
- `go/internal/managedcapture`: managed tool execution, stdout/stderr capture,
  formatter change detection, and trace metadata.
- `go/diagnostics`: normalized parsers for linter, formatter, type-checker, and
  test output.
- `go/internal/policy`, `go/internal/hooks`, `go/internal/gitwrap`,
  `go/internal/realgit`, and `go/internal/agenthooks`: compiled policy runtime,
  agent hook adapters, real Git execution, and git wrapper enforcement.
- `go/internal/codeintel`: repo-local trace, SARIF, AST, vector, and
  remediation storage/query.
- `go/internal/agentproxy`: provider-neutral event envelope and transform
  foundations for future agent proxy work.
- `go/cmd/*`: thin process entrypoints that delegate to internal packages.

The CLI is intentionally thin. New behavior should usually land in one of the
narrow modules above, with `cli.py` limited to wiring and exit-code handling.
The same rule applies to Go command entrypoints: do not put parser,
formatter, policy, storage, or hook-group behavior directly in `go/cmd/*` when
an internal package can own and test the behavior.

## Source-of-truth files

`coding_ethos.yml` is the shared documentation contract. It defines the
principle list, agent profiles, quick references, merge topics, and detailed
section prose. `repo_ethos.yml` is the repo-local refinement layer used when
generating this repository's checked-in agent files.

`config.yaml` is the bundle-wide enforcement contract. It controls generated
tool configs and hook policy. Consumer repositories refine those defaults with
`repo_config.yaml` or `repo_config.yml`.

`pre-commit/prompts/` contains Jinja templates used to render
`.code-ethos/gemini/prompt-pack.json`. The prompt pack is generated from ethos
content, repo context, enforcement settings, and template text.

## Generated artifacts

Agent-facing generated artifacts:

- `ETHOS.md`
- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.agents/ethos/*.md`
- `.claude/ethos/MEMORY.md`
- `.agent-context/prompt-addons/*.md`

Enforcement generated artifacts:

- `pyrightconfig.json`
- `mypy.ini`
- `ruff.toml`
- `.yamllint.yml`
- `.bandit.yml`
- `.sqlfluff`
- `tombi.toml`
- `.golangci.yml`
- `.code-ethos/gemini/prompt-pack.json`

Generated Markdown files should not be hand-edited unless the task is
explicitly about the generated output itself. Change the ethos YAML or renderer
logic, regenerate, and review the derived diff.

## Runtime flow

For agent document generation, the CLI resolves the target repo, primary ethos
YAML, and optional repo overlay. It loads and validates the primary bundle,
applies the overlay when present, renders all deterministic output, and writes
files into the target repo. With `--merge-existing`, only `AGENTS.md`,
`CLAUDE.md`, and `GEMINI.md` are merge-preserved.

For tool-config sync, the CLI remains the user-facing entrypoint, but generated
tool config rendering now runs through the Go implementation under
`go/internal/toolconfigs`. That code loads `config.yaml`, applies an optional
consumer repo config, renders the supported tool config files, and either
writes them or reports drift.

For Gemini prompt-pack sync, the CLI remains the user-facing entrypoint while
the Go implementation under `go/internal/geminiprompts` merges ethos context
and enforcement config, renders every prompt template, attaches check selectors
and runtime metadata, and writes `.code-ethos/gemini/prompt-pack.json`.

For hooks, the Makefile installs repo-local Git hook entrypoints that resolve
to the Go helper and compiled policy entrypoints. `cutover` also syncs Claude, Codex, and
Gemini repo-local agent hook settings, verifies the git wrapper path, and
checks required runtime ignores before declaring the hook surface ready.

## Validation boundaries

The primary ethos loader rejects malformed schema versions, empty principle
lists, duplicate principle ids, duplicate orders, unknown related principle
ids, unsupported agents, malformed sections, and empty required fields.

The repo overlay rejects non-mapping repo payloads, unsupported agent notes,
unknown principle override ids, malformed override sections, duplicate
additional principle ids, and invalid final principle collections.

The generated config path treats missing or out-of-sync generated files as
verification failures rather than silently tolerating drift.

## Change impact guide

- CLI flags or Makefile workflow: update `README.md`, `CONTRIBUTING.md`, and
  tests.
- Primary ethos schema or output layout: update `README.md`,
  `repo_ethos.example.yml`, and `tests/test_cli.py`.
- Repo overlay semantics: update `README.md`, `repo_ethos.example.yml`, and
  loader tests.
- Tool-config rendering: update `README.md`, `repo_config.example.yaml`, and
  Go tests under `go/internal/toolconfigs`.
- Gemini prompt-pack rendering: update `README.md`, prompt templates, and Go
  tests under `go/internal/geminiprompts`.
- Hook runtime behavior: update `pre-commit/PRE-COMMIT.md`,
  `pre-commit/hooks/HOOKS.md`, and Go tests.
- Public package exports: update `coding_ethos/CODING_ETHOS.md` and tests.

## Verification contract

The canonical test command is:

```bash
make test
```

The local toolchain and hook-path check is:

```bash
make doctor
```

The current repository gate is:

```bash
make check
```

Hook changes should also run:

```bash
make validate
make go-test
```

Generated artifacts must be synced before finishing changes that affect their
source inputs:

```bash
make generate
make sync-tool-configs
make sync-gemini-prompts
```
