<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos-hooks

Go-backed Git hooks for coding-ethos bundles.

Installed consumer repository shims are intentionally thin. They discover the
consumer repo, locate the checked-out `coding-ethos` bundle, repair missing
checkout-local runtime artifacts with `make -C <coding-ethos> build`, and
dispatch to binaries under `coding-ethos/bin/`. Policy selection and strict
policy freshness checks stay inside the `coding-ethos` checkout; lifecycle
hooks do not use a consumer `.git/coding-ethos-hooks` runtime cache.

The Go runner is the output-control layer for Git hooks. It supports
`hooks.output_format` values of `auto`, `human`, `json`, and `toon`; `auto`
selects TOON when known agent/LLM environment markers are present. Successful
groups are silent by default through `hooks.success_output: silent`, while
`hooks.success_output: verbose` restores operator-facing pass summaries. When
`hooks.parallel_groups: true`, enabled groups run concurrently as isolated hook
subprocesses and their captured output is replayed in deterministic group order
only when a group fails or verbose success output is enabled.
Failed grouped runs also emit a compact execution summary before raw tool
detail: group status, duration, failed groups, and command timing where
commands ran in-process.

The shell entrypoint delegates top-level run logging to the managed
`coding-ethos-hook-log` Go tool. It writes
`.coding-ethos/hook-runs/<run-id>/` in the checked repo, including `stdout.log`,
`stderr.log`, and `metadata.env`. That directory is local runtime evidence and
should stay ignored. `check-runtime-ignores` blocks hook execution when required
runtime output paths are not ignored, and `hook-log-summary`
summarizes collected runs for later analysis. `hook-log-analyze` ranks failed
tools, codes, repeated findings, and output-quality problems such as raw output,
escaped newline cells, or leaked absolute repo paths. Analysis scans the newest
hook runs first and caps both scanned runs and examples so it remains usable on
large real-world log directories.

Known linter/type-checker diagnostics can map to ETHOS policy evidence through
`policy.evidence_maps`. Mapped findings receive policy-grounded advice in
human, JSON, and TOON output; unmapped findings keep their normal diagnostic
shape.
The compiled lint path also writes normalized JSON traces under
`.coding-ethos/lint-runs/` for later analysis. Those traces keep the full
policy decision, normalized finding, diagnostic, and evidence payloads out of
agent-facing output while preserving enough data to identify recurring lint
failures and improve deterministic guidance.
Use `policy-lint --analyze-log` to rank top failing checks, top tool/code
pairs, repeated file-policy patterns, ETHOS IDs, and guidance candidates. Add
`--for-files path/to/file.py` to filter the analysis to prior findings from the
same file or file-area pattern.
Agent shell commands that invoke common lint tools are routed through the
managed lint capture wrapper. Captured tools currently include `ruff`, `mypy`,
`pyright`, `pylint`, `shellcheck`, `golangci-lint`, `actionlint`, `yamllint`,
`hadolint`, `bandit`, `sqlfluff`, `tombi`, and `dotenv-linter`. Plain tool
calls, absolute tool paths, `uv run <tool>`, and
`python -m <tool>` for Python-backed tools are normalized to
`coding-ethos-run policy-tool <tool> ...` when the provider supports command
rewrites; unsupported providers must use the managed shims injected into the
hook PATH. The wrapper owns output formatting: it forces the tool's
machine-readable output option, parses diagnostics into the shared lint schema,
enriches known findings with ETHOS evidence-map advice, records them in the
lint trace log, and prints coding-ethos human or TOON output instead of raw
tool output.
Raw Python execution is also normalized when the consumer repo has a Python
environment. Hooks prepend `<repo>/.venv/bin` after coding-ethos-managed
directories, and Claude shell commands using `python`, `python3`, or
`python3.x` are rewritten to `uv run --project <repo> python ...` when the repo
has `uv.lock` or `pyproject.toml`; otherwise they are rewritten to
`<repo>/.venv/bin/python ...` when that interpreter exists. Providers that
cannot accept command rewrites are blocked and must invoke the documented repo
Python command directly.
Post-edit advice uses the same traces quietly: when a touched file has relevant
prior lint failures, hooks surface a capped `lint_history` section with the top
three recurring checks, top three tool codes, and top two guidance candidates.
No history section is emitted when there is no relevant captured history.

Agent settings rendering covers every supported provider:

```bash
bin/coding-ethos-run agent-hooks print
bin/coding-ethos-run agent-hooks sync
bin/coding-ethos-run agent-hooks doctor
bin/coding-ethos-run agent-hooks verify
bin/coding-ethos-run cutover install
bin/coding-ethos-run cutover verify
```

Claude output uses Claude Code's native `hooks` map. Codex output enables
`[features].codex_hooks` in `.codex/config.toml`, writes managed native
`[hooks]` entries in that same TOML file, and removes stale `.codex/hooks.json`.
It also writes each provider's native MCP surface: Claude project `.mcp.json`,
Codex `[mcp_servers.coding-ethos]`, and Gemini `mcpServers.coding-ethos`.
Gemini output writes native `.gemini/settings.json` hooks with
`hooksConfig.enabled = true`. All providers use the same `agent-hook` runtime
entrypoint for lifecycle hooks and the same `mcp` runtime entrypoint for MCP.
Single-provider generation is intentionally not exposed: partial protection is
not a valid install state. The active runtime does not call AI systems from
agent hooks; AI review stays in Git hook stages where output, cost, and caching
are controlled by this runner.
`agent-hooks doctor` checks native provider activation files, so a stale file or
missing Codex feature flag does not count as an installed provider surface.
`agent-hooks verify` additionally executes provider-native smoke payloads
through the configured hook command, proving that Claude rewrites, Codex blocks
raw git, absolute git, nested shell git, and Python subprocess git, and Gemini
denies reach the active runtime.
This is settings plus runtime-probe verification, not proof that the real
provider binary executed an end-to-end tool call.
`cutover install` installs Git hook entrypoints, syncs all agent settings, and then
runs the readiness gate. `cutover verify` is read-only and reports Git hook,
agent hook, repo-ignore, and policy runtime readiness in TOON. Required runtime
ignore checks run through the compiled `repo.required_ignores` policy.
Blocked reports include `fix_first` rows naming the stale or missing hook
surface and the next action.

