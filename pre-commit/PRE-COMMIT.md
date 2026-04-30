<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Pre-Commit Hooks

This bundle provides ETHOS-oriented Git hooks through the bundled Go runner.
The bundle supports two layouts:

- Source repo: `pre-commit/`
- Vendored/submodule repo: `code-ethos/pre-commit/`

The hook runners resolve either layout automatically. Installed Git hooks call
the Go runner directly.

## Install

Run from the bundle repo root:

```bash
make install-hooks
make cutover-install
make cutover-verify
```

In a consuming repo, run the same target from `code-ethos/`.

When `code-ethos/` is a submodule, the root `Makefile` resolves the parent
repo automatically and installs hooks into the parent repo's `.git/hooks`.

Before the hook shims are installed, `make install-hooks` also generates the
consumer repo's `pyrightconfig.json`, `mypy.ini`, `ruff.toml`, `.pylintrc`,
`.yamllint.yml`, `.golangci.yml`, and
`.code-ethos/gemini/prompt-pack.json` from the shared bundle inputs plus any
consuming-repo overrides.

`make install-hooks` installs small `.git/hooks/pre-commit`, `pre-push`, and
`commit-msg` shims that execute `pre-commit/hooks/run-go-hook.sh git-hook ...`.
The installed Go helper binaries and compiled policy bundle live under
`.git/coding-ethos-hooks/`. Normal hook execution does not rebuild these
artifacts; run `make build` from the coding-ethos repository to update the
installed runtime.

`make cutover-install` installs the Git hook shims, syncs Claude, Codex, and
Gemini repo-local agent hook settings, and then verifies the full cutover
surface. `make cutover-verify` checks the installed Git shims, runs
`agent-hooks verify`, verifies required runtime ignores through the compiled
`repo.required_ignores` policy, runs the policy runtime validation hook,
and prints a concise TOON readiness report. Blocked reports include `fix_first`
entries that name the stale or missing surface and the next action.

Each top-level hook runner invocation logs stdout, stderr, and run metadata
under `.coding-ethos/hook-runs/<run-id>/` in the repo being checked. Keep
`.coding-ethos/` ignored in both the bundle repo and consuming repos; it is
runtime evidence for later analysis, not source. The cutover gate reports
missing ignore rules before installation, and normal hook execution still fails
before writing logs when `.coding-ethos/` is not ignored.

Required tools:

- `go` 1.26 or newer
- `uv`
- `shellcheck`
- `shfmt`
- `hadolint`
- `actionlint`
- `golangci-lint`

Useful install commands:

