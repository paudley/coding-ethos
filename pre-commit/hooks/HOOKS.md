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