`agent-hook` normalizes provider payloads at the JSON boundary. Claude native
payloads (`hook_event_name`, `tool_name`, `tool_input`, `tool_response`) remain
supported. Codex and Gemini CLI integrations should use the first-class
provider-neutral payload shape:

```json
{"provider":"codex","event":"PreToolUse","tool":"Bash","input":{"command":"git status"}}
```

CamelCase hook fields, Gemini `BeforeTool` / `AfterTool` payloads, and nested
`tool_call.name`/`tool_call.arguments` are accepted for CLI adapters that expose
those shapes. Codex native shell aliases (`exec_command`, `run_command`,
`run_shell`, `run_shell_command`, `shell`, `shell_command`) normalize to the
internal `Bash` tool, and edit aliases (`apply_patch`, `edit_file`) normalize
to `Edit`. After normalization, provider events run through the same policy
bundle and receive the same blocking, rewrite, advice, continuation, and
post-tool feedback behavior where the provider exposes that lifecycle point.
Provider output is adapted at the boundary: Claude receives full
`hookSpecificOutput` including `updatedInput`, Codex receives native block
output with compact `reason` text plus compact native `additionalContext` for
supported lifecycle and post-tool advice, and Gemini receives native `deny` /
`systemMessage` responses for tool gates. Codex uses compact `systemMessage`
only for supported events that do not expose `additionalContext`.

Codex generation follows four invariants:

- generated commands do not inline `PATH=` or other shell environment mutation;
- `PreToolUse` and `PostToolUse` use explicit shell/edit matchers, never a
  catch-all matcher;
- lifecycle hooks install one command hook each and no tool matcher; and
- nested checkouts only enforce the hook owned by the nearest repo root, so a
  parent repo and nested `coding-ethos` checkout cannot both report the same
  Codex event.

Trusted `coding-ethos-run` handling is exact-path based. A command is treated as
managed only when it invokes the generated relative hook path or the exact hook
path exported by the active runtime; a different executable with the same
filename suffix is still blocked.

Hook logs under `.coding-ethos/hook-runs/` include `stdout.log`, `stderr.log`,
`metadata.env`, and a sanitized `event.json` for agent-hook executions. The JSON
trace records provider, event, tool, cwd, referenced files, command preview and
hash, decision policy IDs, status, and output shape without dumping raw provider
input.
Agent-facing post-tool context
normalizes absolute repo, home, and temporary paths, collapses multiline
commands, and renders hook output as TOON line tables instead of giant escaped
string cells. Post-edit feedback for
`Write`, `Edit`, and `MultiEdit` includes a checkpoint, language-specific next
steps, compiled lint findings, and a fast Ruff probe for Python files when
`ruff` is available.

## Included Hooks

- **go-hooks/** - Compiled policy preflight, shell checks, direct-import
  enforcement, utility and SQL centralization, file
  and module doc checks, type-check orchestration, Python quality orchestration,
  Dockerfile and workflow validation, Go toolchain checks, pytest gating,
  compiled `python.pyproject_ignores` enforcement, repo-root Python version
  consistency checks, shared hook policy, and the active Gemini AI review runner

Cheap deterministic checks such as syntax parsing, merge-conflict markers,
private-key detection, shebang consistency, large-file limits, line limits, and
shell best practices now run through compiled policy preflight. Repo-specific
PII, required-ignore, and license/copyright policies also run through the
compiled policy bundle when configured. The remaining bundled groups are either
richer repo-structure checks or direct external analyzer orchestration from the
Go runner.

Runtime bootstrap is progressively moving out of shell. Strict policy metadata
source-hash validation is performed by `coding-ethos-policy
validate-metadata`, and managed GitHub release download, digest verification,
archive extraction, and binary installation are performed by the compiled
`coding-ethos-toolchain` helper. The same helper owns managed-toolchain
manifest parsing and installed-manifest generation, Git wrapper shim
generation, Git hook shim install/verify reporting, and cutover report
rendering so those security-sensitive writes and status surfaces are tested Go
behavior instead of ad hoc shell text generation.

## Installation

Install the Go hook entrypoints from the repository root that exposes the bundle:

```bash
cd /path/to/repo
make -C code-ethos install-hooks
```

## Dependencies

- pyyaml >= 6.0
- go >= 1.26
- uv
- shellcheck, shfmt, hadolint, actionlint, dotenv-linter, and golangci-lint for their
  corresponding hook groups

## Development

Bundle policy now comes from the repo-root `config.yaml` plus an optional
consumer-root `repo_config.yaml`. Generated tool configs live at the consumer
repo root, and the Go hook runner reads that merged policy directly.

Primary development commands:

```bash
make doctor
make validate
make go-fmt
make go-test
make pre-commit-all
```

When adding or changing Go-backed checks, keep tunable policy in `config.yaml`
and add or update Go tests in this directory. `make go-fmt` formats every Go
source file in `go-hooks/`, and `make go-tidy` runs that formatter before
tidying module metadata. Shell files in this directory are bootstrap shims only;
new check behavior belongs in the Go runner.