```bash
go install mvdan.cc/sh/v3/cmd/shfmt@latest
go install github.com/rhysd/actionlint/cmd/actionlint@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Run

From the bundle repo root:

```bash
make doctor
make pre-commit
make pre-commit-all
make pre-push
make validate
make hook-plan
```

Run a single group directly:

```bash
cd "$(git rev-parse --show-toplevel)"
pre-commit/hooks/run-go-hook.sh run-group python-static path/to/file.py
pre-commit/hooks/run-go-hook.sh run-group syntax path/to/config.yaml
```

Run commit-message checks directly:

```bash
tmp="$(mktemp)"
printf 'feat(hooks): update bundle\n' > "$tmp"
make commit-msg MSG="$tmp"
rm -f "$tmp"
```

Hook bypass is forbidden. Do not use `--no-verify`.

## Layout

Primary files:

- `../Makefile` - root-level hook entry points and Git hook installation
- `../config.yaml` - repo-root bundle policy and per-check defaults
- `../pyrightconfig.json`, `../mypy.ini`, `../ruff.toml`, `../.pylintrc`,
  `../.yamllint.yml`, `../.golangci.yml` - generated consumer-repo tool
  configs
- `../.code-ethos/gemini/prompt-pack.json` - generated consumer-repo Gemini
  prompt pack with rendered prompts and per-check runtime metadata
- `hooks/pyproject.toml` - Ruff, mypy, pyright, and tool dependency config for the hook project
- `hooks/run-go-hook.sh` - cached Go helper build and execution wrapper
- `hooks/run-git-hook.sh` - installed Git hook shim
- `hooks/go-hooks/main.go` - Go-backed hook commands, including the active Gemini AI review runner

The active Go Gemini runner now executes file batches concurrently, applies
repo-local response caching under `.git/coding-ethos-hooks/gemini-cache/`,
supports per-check `model_overrides` and `service_tier_overrides`, reuses
Gemini `cachedContents` entries when the same batch corpus is reviewed by
multiple prompts, and can run `standard`, `flex`, or `priority` requests from
merged `config.yaml` plus `repo_config.yaml`.

The hook runtime lives in `.git/coding-ethos-hooks/`. It is updated by explicit
build/install targets, not by normal hook execution. If the runtime is missing
or stale, hooks fail with an instruction to run `make build` or ask an admin to
update the installed runtime.

The same wrapper also exposes local policy-runtime entrypoints:

```bash
pre-commit/hooks/run-go-hook.sh agent-hook
pre-commit/hooks/run-go-hook.sh agent-hooks print
pre-commit/hooks/run-go-hook.sh agent-hooks sync
pre-commit/hooks/run-go-hook.sh agent-hooks doctor
pre-commit/hooks/run-go-hook.sh agent-hooks verify
pre-commit/hooks/run-go-hook.sh cutover install
pre-commit/hooks/run-go-hook.sh cutover verify
pre-commit/hooks/run-go-hook.sh policy-lint --staged
pre-commit/hooks/run-go-hook.sh policy-git --check-only commit -m test
pre-commit/hooks/run-go-hook.sh hook-log-analyze
pre-commit/hooks/run-go-hook.sh hook-log-summary
```

`agent-hook` reads agent hook JSON from stdin and never calls Gemini. Gemini
checks stay in the Git hook stages: changed-file review on pre-commit and
full review on pre-push.

`agent-hooks print|sync|doctor|verify` always covers every supported agent
surface.
There is no single-agent generation path because partial protection is not a
valid install state. Claude output uses Claude Code's native `hooks` map.
Codex output enables `[features].codex_hooks` in `.codex/config.toml`, writes
managed native `[hooks]` entries in that same TOML file, and removes stale
`.codex/hooks.json`. Gemini output writes native `.gemini/settings.json` hooks
with `hooksConfig.enabled = true`. `doctor` verifies those native activation
files and fails when a provider does not point at the expected hook command.
Codex hook generation uses one native command hook per supported lifecycle
event, while the runtime normalizes aliases such as `exec_command`,
`run_shell_command`, `shell`, `write_file`, and `apply_patch`; shell and edit
policy does not depend on a single Codex tool name spelling.
`verify` executes provider-shaped runtime probes through the configured hook
command after `doctor` succeeds. It proves the settings point at a runnable
policy path; it is not a substitute for a real provider binary executing a live
tool call.

`agent-hook` accepts each provider's supported event shape and normalizes it
before policy evaluation. Claude may send native `hook_event_name`,
`tool_name`, `tool_input`, and `tool_response` fields. Codex and Gemini CLI
callers should send the provider-neutral shape:

```json
{
  "provider": "gemini-cli",
  "event": "PreToolUse",
  "tool": "Bash",
  "input": {"command": "git status"}
}
```

The decoder also accepts camelCase hook fields (`hookEventName`, `toolName`,
`toolInput`, `toolResponse`, `exitCode`), Gemini's `BeforeTool`,
`run_shell_command`, and `write_file` names, Codex shell aliases
(`exec_command`, `run_command`, `run_shell`, `run_shell_command`, `shell`,
`shell_command`), and nested Codex-style `tool_call.name` plus
`tool_call.arguments`. Provider identity does not weaken policy: the same git
wrapper, filesystem, Python-edit, continuation, and post-tool output rules
apply wherever the provider exposes the corresponding lifecycle hook.

Tamper and bypass blocks are intentionally louder than normal lint findings.
Direct attempts to inspect, delete, rebuild, replace, chmod, or write managed
hook binaries under `.git/coding-ethos-hooks/` are treated as employment
violations. Agent-facing output starts with a uniform
`CODING-ETHOS EMPLOYMENT VIOLATION` warning before the policy-specific message,
states that the actor has done something wrong, and warns that continued
circumvention attempts may result in termination.

Hook responses are provider-aware. Claude keeps the full `hookSpecificOutput`
contract, including `updatedInput` for transparent git-wrapper rewrites. Codex
does not currently support `updatedInput`, so coding-ethos returns native block
output (`decision: "block"` plus `permissionDecision: "deny"`) when a raw git
command must be rerun through the wrapper. Gemini uses native
`decision: "deny"` and `systemMessage` for tool blocks, and maps `AfterTool` to
the same internal `PostToolUse` feedback path for shell and edit advice.
Agent-facing post-tool context replaces absolute repo, home, and temp paths
with stable tokens, collapses multiline commands, and renders hook output as
TOON line tables instead of escaped newline cells.

Post-edit feedback for `Write`, `Edit`, and `MultiEdit` includes focused context,
language-specific advice, compiled lint findings for the edited files, and a
fast Ruff probe for Python files when `ruff` is available. Expensive external
tool suites still belong to the Git hook/check path.
Fast Ruff findings use the same ETHOS evidence maps as captured lint output,
so known codes carry policy-grounded repair advice in post-edit context.
When captured lint history has relevant failures for the same file area,
post-edit feedback also surfaces recurring checks, recurring tool/code pairs,
and unmapped tool/code pairs that still need ETHOS evidence-map coverage.

`hook-log-summary` summarizes `.coding-ethos/hook-runs/` and `hook-log-analyze`
ranks failed tools, codes, repeated findings, and output-quality problems such
as raw output, escaped newline cells, or leaked absolute repo paths. The
analyzer scans newest runs first and caps scanned runs plus examples so it stays
interactive on large agent log directories. Both commands honor the same human,
JSON, and TOON output selection as hook execution output.
Compiled lint preflights also persist normalized result traces under
`.coding-ethos/lint-runs/`. These are intended for offline trend analysis:
which policies fail most often, which linter codes drive the most churn, and
which ETHOS-backed advice should become more specific. Future guidance synthesis
may use a very small LLM or local model over these traces, but hook-time
enforcement remains deterministic and policy-bundle driven.
Analyze those traces with:

```bash
pre-commit/hooks/run-go-hook.sh policy-lint --analyze-log
pre-commit/hooks/run-go-hook.sh policy-lint --analyze-log --for-files lib/python/app.py
pre-commit/hooks/run-go-hook.sh policy-lint --analyze-log --json
```

The analyzer reports top failing checks, top tool/code pairs, unmapped
tool/code pairs, repeated file-policy patterns, ETHOS IDs, and deterministic
guidance candidates. The
`--for-files` filter narrows output to prior findings from the same file or
same high-level file area so post-edit feedback can stay focused.
Direct agent lint runs are captured too. The agent hook rewrites common forms
for `ruff`, `mypy`, `pyright`, `pylint`, `shellcheck`, `golangci-lint`,
`actionlint`, `yamllint`, and `hadolint` to the managed
`policy-tool <tool>` wrapper when the provider supports command rewrites. This
covers plain tool names, absolute tool paths, `uv run <tool>`, and
`python -m <tool>` for Python-backed tools. The installed hook PATH also
contains managed shims for tools that execute by name. Captured runs preserve
exit codes while forcing machine-readable tool output, parsing diagnostics into
the shared lint schema, enriching known findings with ETHOS evidence-map advice,
writing normalized lint traces, and returning coding-ethos human or TOON output
instead of raw linter output.

## Configuration

Bundle defaults live in the code-ethos repo-root `config.yaml`. Consuming repos
can override them with one of these root-level files:

- `repo_config.yaml`
- `repo_config.yml`

You can also point the bundle at an explicit override file with
`CODE_ETHOS_PRECOMMIT_CONFIG`.

Use `pre-commit/hooks/run-go-hook.sh policy config-trace --json` after
enforcement config edits to validate known top-level sections, compile the
merged policy bundle, and report policy/evidence/dispatch counts.

Legacy override names like `code-ethos.pre-commit.yaml` are still accepted, but
`repo_config.yaml` is the preferred consuming-repo entry point.

License enforcement is intentionally not inherited from the bundle defaults.
Consumers opt in with `repo.license.spdx_identifier` in `repo_config.yaml`; the
compiler downloads that SPDX license text into the policy bundle, the hook
verifies `LICENSE` without overwriting it, and source files must carry the
configured SPDX license and copyright headers. The same compiled file-policy
path also enforces configured PII scrub patterns and required runtime ignore
paths such as `.coding-ethos/`.

Generated config drift is checked with:

```bash
make check-tool-configs
make check-gemini-prompts
```

Regenerate derived files with:

```bash
make sync-tool-configs
make sync-gemini-prompts
```

The Go runner invokes Python tools with
`uv run --project code-ethos/pre-commit/hooks` or
`uv run --project pre-commit/hooks`. Ruff, mypy, pyright, pylint, yamllint, and
golangci-lint read policy from the generated consumer-repo config files at the
repo root. The hook project `hooks/pyproject.toml` remains the isolated
toolchain environment. Parent `uv` workspace membership is optional, not
required.

Known external diagnostics can be enriched through `policy.evidence_maps`.
Mapped findings keep their raw tool, code, location, severity, and message, then
add policy ID, principle IDs, confidence, meaning, advice, and rerun commands.
Unmapped diagnostics still flow through as ordinary lint findings.
Type-checker evidence maps cover common mypy, Pyright, and Pylint findings for
optional required dependencies, unknown type leakage, missing imports, unstable
interfaces, and import cycles. Those findings point back to ETHOS guidance such
as Protocol-first design, fail-fast required imports, and structural fixes
instead of lazy imports or broad suppressions.
When multiple tools report the same mapped policy at the same location, the
lint result keeps one actionable finding and records the secondary tool/code in
the finding detail. This keeps agent context focused on the repair instead of
repeating equivalent diagnostics.

Important configurable areas:

- `style.*` - shared cross-cutting settings like Python version and line
  length; `style.python_version` also drives `pyupgrade`, generated tool
  configs, and repo-root version consistency checks
- `python.source_paths`, `python.test_paths`, `python.stub_paths`,
  `python.extra_paths` - shared repository layout inputs for generated tool
  configs
- `python.direct_imports` - public-package import enforcement
- `python.util_centralization` - banned direct utility imports and exemptions
- `python.sql_centralization` - centralized SQL module name and exempt paths;
  test paths are exempt from SQL centralization enforcement
- `python.manifest_validation` - candidate manifest paths and required sections
- `python.plan_completion` - plan metadata filename, root markers, and done states
- `python.pytest_gate` - banned markers and pytest command
- `python.file_docstrings` - minimum sentence count and exempt filenames for file-level module docstrings
- `python.type_check` - aggregated Ruff, mypy, pyright, and optional pylint
  command execution, hook-project execution, config injection, per-checker
  enablement, and excluded path fragments
- `python.docstring_coverage` - interrogate command, threshold, path selection, exclude regexes, and ignore flags
- `hooks.*` - normalized output format, agent environment detection,
  external tool timeout, severity thresholds, and canonical hook groups
- `tooling.pyright`, `tooling.mypy`, `tooling.ruff`, `tooling.pylint`,
  `tooling.yamllint`, `tooling.golangci_lint` - generated repo-root tool config
  defaults, including the expanded Go security, dependency, module-directive,
  modern-library, protobuf, test, and whitespace/style linter policy
- `gemini.*` - AI review enablement, model, concurrency, timeout, repo context, and modal allowlist file patterns
- `go.*` - compiled commitlint and commit attribution policy, text policy,
  line limits, and quiet-filter rules

Agent-facing hook feedback should render from normalized diagnostics instead of
raw tool output. `CODE_ETHOS_HOOK_OUTPUT_FORMAT=human|json|toon|auto` controls
structured hook reports; `auto` selects TOON when common agent caller
environment markers are present and otherwise keeps the human terminal report.
Failed grouped hook runs emit a runner-owned execution summary before captured
tool output, including group status, duration, failed groups, and per-command
timing for in-process group execution.
Go-owned policy checks, Python static checks, Gemini AI checks, docstring
coverage, shellcheck/yamllint, and external analyzer orchestration use this
normalized report path.
Pylint config is generated as `.pylintrc`, but the Pylint checker is disabled
by default in `python.type_check.checkers`; re-enable it per repo after the
local `.pylintrc` policy has been reviewed.
The canonical Go-owned hook groups are `format`, `syntax`, `python-policy`,
`python-quality`, `python-static`, `docs`, `security`, `shell`, `docker`,
`workflow`, `go`, and `ai`; compiled policy preflight owns commit message
behavior. Run `make hook-plan` to print the active group and command plan.

For this repo, many project-specific checks are disabled by default because the
codebase does not have SQL centralization, manifest, plan, or Go worktree
requirements. Consuming repos enable and tune those checks in their override
config.

Typical consuming-repo overrides include:

- `style.line_length` for line-length policy shared across Ruff and yamllint
- `python.source_paths`, `python.test_paths`, and `python.stub_paths` for nested layouts like `lib/python/tests`
- `python.direct_imports.packages` for the repo's public package names
- `python.pytest_gate.test_command` for nonstandard test roots like `lib/python/tests`
- `python.file_docstrings.min_sentences` for stricter module-level docstring requirements
- `python.docstring_coverage.check_paths` for nested source trees
- `python.docstring_coverage.ignore_private` / `ignore_nested_classes` when a repo wants stricter coverage
- `python.type_check.excluded_path_fragments` for generated or container-specific Python trees
- `python.sql_centralization` and `python.util_centralization` for repo-specific wrapper modules
- `gemini.modal_allowlist_files` for repo-configured file-level modal waivers instead of inline source comments

See [../repo_config.example.yaml](../repo_config.example.yaml) for a minimal
consumer-repo override file.

Policy lint selection can be inspected without running checks:

```bash
pre-commit/hooks/run-go-hook.sh policy-lint --scope staged --explain
pre-commit/hooks/run-go-hook.sh policy-lint --scope staged --explain --json
```

The explain output reports the selected policy checks, evaluator names,
severity, ETHOS IDs, hook-owned tool selection, and the active evidence maps
that turn external linter codes/messages into ETHOS-backed policy advice.

## Hook Inventory

Pre-commit includes:

- formatting and whitespace normalization
- pyupgrade autofix using the configured `style.python_version`
- syntax validation for YAML, TOML, and JSON
- merge-conflict, shebang, private-key, and large-file checks
- shell linting and shell best-practice enforcement
- direct-import, utility-centralization, SQL-centralization, and type-policy checks
- repo-root Python version consistency checks for `.python-version`,
  `pyproject.toml`, `mypy.ini`, `pyrightconfig.json`, and `ruff.toml`
- security, logging, dead-code, complexity, maintainability, and docstring checks
- optional manifest and plan workflow validation
- optional Gemini-powered ETHOS review
- optional Go vet/test/lint stages

Pre-push re-runs the higher-signal checks over the pushed diff, including full
Gemini review when enabled.

Most hook runtime and policy enforcement now lives in `hooks/go-hooks/`. Python
quality checks call Radon and Vulture directly from the Go runner, and shell,
YAML, Docker, workflow, Go, and AI analyzer output is normalized there as well.

Commit-message hooks enforce:

- conventional commit structure with a required scope
- no AI attribution or promotional co-author lines

## Updating

To update Go helper behavior:

```bash
make go-tidy
make go-test
```

`make go-fmt` formats every Go source file under
`pre-commit/hooks/go-hooks/`. Override `GO=/path/to/go` or
`GOFMT=/path/to/gofmt` when testing with a non-default toolchain.

## Adding Hooks

Use Go for generic file, shell, text, and commit-message checks that do not
need Python AST analysis or Python package imports. Keep the command in
`hooks/go-hooks/main.go` and the tunable policy in the repo-root `config.yaml`.

Use Python for checks that need AST parsing, type tooling, Python import
analysis, or repository-specific policy modules.

For hooks that modify files:

- set `stage_fixed: true`
- keep `pre-commit.fail_on_changes: never`
- avoid stash-based workflows
- keep output quiet unless the hook fails
