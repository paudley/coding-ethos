<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Pre-Commit Hooks

This bundle provides ETHOS-oriented Git hooks built on
[Lefthook](https://github.com/evilmartians/lefthook). It supports two layouts:

- Source repo: `pre-commit/`
- Vendored/submodule repo: `code-ethos/pre-commit/`

The hook runners resolve either layout automatically. Consuming repos still
need a root-level `lefthook.yml` that points at this bundle, usually via a
symlink.

## Install

Run from the bundle repo root:

```bash
make install-hooks
```

In a consuming repo, run the same target from `code-ethos/`.

When `code-ethos/` is a submodule, the root `Makefile` resolves the parent
repo automatically and installs hooks into the parent repo's `.git/hooks`.

Before the hook shims are installed, `make install-hooks` also generates the
consumer repo's `pyrightconfig.json`, `mypy.ini`, `ruff.toml`,
`.yamllint.yml`, `.golangci.yml`, and `.code-ethos/gemini/prompt-pack.json` from the shared
bundle inputs plus any consuming-repo overrides.

`make install-hooks` installs a pinned repo-local Lefthook binary to:

```text
.git/coding-ethos-hooks/lefthook
```

The bundle does not install Lefthook into the host system and does not rely on
`lefthook` from `PATH`. It bootstraps the pinned binary with:

```bash
GOBIN=.git/coding-ethos-hooks go install \
  github.com/evilmartians/lefthook@$(cat pre-commit/lefthook.version)
```

Installed Git hooks use that repo-local binary only. If the binary is missing
or the cached version is stale, the hook shim rebuilds it into the same local
path and then runs it. The bundle always executes Lefthook with
`--no-auto-install` so runtime hook execution cannot replace the custom
`coding-ethos` shims in `.git/hooks/` with Lefthook's stock launcher.

Required tools:

- `go` 1.26 or newer
- `uv`
- `shellcheck`
- `hadolint`
- `actionlint`
- `golangci-lint`

Useful install commands:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Run

From the bundle repo root:

```bash
make pre-commit
make pre-commit-all
make pre-push
make validate
```

Run a single job directly:

```bash
cd "$(git rev-parse --show-toplevel)"
.git/coding-ethos-hooks/lefthook run --no-auto-install pre-commit --jobs "Ruff lint"
.git/coding-ethos-hooks/lefthook run --no-auto-install pre-commit --all-files --jobs "Validate YAML/TOML/JSON syntax"
```

Run commit-message checks directly:

```bash
tmp="$(mktemp)"
printf 'feat(hooks): update bundle\n' > "$tmp"
make commit-msg MSG="$tmp"
rm -f "$tmp"
```

Hook bypass is forbidden. Do not use `LEFTHOOK=0` or `--no-verify`.

## Layout

Primary files:

- `lefthook.yml` - hook stages, globs, excludes, and command templates
- `../Makefile` - root-level hook entry points and repo-local Lefthook bootstrap
- `lefthook.version` - single source of truth for the pinned Lefthook version
- `../config.yaml` - repo-root bundle policy and per-check defaults
- `../pyrightconfig.json`, `../mypy.ini`, `../ruff.toml`, `../.yamllint.yml`, `../.golangci.yml` - generated consumer-repo tool configs
- `../.code-ethos/gemini/prompt-pack.json` - generated consumer-repo Gemini prompt pack with rendered prompts and per-check runtime metadata
- `hooks/pyproject.toml` - Ruff, mypy, pyright, and tool dependency config for the hook project
- `hooks/run-go-hook.sh` - cached Go helper build and execution wrapper
- `hooks/run-lefthook.sh` - repo hook shim used for installed Git hooks
- `hooks/go-hooks/main.go` - Go-backed hook commands, including the active Gemini AI review runner

The active Go Gemini runner now executes file batches concurrently, applies
repo-local response caching under `.git/coding-ethos-hooks/gemini-cache/`,
supports per-check `model_overrides` and `service_tier_overrides`, reuses
Gemini `cachedContents` entries when the same batch corpus is reviewed by
multiple prompts, and can run `standard`, `flex`, or `priority` requests from
merged `config.yaml` plus `repo_config.yaml`.

The cached Go helper binary lives in `.git/coding-ethos-hooks/`. It rebuilds
when Go sources, `go.mod`, `go.sum`, or the repo-root `config.yaml` change.
The Lefthook binary is cached there as well, with a small version stamp file so
the bundle can refresh it locally when the pinned version changes.

## Configuration

Bundle defaults live in the code-ethos repo-root `config.yaml`. Consuming repos
can override them with one of these root-level files:

- `repo_config.yaml`
- `repo_config.yml`

You can also point the bundle at an explicit override file with
`CODE_ETHOS_PRECOMMIT_CONFIG`.

Legacy override names like `code-ethos.pre-commit.yaml` are still accepted, but
`repo_config.yaml` is the preferred consuming-repo entry point.

Lefthook runs the toolchain with `uv run --project code-ethos/pre-commit/hooks`
or `uv run --project pre-commit/hooks`, but Ruff, mypy, pyright, and yamllint
read their policy from the generated consumer-repo config files at the repo
root. The hook project `hooks/pyproject.toml` remains the isolated toolchain
environment. Parent `uv` workspace membership is optional, not required.

Important configurable areas:

- `style.*` - shared cross-cutting settings like Python version and line length; `style.python_version` also drives `pyupgrade`, generated tool configs, and repo-root version consistency checks
- `python.source_paths`, `python.test_paths`, `python.stub_paths`, `python.extra_paths` - shared repository layout inputs for generated tool configs
- `python.direct_imports` - public-package import enforcement
- `python.util_centralization` - banned direct utility imports and exemptions
- `python.sql_centralization` - centralized SQL module name and exempt paths
- `python.manifest_validation` - candidate manifest paths and required sections
- `python.plan_completion` - plan metadata filename, root markers, and done states
- `python.pytest_gate` - banned markers and pytest command
- `python.file_docstrings` - minimum sentence count and exempt filenames for file-level module docstrings
- `python.type_check` - checker commands, hook-project execution, config injection, enablement, and excluded path fragments
- `python.docstring_coverage` - interrogate command, threshold, path selection, exclude regexes, and ignore flags
- `tooling.pyright`, `tooling.mypy`, `tooling.ruff`, `tooling.yamllint`, `tooling.golangci_lint` - generated repo-root tool config defaults
- `gemini.*` - AI review enablement, model, concurrency, timeout, repo context, and modal allowlist file patterns
- `go.*` - commitlint, commit attribution, text policy, line limits, and quiet-filter rules

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

## Hook Inventory

Pre-commit includes:

- formatting and whitespace normalization
- pyupgrade autofix using the configured `style.python_version`
- syntax validation for YAML, TOML, and JSON
- merge-conflict, shebang, private-key, and large-file checks
- shell linting and shell best-practice enforcement
- direct-import, utility-centralization, SQL-centralization, and type-policy checks
- repo-root Python version consistency checks for `.python-version`, `pyproject.toml`, `mypy.ini`, `pyrightconfig.json`, and `ruff.toml`
- security, logging, dead-code, complexity, maintainability, and docstring checks
- optional manifest and plan workflow validation
- optional Gemini-powered ETHOS review
- optional Go vet/test/lint stages

Pre-push re-runs the higher-signal checks over the pushed diff, including full
Gemini review when enabled.

Most hook runtime and policy enforcement now lives in `hooks/go-hooks/`. The
remaining Python hook files are wrappers around external analyzers like Radon
and Vulture rather than bespoke policy implementations.

Commit-message hooks enforce:

- conventional commit structure with a required scope
- no AI attribution or promotional co-author lines

## Updating

To update Lefthook:

1. Change `pre-commit/lefthook.version`.
2. Change `min_version` in `pre-commit/lefthook.yml`.
3. Run:

```bash
make validate
make install-hooks
cd "$(git rev-parse --show-toplevel)"
.git/coding-ethos-hooks/lefthook validate
```

To update Go helper behavior:

```bash
make go-tidy
make go-test
```

## Adding Hooks

Use Go for generic file, shell, text, and commit-message checks that do not
need Python AST analysis or Python package imports. Keep the command in
`hooks/go-hooks/main.go` and the tunable policy in the repo-root `config.yaml`.

Use Python for checks that need AST parsing, type tooling, Python import
analysis, Gemini integration, or repository-specific policy modules.

For hooks that modify files:

- set `stage_fixed: true`
- keep `pre-commit.fail_on_changes: never`
- avoid stash-based workflows
- keep output quiet unless the hook fails
