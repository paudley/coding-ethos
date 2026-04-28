# PyQA Lint Delta

## Overview

`pyqa_lint` is a general-purpose lint orchestration package. It owns tool
catalogs, command preparation, file discovery, diagnostics normalization,
reporting, hook installation, config inspection, runtime cache handling, and
remediation-oriented advice.

`coding-ethos` is narrower and more policy-centric. It generates agent-facing
ethos documents, repo tool configs, Gemini prompt packs, Go-backed git hooks,
and an emerging policy bundle/linter. The current `coding-ethos` hook runtime
already has strong policy checks, grouped hook execution, human/JSON/TOON output,
and agent-aware formatting. It does not yet have the full lint-orchestrator
substrate that `pyqa_lint` provides.

The useful delta is not to absorb `pyqa_lint` wholesale. The useful path is to
adopt selected orchestration patterns that make `coding-ethos lint` a richer,
policy-aware execution API for agents and hooks.

The long-term goal is to subsume the useful product idea behind `pyqa_lint`,
not to clone its foundation. `pyqa_lint` is tool-first: diagnostics are
collected and then advice tries to infer quality meaning. `coding-ethos` should
be policy-first: ETHOS principles define the engineering meaning, and linters,
type checkers, AST checks, hooks, and future analyzers provide evidence for or
against those principles.

The target pipeline is:

```text
ETHOS principle -> policy rule -> tool evidence -> normalized finding -> advice
```

Known tool messages should become stronger when they map to ETHOS. For example,
Ruff `PLC 0415` is not merely "import not at top of file"; in this repo it can
be evidence for `No Conditional Imports`, required-dependency validation, and
startup fail-fast behavior. The advice should explain that ETHOS violation and
give deterministic repair steps. Unknown or unmapped linter messages should
still flow through normally as plain diagnostics. ETHOS mappings enrich known
evidence; they do not filter out everything else.

## Feature Deltas

### Hook and lint orchestration

- `pyqa_lint` has a full `pyqa lint` command that builds structured CLI inputs,
  validates incompatible options, loads layered config, prepares runtime
  dependencies, runs an orchestrator, displays progress, and reports results.
- `pyqa_lint` supports explainable tool selection through `--explain-tools` and
  JSON export: order, tool, family, action, reasons, indicators, and
  descriptions.
- `pyqa_lint` models tool execution phases such as format, lint, analysis,
  security, test, coverage, and utility.
- `coding-ethos` has grouped Go hook commands and an early `coding-ethos-lint`
  policy evaluator with scopes (`files`, `changed`, `staged`, `smoke`, `full`),
  but it currently evaluates policy decisions rather than orchestrating external
  tools through a reusable selection/execution pipeline.

### Tool wrappers and runtime handling

- `pyqa_lint` has catalog-backed tool definitions for Python, JavaScript,
  TypeScript, Go, Rust, SQL, YAML, TOML, Markdown, Docker, shell, Lua, Perl, PHP,
  Kubernetes, and more.
- Tool definitions include runtime type, package, minimum version, version
  command, options, actions, parser strategy, diagnostics metadata, config files,
  file extensions, and auto-fix capability.
- `pyqa_lint` has a `CommandPreparer` that chooses Python, npm, Go, Lua, Perl,
  Rust, or binary runtime handlers and manages a deterministic tool cache layout.
- `coding-ethos` currently generates static configs for selected tools and runs
  explicit hook commands, but has no equivalent typed catalog or runtime
  preparation layer.

### Output formatting and diagnostics

- `pyqa_lint` normalizes heterogeneous raw diagnostics into a common model:
  file, line, column, severity, message, tool, code, group, function, hints,
  tags, and metadata.
- It distinguishes tool crashes from diagnostic exits with a `ToolExitCategory`
  rather than relying only on return code.
- It deduplicates diagnostics across tools using severity, code equivalence,
  semantic tags, location scope, and configurable preferences.
- It emits user-facing diagnostics through Rich and supports quiet/pretty output,
  Markdown, SARIF, JSON, and PR summary style reporting.
- `coding-ethos` already has useful hook report formats (`human`, `json`,
  `toon`) and LLM caller auto-detection, but its lint result schema is still
  policy-decision oriented and does not yet normalize external tool diagnostics.

### Config models and inspection

- `pyqa_lint` uses Pydantic config sections for clean, complexity, dedupe,
  execution, file discovery, license, output, quality, severity, strictness, and
  update settings.
- It supports layered configuration, strict validation, config show/validate,
  config diff, schema export, and tool-schema export.
- It applies sensitivity presets that update shared knobs and tool-specific
  settings while respecting explicitly supplied CLI options.
- `coding-ethos` has a YAML merge path for `config.yaml` plus repo overrides and
  deterministic generated tool configs. It lacks typed section models, config
  diff/trace, strict unknown-key validation, and schema export for the
  enforcement config.

### Agent and LLM output

- `pyqa_lint` has SOLID-oriented advice generation from normalized diagnostics,
  plus optional analysis providers using tree-sitter and spaCy.
- It can produce PR-summary style outputs and explain why tools were selected or
  skipped.
- `coding-ethos` is stronger on explicit agent surfaces: AGENTS/CLAUDE/GEMINI
  rendering, deep ethos docs, prompt-pack generation, policy bundle metadata,
  TOON output for agent callers, and planned agent-hook integration.
- The delta is that `coding-ethos` can explain policy and hook decisions, but
  does not yet provide pyqa-style diagnostic-to-remediation advice for regular
  lint failures.

### Remediation workflows

