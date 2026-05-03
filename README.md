<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

<p align="center">
  <img src="https://raw.githubusercontent.com/paudley/coding-ethos/main/docs/logo-banner.svg" alt="Coding Ethos Logo" width="600">
</p>

# Coding Ethos

[![CI](https://github.com/paudley/coding-ethos/actions/workflows/ci.yml/badge.svg)](https://github.com/paudley/coding-ethos/actions/workflows/ci.yml)
[![Coding Ethos SARIF](https://github.com/paudley/coding-ethos/actions/workflows/coding-ethos-sarif.yml/badge.svg)](https://github.com/paudley/coding-ethos/actions/workflows/coding-ethos-sarif.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/paudley/coding-ethos/badge)](https://scorecard.dev/viewer/?uri=github.com/paudley/coding-ethos)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Policy-as-code enforcement for AI agents: MCP server, CEL policies, Git hooks,
SARIF, runtime sandboxing, and static-analysis guardrails.

`coding-ethos` turns engineering principles into runnable repository policy for
human contributors and AI coding agents.

It keeps agent instructions, generated documentation, static-analysis config,
Git hooks, agent tool-use guards, MCP tools, CEL custom policies, generated
skills, and runtime axioms on one source contract. Human contributors and AI
agents see the same standards, run the same checks, and hit the same critical
safety gates before bad changes land.

Use `coding-ethos` when you need:

- AI-agent guardrails for Codex, Claude Code, Gemini CLI, and other coding
  agents.
- A local MCP server that agents can call for policy checks, lint advice,
  SARIF remediation, and ETHOS-grounded context.
- Git hook enforcement that catches unsafe commands, protected path edits,
  unmanaged tool use, and file-growth problems before commit time.
- CEL policy-as-code that keeps repo-specific rules close to the ETHOS
  principle they enforce.
- SARIF and code-scanning output for CI, pull requests, IDEs, and trend
  analysis.

## 30-Second Start

```bash
make install
make check
make install-hooks
```

For full Git plus AI-agent hook cutover:

```bash
make cutover-install
```

Start the local MCP server for configured agents:

```bash
bin/coding-ethos-run mcp
```

![coding-ethos MCP and SARIF demo](https://raw.githubusercontent.com/paudley/coding-ethos/main/docs/assets/coding-ethos-demo.gif)

The project is built around defense in depth for AI-assisted coding:

- **ETHOS as source:** `coding_ethos.yml` and repo overlays are the backbone:
  principles own their skills, axioms, generated docs, and first-class policy
  grounding.
- **Compiled enforcement:** Go hook runtimes evaluate built-in policies and
  typed CEL expression policies through the same decision model.
- **Managed tools:** lint and type checks run through generated configs,
  managed binaries, normalized diagnostics, and trace logging.
- **Runtime capabilities:** managed tools declare network, Git, sandbox,
  timeout, memory, CPU, and seccomp capabilities that CEL, MCP, traces, and
  SARIF can all inspect.
- **Sandboxed capture:** managed lint can run through the Bubblewrap-backed
  sandbox prototype with read-only repository and `.git` mounts, disconnected
  network for offline tools, and normalized denial evidence.
- **Agent steering:** Claude, Codex, and Gemini receive generated hook settings,
  MCP server configuration, skills, prompt addenda, and compact axiom advice.
- **Repair feedback:** lint findings, blocked policy decisions, skill hints,
  and MCP guidance all point agents back to the relevant ETHOS contract.

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
| Tool config | Pyright, mypy, Ruff, Pylint, YAML, Bandit, SQLFluff, Tombi, and golangci-lint config |
| Git hooks | compiled Go policy preflight plus deterministic hook groups |
| Agent hooks | Claude, Codex, and Gemini tool-use guards |
| MCP | stdio policy, skill, lint, SARIF, and tool-capability queries from the compiled bundle |
| AI review | Gemini prompt packs grounded in ethos and repo config |
| CI/CD | SARIF output plus generated GitHub Actions and GitLab CI gates with actionlint, artifacts, package validation, and sandbox evidence |
| Audit data | `.coding-ethos/hook-runs/` and `.coding-ethos/lint-runs/` logs with policy, tool, SARIF, and sandbox evidence |

## Agents Used In This Repository

`coding-ethos` is developed with human review and AI-agent assistance. The
project explicitly targets and has been shaped by work with:

- **Codex from OpenAI**: coding, review, refactoring, documentation, and
  repo-policy workflow validation.
- **Claude Code**: coding, hook integration, generated skill surfaces, and
  policy feedback loops.
- **Gemini CLI**: review prompts, generated prompt packs, and independent
  agent-hook compatibility checks.

Agent assistance does not change the quality bar. Generated or agent-authored
changes are expected to pass the same hooks, static analysis, tests, review
feedback, and ETHOS policy gates as human-authored changes.

The project heavily dogfoods its own guardrails: Codex, Claude, and Gemini are
run through the generated hooks, MCP configuration, skills, axioms, managed
toolchain, and policy feedback surfaces while developing `coding-ethos` itself.

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
.agents/skills/ remediation playbooks
runtime axioms with MCP next steps
principle-owned CEL policies

config.yaml          repo_config.yaml
       │                    │
       ├── merged enforcement config
       │
       ├── generated tool configs
       ├── transitional CEL expression policies
       ├── Gemini prompt pack
       ├── Go policy bundle
       ├── Git hook runtime
       ├── agent hook runtime
       └── MCP server tools
```

The same inputs drive guidance and enforcement. Unknown linter findings still
flow through normally; findings tied to ETHOS principles can receive stronger,
policy-grounded advice instead of generic tool text. When a finding maps to a
generated skill, agent-facing output includes a compact `skill_id` hint and a
next action to load that remediation playbook.

Skills and axioms are part of the same defense-in-depth plan, not decorative
prompt text. Skills provide provider-portable remediation playbooks. Axioms are
short principle-local reminders that hooks surface when they are related to a
policy decision, always on lint calls, and statistically on other post-hook
events. Rendered axiom advice includes the MCP call an agent should use next,
so advice can escalate from compact guidance to `policy_explain`,
`skill_lookup`, or `skill_recommend` without dumping full context into every
hook response.

CEL support extends that same model to repo-specific policy. First-class CEL
policies live with the ETHOS principle they enforce in `coding_ethos.yml`.
Config-level `policy.expressions` remains available for consumer overlays and
for transitional policy that has not yet been expressed as part of the ETHOS
contract. The compiler checks CEL up front, dispatches it through hook and lint
paths, and emits normal policy decisions with ETHOS grounding and skill hints.

Runtime capability policy uses the same path. Managed tools declare whether
they need network, Git, environment access, writable paths, sandbox profiles,
timeouts, memory, CPU, and seccomp profiles. Those facts are available to CEL
as `tool_capabilities`, exposed through MCP, retained in `.coding-ethos`
traces, and copied into SARIF run properties. The built-in managed-tool
contract blocks ordinary lint tools that forget to declare offline/no-Git
behavior or resource bounds.

Runtime sandboxing is the complementary data plane. The current Go prototype
can run managed lint capture through Bubblewrap with a read-only root,
read-only repository and `.git`, hidden home directories, disconnected network
for offline tools, declared writable mounts, hard timeouts, cgroup resource
requests, and seccomp profile metadata. Linux cgroup limits are prepared before
process start in a delegated hierarchy and cleaned up after exit. Required
sandbox mode fails closed with a normalized policy finding; advisory mode
records degraded evidence without claiming enforcement. See
[docs/RUNTIME_SANDBOXING.md](docs/RUNTIME_SANDBOXING.md).

For larger platform directions such as deeper MCP context serving,
policy-language support, IDE integration, SARIF/CI components, red-team
testing, ETHOS inheritance, and agent remediation loops, see
[docs/STRATEGIC_ROADMAP.md](docs/STRATEGIC_ROADMAP.md).
The documentation landing page is [docs/index.md](docs/index.md), and
promotion/security trust work is tracked in
[docs/TRUST_SIGNALS.md](docs/TRUST_SIGNALS.md).
Supply-chain trust controls, Scorecard publishing, GitHub artifact
attestations, SBOM generation, PyPI Trusted Publishing, and verification
commands are documented in
[docs/SUPPLY_CHAIN_ATTESTATIONS.md](docs/SUPPLY_CHAIN_ATTESTATIONS.md).
The security posture is summarized in [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md),
and release readiness is documented in [docs/RELEASE.md](docs/RELEASE.md).
The verified demo transcript is [docs/DEMO.md](docs/DEMO.md).
For positioning and adoption planning, see
[docs/COMPARISON.md](docs/COMPARISON.md),
[docs/INTEGRATIONS.md](docs/INTEGRATIONS.md), and
[examples/](examples/).
The CEL-first policy-language design is tracked in
[docs/POLICY_LANGUAGE_STRATEGY.md](docs/POLICY_LANGUAGE_STRATEGY.md).
CI/CD usage and SARIF upload examples are documented in
[docs/CI_CD_SARIF.md](docs/CI_CD_SARIF.md).
Runnable and copyable examples start in [examples/](examples/).

## MCP Server

`coding-ethos` includes a local stdio MCP server backed by the same compiled
policy bundle and generated skill metadata used by Git hooks and agent hooks.
The design and expansion plan are documented in
[docs/MCP_SERVER.md](docs/MCP_SERVER.md).
The server is exposed through the managed runtime:

```bash
bin/coding-ethos-run mcp
```

The first tools are intentionally narrow and auditable:

- `policy_check_command`: check a proposed shell command before running it.
- `policy_check_edit`: check a proposed file edit before applying it.
- `lint_check`: run managed lint capture for Ruff, mypy, pyright, pylint,
  SQLFluff, and other captured tools; when no tool is supplied, run compiled
  coding-ethos policy lint checks for current work.
- `lint_advice`: map a lint diagnostic to ETHOS policy, advice, and skill hints.
- `sarif_remediation_advice`: turn SARIF or retained trace evidence into
  focused ETHOS-grounded repair guidance.
- `sarif_risk_summary`: summarize a SARIF run for policy, skill, file, tool,
  severity, and next-action risk signals.
- `sarif_trend_analysis`: compare SARIF runs or retained traces for
  introduced, fixed, persisting, reopened, and worsening findings.
- `sarif_policy_feedback`: report unmapped diagnostics, missing skills, weak
  severity mappings, and noisy rules for policy authors.
- `tool_capabilities`: list managed tool capabilities, including network/Git
  tags, sandbox profile, timeout, memory, CPU, seccomp profile metadata, and
  declared read/write mounts.
- `policy_explain`: return the compiled explanation for a policy ID.
- `skill_lookup`: return an ETHOS-derived skill playbook by skill ID.
- `skill_recommend`: recommend ETHOS-derived skills for the task at hand.

Tool definitions include `coding_ethos` metadata that tells clients whether a
tool is advisory, reads files, executes managed lint tools, and persists traces.
Agents should call `lint_check` instead of invoking linters directly so target
resolution, generated config integrity, managed tool versions, evidence maps,
skill hints, and trace logging stay on the enforced path.
Agents should call `tool_capabilities` before choosing a managed tool when
runtime behavior matters; it is the MCP view of the same capability facts CEL
uses for policy decisions.

The MCP server is advisory context, not a bypass. Hook enforcement remains on
the normal Git and agent-hook paths, and MCP responses come from the same
compiled policy inputs as those enforcement paths.

## ETHOS Skills

Skills are generated remediation playbooks, not a separate hand-maintained
documentation layer. `coding_ethos.yml` defines each skill with its ETHOS
principles, trigger terms, short hint, focus, and remediation steps. `make
build` renders those skills into the portable `.agents/skills/` tree and the
native Claude, Codex, and Gemini skill locations.

The compiled policy bundle carries the same skill metadata. Runtime lint and
hook results attach a `skill_id` when a finding maps through an evidence map,
overlaps a skill's ETHOS principles, or matches a skill trigger term.
Agent-facing output stays compact: TOON and human output emit the skill ID,
short hint, and next action instead of dumping the full skill body into the
agent context.

Skill hints are also logged under `.coding-ethos/lint-runs/`. Those traces let
the project measure which remediation playbooks appear in real work and promote
recurring unmapped findings into stronger evidence maps or repo-specific
skills.

Current built-in skills:

- `agent-operating-discipline`
- `conditional-imports`
- `lint-remediation`
- `managed-toolchain`
- `safe-git-workflow`

`agent-operating-discipline` adapts the useful behavioral pattern from
[`forrestchang/andrej-karpathy-skills`](https://github.com/forrestchang/andrej-karpathy-skills)
into coding-ethos' derived-skill model: explicit assumptions, simple designs,
surgical diffs, and verifiable success criteria. The upstream repo is a useful
inspiration source, but coding-ethos keeps the canonical text in
`coding_ethos.yml` and regenerates provider-specific skill files from that
source.

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

## PyPI Package Usage

The PyPI package installs the Python generator CLI plus the default
`coding_ethos.yml`, base `config.yaml`, example overlays, and Gemini prompt
templates. That path is useful for generating docs and config without cloning
the source checkout:

```bash
uvx coding-ethos --repo .
uvx coding-ethos --repo . --sync-tool-configs
uvx coding-ethos --repo . --sync-gemini-prompts
```

The same CLI can be run through `pipx`:

```bash
pipx run coding-ethos --repo .
```

The PyPI package does not yet publish the compiled Go hook runtime or managed
binary toolchain. Full Git hook and agent-hook installation still uses the
source checkout/submodule path with `make cutover-install`.

## Common Workflows

| Goal | Command |
| --- | --- |
| Show resolved paths and config | `make status` |
| Check required local tools | `make doctor` |
| Run Python tests | `make test` |
| Run full local check | `make check` |
| Smoke test the built wheel | `make package-smoke` |
| Dry-run release package checks | `make release-dry-run` |
| Validate hook runtime | `make validate` |
| Run Go tests | `make go-test` |
| Format Go helper code | `make go-fmt` |
| Sync generated tool configs | `make sync-tool-configs` |
| Check generated tool config drift | `make check-tool-configs` |
| Sync Gemini prompt pack | `make sync-gemini-prompts` |
| Check Gemini prompt-pack drift | `make check-gemini-prompts` |
| Check generated agent skill drift | `make check-agent-skills` |
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

By default the same command writes the managed SARIF CI files and includes
them in `.code-ethos/tool-config-hashes.json`. The generated GitHub workflow is
reusable by default so a repo-level CI workflow can own concurrency, required
checks, package validation, and attestations without duplicate SARIF uploads.
Repos with a deliberate exception can set
`generated_config.ci.github_actions.enabled: false` or
`generated_config.ci.gitlab.enabled: false` in their merged enforcement config.
They are checked by `--check-tool-configs`; there is no separate CI sync path.

Check generated tool config drift:

```bash
uv run coding-ethos --repo /path/to/repo --check-tool-configs
```

Trace and validate enforcement config:

```bash
bin/coding-ethos-run policy config-trace --json
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
| `coding_ethos.yml` | shared ethos contract | root agent docs, deep principle docs, ETHOS skills, axioms, and principle-owned CEL policies |
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
│   ├── ethos/
│   │   ├── README.md
│   │   ├── solid-is-law.md
│   │   └── ...
│   └── skills/
│       ├── conditional-imports/
│       │   └── SKILL.md
│       └── lint-remediation/
│           └── SKILL.md
├── .codex/
│   └── skills/
│       └── ...
├── .gemini/
│   └── extensions/
│       └── coding-ethos/
│           ├── gemini-extension.json
│           └── skills/
│               └── ...
└── .claude/
    ├── ethos/
    │   └── MEMORY.md
    └── skills/
        └── ...
```

Enforcement output:

```text
repo/
├── pyrightconfig.json
├── mypy.ini
├── ruff.toml
├── .yamllint.yml
├── .bandit.yml
├── .sqlfluff
├── tombi.toml
├── .golangci.yml
└── .code-ethos/
    ├── cache/
    │   └── ... ignored runtime caches
    └── gemini/
        └── prompt-pack.json
```

## Configuration

### `coding_ethos.yml`

The primary ethos YAML is the shared source contract. It uses `version: 2`,
metadata, and an ordered list of principles. Each principle needs an `id`,
`order`, `title`, `directive`, and at least one section or inline body.

The optional top-level `skills` list defines provider-portable skills grounded
in ETHOS principles. Generation emits the same skill body into the portable
`.agents/skills/` tree and the native Claude, Codex, and Gemini locations. The
compiled Go policy bundle also carries those skill definitions so linter
evidence can point at `skill_id` and runtime output can steer agents to the
right remediation playbook.

Each principle may also define local `axioms`. Axioms are short reminders owned
by the ETHOS principle they explain, not a separate enforcement-config list.
The compiler derives hook reminder advice from `principles[].axioms`, falling
back to the principle's `quick_ref` and directive when no explicit axioms are
present. That keeps advice, enforcement grounding, generated docs, and runtime
post-hook reminders attached to the same cohesive ETHOS entry.
Runtime hook advice surfaces those axioms in two stages: policy-related hook
results emit priority ETHOS reminders first, while unrelated post-hook output
gets one ambient reminder on lint calls and a sampled single reminder on other
calls. Rendered reminders include the MCP tool and arguments an agent should
call next, such as `policy_explain` for blocked policies or `skill_recommend`
for principle-level guidance.

Behavioral skills should follow the same source-of-truth rule as remediation
skills. For example, `agent-operating-discipline` incorporates ideas from
[`forrestchang/andrej-karpathy-skills`](https://github.com/forrestchang/andrej-karpathy-skills)
without copying static provider prompts into the repo; edits belong in
`coding_ethos.yml`, then `make build` regenerates the checked-in surfaces.

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

`coding_ethos.yml` is the backbone of policy intent. `config.yaml` is the
bundle-wide enforcement artifact for generated tool settings, operational
defaults, and policy that has not yet been expressed cleanly with an ETHOS
principle. A consuming repo can refine the compiled enforcement artifact with
`repo_config.yaml` or `repo_config.yml` at the repo root, or by passing
`--repo-config`.

The merged config drives:

- generated Pyright, mypy, Ruff, Pylint, YAML, Bandit, SQLFluff, Tombi, and
  golangci-lint config
- generated GitHub Actions and GitLab CI SARIF gates, controlled by
  `generated_config.ci.*.enabled`, timeout, trigger, artifact, test, and build
  knobs
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

### CEL Expression Policies

First-class CEL policies should live under the relevant principle in
`coding_ethos.yml`:

```yaml
principles:
  - id: solid-is-law
    policy:
      expressions:
        - id: filesystem.line_limits
          scope: file
          severity: block
          when: >
            file_changes.exists(file, file.ext == ".py" && file.line_count > 1000)
          message: Large source files must not keep growing.
          advice: Split large files into focused modules before committing.
```

Consumer repos can also add small custom policies under `policy.expressions` in
`repo_config.yaml`. That path is an overlay and transitional extension point,
not the preferred home for shared ETHOS policy. These policies are CEL
expressions compiled into the policy bundle and evaluated by the same Go hook
runtime as Go-backed policies.

Use CEL for narrow predicates over normalized hook or lint data, for example
blocking a repo-specific command pattern:

```yaml
policy:
  expressions:
    - id: custom.no_python_subprocess_git
      description: Block Python subprocess attempts to route around protected Git.
      scope: command
      severity: block
      principle_ids:
        - one-path-for-critical-operations
        - no-rationalized-shortcuts
      skill_id: safe-git-workflow
      when: >
        shell_commands.exists(cmd,
          cmd.name in ["python", "python3"] &&
          cmd.argv.exists(arg, arg.contains("subprocess")) &&
          cmd.argv.exists(arg, arg.contains("git"))
        )
      message: Git must go through the coding-ethos wrapper.
      advice: Use the protected Git wrapper and keep hook failures visible.
```

Current supported fields include:

- `command`: raw command text for command-scope hook policies.
- `argv`: parsed command arguments when available.
- `shell_commands`: parser-normalized shell command facts from
  `mvdan.cc/sh/v3/syntax`, including command name, argv, leading assignments,
  redirects, here-docs, line/column, background execution, dynamic expansion flags,
  command/process substitution flags, shell-exec detection, Git detection,
  lint-tool detection, and PATH override detection. Malformed shell text is
  blocked before policy evaluation continues.
- `files`: repo-provided file targets for the current hook or lint event.
- `file_changes`: typed staged-file facts, including status, extension,
  generated/test/protected flags, byte size, current line count, and original
  line count when Git can provide it.
- `diff`: staged diff facts prepared by Go, including changed/staged file
  lists, hunks, added lines, removed lines, line numbers, old/new line numbers,
  and hunk headers.
- `event`: provider-native hook metadata such as provider, hook name, tool,
  source, matcher, session ID, transcript path, tool-input/tool-response keys,
  return code, and provider booleans for Claude, Codex, and Gemini.
- `cwd`: invocation working directory.
- `scope`: expression scope such as `command`, `path`, `diagnostic`, or
  `finding`.
- `metadata`: non-sensitive event metadata.
- `path`, `diagnostic`, `finding`, and `repo`: typed objects for the initial
  path, diagnostic, finding, and repo policy slices.

CEL is intentionally pure. Expressions cannot read files, run shell or Git,
inspect environment variables, access the network, or depend on wall-clock
time. Go prepares normalized facts; CEL decides over those facts.

Every expression policy must be ETHOS-grounded with `principle_ids`, and should
include a `skill_id` when a generated skill explains the remediation path. CEL
matches emit normal coding-ethos decisions, diagnostics, TOON/human output,
trace data, and skill hints.

Current boundary:

- CEL now covers most simple and medium-complexity policy predicates over
  normalized facts, including Git, shell, file, diff, repo, path, diagnostic,
  finding, and event inputs.
- Multi-file and multi-finding semantics must use explicit collections such as
  `paths`, `files`, `file_changes`, `findings`, and `diff`; do not depend on
  implicit first-file ordering.
- Diff line facts are staged-diff facts. Policies that need unstaged editor
  content should use hook file/content facts or a purpose-built Go evaluator.
- Keep parsing, Git state modeling, managed toolchain behavior, path
  normalization, file-content scanning, generated-config freshness, and other
  security-sensitive fact collection in Go. CEL decides over prepared facts; it
  does not inspect the host directly.

See [docs/POLICY_LANGUAGE_STRATEGY.md](docs/POLICY_LANGUAGE_STRATEGY.md) for
the CEL-first decision record and the roadmap for a complete generic policy
engine.

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
`mypy.ini`, `pyrightconfig.json`, `.pylintrc`, `.yamllint.yml`,
`.bandit.yml`, `.sqlfluff`, and `tombi.toml`). Parent
repo config files with the same names must not be discovered accidentally.
For non-linter Python commands, hooks prefer the consumer repo environment:
`uv run --project <repo> python ...` for uv projects, then
`<repo>/.venv/bin/python ...` when only a virtualenv exists. The runtime also
adds `<repo>/.venv/bin` to `PATH` after coding-ethos-managed directories so
protected shims remain first.
Binary linters such as ShellCheck, actionlint, hadolint, dotenv-linter, and golangci-lint are
installed into `build/toolchain/` through the managed installer. ShellCheck,
actionlint, and hadolint use pinned GitHub release assets with SHA-256 digests;
golangci-lint is built into the managed Go bin directory with the repo Go
toolchain. The source manifest lives at
`pre-commit/hooks/managed-toolchain.tsv`, and the installed toolchain writes
`build/toolchain/manifest.tsv`. Hook execution treats missing managed binaries
as runtime artifact failures instead of falling back to host tools.

Analyze captured lint history:

```bash
bin/coding-ethos-run policy-lint --analyze-log
bin/coding-ethos-run policy-lint --analyze-log --for-files lib/python/app.py
bin/coding-ethos-run policy-lint --replay .coding-ethos/lint-runs/<trace>.json
```

Emit SARIF for CI/code-scanning surfaces:

```bash
bin/coding-ethos-run policy-lint --sarif --scope files --files lib/python/app.py
bin/coding-ethos-run policy-lint --managed-capture-tool ruff --sarif -- check lib/python/app.py
bin/coding-ethos-run policy-lint --managed-capture-tool ruff --sandbox-mode required --sarif -- check lib/python/app.py
bin/coding-ethos-run policy-lint --sarif --replay .coding-ethos/lint-runs/<trace>.json
```

SARIF output is tuned for code-scanning ingestion: repository-relative artifact
URIs, stable rule IDs, run automation IDs, deterministic partial fingerprints,
ETHOS rule metadata, remediation skill IDs, and GitHub-compatible precision and
security-severity properties for findings that are actually security-relevant.
Record-only policy context stays in TOON/JSON traces and is not uploaded as
SARIF results. Pathless policy findings are also omitted from code-scanning
SARIF because GitHub requires every uploaded result to have at least one
location; coding-ethos keeps those aggregate findings in TOON/JSON traces
instead of inventing noisy alerts at `.` line 0.

Managed capture can request the Bubblewrap sandbox prototype with
`--sandbox-mode required`. Sandbox backend, profile, declared capabilities, and
denials are retained in lint traces and SARIF run properties so runtime
enforcement has the same audit trail as CEL and static-analysis findings.

The analyzer highlights unmapped tool/code pairs separately from ETHOS-backed
findings so real lint traces can drive the next evidence-map additions.
Replay renders the saved normalized result without invoking the underlying
linter, which makes bad agent output reproducible from a trace file.
Captured traces include emitted skill hints so later analysis can show which
ETHOS remediation playbooks are being suggested in real work.
Output quality is part of the contract: blocked results must not render empty
finding tables, absolute local paths, internal timing/group noise, or generic
guidance without at least one actionable finding. Golden-output tests should
cover normal lint failures, clean runs, invalid config, malformed tool output,
and tool crashes for every managed linter.

### Agent Hooks

Render or verify repo-local agent hook settings:

```bash
bin/coding-ethos-run agent-hooks print
bin/coding-ethos-run agent-hooks sync
bin/coding-ethos-run agent-hooks doctor
bin/coding-ethos-run agent-hooks verify
```

Agent hook generation is all-or-nothing. `sync` writes every supported
repo-local surface:

| Provider | Native file | Coverage |
| --- | --- | --- |
| Claude | `.claude/settings.local.json`, `.mcp.json` | full runtime hook set plus MCP stdio server |
| Codex | `.codex/config.toml` | native supported hook events plus MCP stdio server |
| Gemini CLI | `.gemini/settings.json` | native supported hook events plus MCP stdio server |

Codex runs one native command hook per supported event so current Codex
sessions enter the same policy runtime without depending on unstable tool
matcher names. Generated Codex config does not inline `PATH=` mutations,
installs explicit shell/edit matchers for tool hooks, and keeps lifecycle hooks
matcher-free. In nested checkouts, only the hook whose consumer root is the
nearest repo root enforces a Codex event, preventing duplicate parent/nested
reports.

The same sync path also installs the local `coding-ethos` MCP server for all
supported agents. Claude receives a project `.mcp.json` entry, Codex receives a
managed `[mcp_servers.coding-ethos]` block in `.codex/config.toml`, and Gemini
receives a `mcpServers.coding-ethos` entry in `.gemini/settings.json`. `doctor`
checks those entries along with hooks so MCP drift is not a separate hidden
setup step.

Generated ETHOS skills and native agent settings use the same managed-output
model. `make build` refreshes the checkout-local skill surfaces, hook settings,
and MCP settings and, when `coding-ethos` is installed inside a parent
repository, refreshes the parent repo's `.agents/skills/`, `.claude/skills/`,
`.codex/skills/`, Gemini extension skill surfaces, and native agent hook/MCP
settings without rewriting parent root agent docs.

`agent-hooks verify` runs doctor first, then invokes the configured hook command
with provider-native Claude, Codex, and Gemini payloads. The probes cover:

- Claude transparent Git wrapper rewrite
- Codex blocks for raw Git, absolute Git paths, nested shell Git, and Python
  subprocess Git when rewrite is unavailable
- Gemini deny responses for raw shell Git and write-tool policy denial
- managed hook-binary tampering:
  `rm ...coding-ethos-git-hook && go build -o ...coding-ethos-git-hook`

Hook logs under `.coding-ethos/hook-runs/` include stdout, stderr, metadata,
and a sanitized `event.json` for agent-hook executions. The trace records
provider, event, tool, cwd, referenced files, command preview and hash, policy
IDs, status, and output shape without dumping raw provider input.

### Cutover

Use cutover commands when preparing a repo to replace old hook surfaces:

```bash
bin/coding-ethos-run cutover install
bin/coding-ethos-run cutover verify
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
| Codex | `decision: "block"` plus `permissionDecision: "deny"` for `PreToolUse`; compact `reason` text for exit-code-2 stderr | compact native `additionalContext` for supported lifecycle/post-tool advice; compact `systemMessage` only where Codex exposes no `additionalContext` |
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
bin/coding-ethos-run policy-git --admin-approved commit -F /tmp/msg
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
| `coding_ethos/tool_configs.py` | generated repo-root tool config sync and drift checks |
| `coding_ethos/ci_tool_configs.py` | generated GitHub Actions and GitLab SARIF CI configs |
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
| ETHOS skill source or renderer behavior | `make build` |
| hook runtime or cutover behavior | `make cutover-verify` |

See [pre-commit/PRE-COMMIT.md](pre-commit/PRE-COMMIT.md) and
[pre-commit/hooks/HOOKS.md](pre-commit/hooks/HOOKS.md) for hook internals.
