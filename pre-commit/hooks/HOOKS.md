<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# coding-ethos-hooks

Go-backed Git hooks for coding-ethos bundles.

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

The shell entry wrapper logs every top-level run to
`.coding-ethos/hook-runs/<run-id>/` in the checked repo, including
`stdout.log`, `stderr.log`, and `metadata.env`. That directory is local runtime
evidence and should stay ignored. `check-runtime-ignores` blocks hook execution
when required runtime output paths are not ignored, and `hook-log-summary`
summarizes collected runs for later analysis.

Known linter/type-checker diagnostics can map to ETHOS policy evidence through
`policy.evidence_maps`. Mapped findings receive policy-grounded advice in
human, JSON, and TOON output; unmapped findings keep their normal diagnostic
shape.

Agent settings rendering covers every supported provider:

```bash
pre-commit/hooks/run-go-hook.sh agent-hooks print
pre-commit/hooks/run-go-hook.sh agent-hooks sync
pre-commit/hooks/run-go-hook.sh agent-hooks doctor
```

Claude output uses Claude Code's native `hooks` map. Codex output enables
`[features].codex_hooks` in `.codex/config.toml` and writes native
`.codex/hooks.json`. Gemini output writes native `.gemini/settings.json`
hooks. All providers use the same `agent-hook` runtime entrypoint for the
lifecycle hooks they expose. Single-provider generation is intentionally not
exposed: partial protection is not a valid install state. The active runtime
does not call AI systems from agent hooks; AI review stays in Git hook stages
where output, cost, and caching are controlled by this runner. `agent-hooks
doctor` checks native provider activation files, so a stale file or missing
Codex feature flag does not count as an installed provider surface.

`agent-hook` normalizes provider payloads at the JSON boundary. Claude native
payloads (`hook_event_name`, `tool_name`, `tool_input`, `tool_response`) remain
supported. Codex and Gemini CLI integrations should use the first-class
provider-neutral payload shape:

```json
{"provider":"codex","event":"PreToolUse","tool":"Bash","input":{"command":"git status"}}
```

CamelCase hook fields, Gemini `BeforeTool` payloads, and nested
`tool_call.name`/`tool_call.arguments` are accepted for CLI adapters that expose
those shapes. After normalization, provider events run through the same policy
bundle and receive the same blocking, rewrite, advice, continuation, and
post-tool feedback behavior where the provider exposes that lifecycle point.

## Included Hooks

- **go-hooks/** - Fast generic file checks, shell checks, commitlint, commit
  attribution, direct-import enforcement, utility and SQL centralization, file
  and module doc checks, type-check orchestration, Python quality wrappers,
  Dockerfile and workflow validation, Go toolchain checks, pytest gating,
  pyproject ignore enforcement, repo-root Python version consistency checks,
  shared hook policy, and the active Gemini AI review runner
- **check_complexity.py** - Cyclomatic complexity checks via Radon
- **check_maintainability.py** - Maintainability index checks via Radon
- **check_vulture.py** - Dead-code detection via Vulture

Most active policy enforcement lives in `go-hooks/`. The Python hook files are
kept for analyzer integrations that are naturally Python-based or already use
Python tooling.

## Installation

Install the Go hook shims from the repository root that exposes the bundle:

```bash
cd /path/to/repo
make -C code-ethos install-hooks
```

## Dependencies

- pyyaml >= 6.0
- go >= 1.26
- uv
- shellcheck, hadolint, actionlint, and golangci-lint for their corresponding
  hook groups

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
tidying module metadata. When changing analyzer wrappers, keep wrapper behavior
narrow and prefer structural fixes over broad suppressions.