- `pyqa_lint` distinguishes check-only and fix-only flows, models auto-fixable
  tools, and defines fix actions in the catalog.
- It has `sparkly-clean` for cache/artifact cleanup with protected-path handling.
- It has install/update/tool-info/doctor/config commands that help users repair
  their local quality environment.
- `coding-ethos` has make targets, generated config drift checks, hook install
  scripts, policy validation, and Go tests. It does not yet have first-class
  remediation commands tied to lint findings.

## Gaps Worth Adopting

- A canonical lint result schema that combines policy decisions and tool
  diagnostics. Include check ID, policy source, severity, status, files, message,
  suggested fix, related ethos IDs, blocking/advisory flag, and raw tool outcome.
- Explainable selection for `coding-ethos lint`, similar to `pyqa lint
  --explain-tools`, but grounded in policy dispatch, hook groups, repo config,
  and file scope.
- Typed enforcement config models with strict validation and config trace/diff.
  This would reduce silent YAML drift and make repo overrides inspectable.
- Tool metadata catalog for the limited tool set `coding-ethos` actually owns:
  ruff, mypy, pyright, yamllint, golangci-lint, shellcheck, shfmt, hadolint,
  actionlint, Go tooling, pytest smoke gates, and Gemini checks.
- Diagnostic normalization for external hook tools. The current hook findings
  are useful, but parsers for tool-native JSON would enable consistent JSON/TOON
  output and remediation advice.
- A small remediation/advice layer that maps known findings back to ETHOS
  principles and concrete next commands while preserving unmapped findings as
  ordinary lint diagnostics.
- An ETHOS evidence-map section that says which linter codes, AST observations,
  type-checker diagnostics, or hook outcomes indicate each policy problem, with
  confidence, meaning, advice, and rerun commands.
- Tool/runtime environment checks exposed as `coding-ethos doctor` or through
  the existing Make doctor target with machine-readable output.

## Non-Goals / Not Relevant

- Do not import the whole `pyqa_lint` runtime into `coding-ethos`. Its scope is
  broader than this project and would blur the policy-bundle architecture.
- Do not replicate pyqa's full polyglot catalog. `coding-ethos` should cover
  tools it configures, invokes, or enforces.
- Do not add spaCy/tree-sitter analysis as a dependency just to generate advice.
  That is useful in `pyqa_lint`, but too heavy for a hook/policy bundle unless a
  specific policy needs it.
- Do not replace pre-commit or the Go hook runner with Typer orchestration.
  `coding-ethos` already has a good low-dependency Go enforcement boundary.
- Do not add auto-fix flows that mutate files from background agent hooks. Fix
  actions should be explicit and scoped.

## Concrete Implementation Candidates

1. Define `LintFinding` and `LintRunResult` in the Go linter package.
   Extend the current result beyond policy decisions with normalized fields:
   check ID, source tool, status, severity, code, file, line, column, message,
   suggestion, ethos IDs, and blocking flag.

2. Add `coding-ethos lint --explain`.
   Show which scope was selected, which policies/check groups are eligible,
   why each one runs or skips, and which command would execute. Emit both human
   and JSON/TOON forms.

3. Introduce a small checked-in tool catalog for owned tools.
   Start with static YAML or JSON under a `tools/` or `go/internal/toolcatalog`
   boundary. Include command, phase, file selectors, config files, parser kind,
   timeout, and whether the tool can fix.

4. Add parsers for the tools already used by hooks.
   Prioritize JSON-producing tools first: `golangci-lint`, `actionlint`,
   `hadolint`, and any ruff/pyright/mypy JSON paths. Convert output into
   `LintFinding` instead of printing tool-native blobs.

5. Add config validation and trace for `config.yaml` plus repo overrides.
   A first pass can validate known top-level sections and hook/tool settings
   before moving to full typed models.

6. Add a `lint-state` record for agent and git-wrapper enforcement.
   Store last successful scope, covered files, staged tree hash, commit SHA,
   failing check IDs, and generated config/prompt-pack hashes under a hook-owned
   path such as `.git/coding-ethos-hooks/lint-state/`.

7. Add a repo ignore checker.
   Verify that hook runtime output directories such as `.coding-ethos/` are
   ignored in both the bundle repo and consuming repo before hooks write logs or
   cache artifacts. This should become a normal policy check rather than tribal
   setup knowledge.
   First pass: `check-runtime-ignores` and the shell hook wrapper both enforce
   `.coding-ethos/` ignore coverage before hook logs are written.

8. Add policy-aware remediation hints.
   Map findings to ethos principle IDs and one concrete next command. Keep this
   deterministic: no LLM call should be required to explain a known lint failure.
   First pass: type-check diagnostics can be enriched from
   `policy.evidence_maps` while preserving unmapped diagnostics.

9. Add ETHOS evidence maps.
   Extend `coding_ethos.yml` / compiled policy data so tool evidence can map to
   policy IDs, principle IDs, confidence, meaning, advice steps, and rerun
   commands. Start with high-signal mappings such as Ruff `PLC 0415` to
   `No Conditional Imports`, then add mypy/pyright/ruff/golangci evidence where
   it clearly supports an ETHOS principle.

10. Add `coding-ethos doctor --json` or equivalent Go hook command.
   Report required tools, resolved config files, generated config drift status,
   prompt-pack drift status, hook group availability, and linter state freshness.

11. Keep `coding-ethos` output modes aligned.
   Any new lint/explain/doctor command should support human output for local use,
   JSON for scripts, and TOON for agent contexts through the existing output
   selection conventions.
