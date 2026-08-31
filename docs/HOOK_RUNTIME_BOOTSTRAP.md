<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Hook Runtime Bootstrap Model

## Decision

The consumer repository hook shim must not own policy behavior, and it must not
point at a worktree that can be retired while sibling worktrees still use the
same Git common directory.

Its job is limited to:

- identify the hook kind
- dispatch to the compiled runner installed in the repository's stable common
  Git runtime

Policy evaluation, policy freshness, generated prompt packs, runtime command
selection, managed capture, diagnostics parsing, and hook behavior belong to
the compiled Coding Ethos runtime. The checked-out Coding Ethos authority is
the source and build authority; `.git/coding-ethos-hooks` is an installed,
byte-verified projection for durable sibling-worktree execution.

## Rationale

Git hook entrypoints live in the Git common directory and are shared by every
worktree. Pointing those entrypoints at one authority checkout creates a hidden
lifetime dependency: retiring or sandbox-hiding that checkout strands every
sibling hook even though the common Git directory remains healthy.

The common runtime is therefore a projection, not a second source of truth:

- `coding-ethos/bin` is built from the selected authority checkout.
- `parent-install` atomically copies every compiled Go command into the common
  runtime.
- `parent-check` requires regular executable files with byte-identical SHA-256
  content and rejects symlinks back to an authority checkout.
- `make build` installs the complete policy, pre-commit, toolchain, shim, and
  executable projection.

The invariant is:

```text
the selected checkout builds and verifies authority artifacts.
hooks execute the stable common projection of those artifacts.
```

If the projection is missing or stale, the error must name the exact supported
`parent-install` or `make build` command. Lifecycle hooks must not silently
select another worktree as authority.

## Target Runtime Layout

Authority build artifacts live under the selected `coding-ethos` checkout and
are ignored by Git. The installed hook runtime lives under the consumer
repository's Git common directory:

```text
coding-ethos/
  bin/
    coding-ethos-git-hook
    coding-ethos-policy
    coding-ethos-lint
    coding-ethos-agent-hooks
  build/
    policy/
      policy-bundle.json
      policy-metadata.json
    gemini/
      prompt-pack.json
    toolchain/
      manifest.tsv
      go-bin/{golangci-lint,shfmt}
      github-bin/{actionlint,dotenv-linter,hadolint,shellcheck}
      prefix/bin/

<git-common-dir>/coding-ethos-hooks/
  bin/{coding-ethos-*,cerun,git,lint}
  policy/{policy-bundle.json,policy-metadata.json}
  pre-commit/
  build/{policy,toolchain}/
  {coding_ethos.yml,repo_ethos.yml,config.yaml}
```

The common projection is runtime state and must never be committed. It is
updated only through the supported install/build workflow and validated against
the selected authority, rather than edited directly.

## Managed Toolchain

Hook execution must not depend on host linters or formatters being installed on
`PATH`. The same checkout-local runtime model applies to third-party tools:

- `build/toolchain/go-bin/` contains source-installed tools copied into a
  managed executable directory, starting with `shfmt` and `golangci-lint`.
- `build/toolchain/github-bin/` contains binaries installed from pinned GitHub
  release assets, including ShellCheck, actionlint, hadolint, and
  dotenv-linter.
- `build/toolchain/prefix/` is the controlled prefix for source-installed
  tools that need a Unix-style install root.

`make build` is responsible for ensuring required managed tools exist before
hook execution. `pre-commit/hooks/managed-toolchain.tsv` is the checked-in
source manifest for required tool versions, release assets, and SHA-256
digests. `make managed-toolchain-install` installs those tools and writes
`build/toolchain/manifest.tsv` with the installed paths.

`coding-ethos-run` prepends the managed tool directories to `PATH` before
dispatching to the Go hook runtime. The Go hook runtime also resolves binary
tool commands to checkout-local managed paths when possible, so `shfmt`,
`shellcheck`, `actionlint`, `hadolint`, `dotenv-linter`, and `golangci-lint` do not silently fall
back to host binaries. Missing required managed tools or a missing installed
manifest are runtime artifact failures and should self-repair through the same
bootstrap path as missing policy binaries.

The managed toolchain has two installer surfaces:

- a GitHub release binary installer for pinned release assets with mandatory
  SHA-256 verification and optional `GITHUB_TOKEN` authentication for GitHub API
  and asset requests
- a source install wrapper that sets checkout-local destinations before running
  a tool build, currently covering Go `go install` and Rust `cargo install`

Direct host installs such as `go install ...` into `$HOME/go/bin` are not a
runtime contract. They may unblock a local shell, but hooks must only rely on
artifacts under the `coding-ethos` checkout.

## Build Versus Test Boundary

`make build` is the explicit environment mutation target. It may regenerate
configs, install managed tools, refresh provider settings, install hook
entrypoints, compile policy bundles, compile Go runtime binaries, and sync
parent hook runtime artifacts.

Test and diagnostic targets must not do those things implicitly. They consume
the artifacts produced by `make build` and fail fast when required artifacts are
missing. This prevents ordinary verification commands from rewriting a parent
worktree, reinstalling hooks, changing generated config, or performing hidden
build setup.

Go tests use the normal Go workflow. `make go-test` runs `go test` through
managed capture, and `make go-e2e-test` runs the e2e package with `go test`.
That preserves normalized diagnostics, CEL promotion, trace retention, and
SARIF-compatible output without introducing a separate compile-and-run test
path.

## Hook Entrypoint Contract

The installed consumer repository hook entrypoint is a small executable script.
It passes the hook kind and hook name explicitly, for example
`<git-common-dir>/coding-ethos-hooks/bin/coding-ethos-run git-hook pre-commit
"$@"`, so installed Git hooks do not rely on `argv[0]` inference or a
worktree-local path.

The supported install/check workflow owns the bootstrap contract:

1. Resolve the consumer repository and its absolute Git common directory.
2. Build Go tools from the explicitly selected Coding Ethos authority.
3. Atomically install the compiled executables into the common runtime.
4. Install the remaining policy, toolchain, and hook artifacts through
   `make build` when the full runtime is being refreshed.
5. Verify executable type, mode, and byte identity with:

   ```bash
   coding-ethos/bin/coding-ethos-run parent-check --repo "$consumer_root"
   ```

6. Install Git entrypoints that dispatch only to the common runner.

The hook entrypoint contract must not:

- compare source mtimes to compiled policy during lifecycle hook execution
- compile policy directly
- select policy files
- inspect policy source configuration
- rewrite generated protected files by hand
- point to a worktree-local authority path
- accept a symlinked executable projection back to an authority checkout
- write response caches or other transient runtime state into `.git`

## Repair Rules

Bootstrap repair is explicit. `parent-install` refreshes generated parent
artifacts and compiled common-runtime executables; `make build` refreshes the
complete projection. It should not run because a timestamp looks old.

Examples that require repair:

- missing hook binary
- non-executable hook binary
- symlinked common-runtime executable
- executable whose SHA-256 differs from the authority build
- missing compiled policy bundle
- unreadable compiled policy bundle
- missing managed toolchain manifest
- missing required managed tool, such as `build/toolchain/go-bin/shfmt`,
  `build/toolchain/github-bin/dotenv-linter`, or
  `build/toolchain/github-bin/shellcheck`

Examples that should not block lifecycle hooks:

- source config has a newer mtime than the compiled bundle
- generated files were touched by checkout tools
- another worktree has different ignored build products

Strict freshness validation belongs in explicit maintainer/CI commands such as
`make validate`, `make cutover-verify`, and CI. Freshness is based on the
source hashes recorded in `build/policy/policy-metadata.json`, not mtimes.

## Safety Requirements

Bootstrap needs a few guardrails:

- Use a recursion guard such as `CODING_ETHOS_BOOTSTRAP=1` while running
  `make build`.
- Use an interprocess lock, preferably `flock`, so concurrent hooks do not run
  multiple builds over the same output directory.
- Print the exact failed command and preserve build output when repair fails.
- Keep authority build outputs under ignored `bin/` and `build/` directories.
- Keep response, trace, and other transient repo-local caches under ignored
  `.coding-ethos/` paths, not under the Git common runtime.
- Hook-launched `uv` commands bind both their download cache and project
  environment to the consumer-owned `.coding-ethos/cache/` tree and use the
  sealed project's committed lockfile in frozen mode. The installed shared
  runtime remains read-only and never receives a generated `.venv` or a lockfile
  rewrite.
- Install common-runtime executables with temporary-file sync plus atomic
  rename, and verify them by content rather than mtime.
- Keep installed hook entrypoints stable and move versioned behavior into the
  compiled common projection.

## Hook Execution Model

Hook execution follows a three-phase model designed for maximum parallelism
while preserving correctness ordering.

### Phase 1 — Format (Sequential Gate, Per-Language Parallel)

Formatters mutate files and must complete before linters run. Within the format
phase, per-language formatter chains run concurrently:

- **Python lane** (sequential): `pyupgrade` → `ruff format` → `ruff autofix`
- **Go lane** (sequential): `golangci-lint-format` (gofmt + golines)

The two lanes operate on disjoint file sets and run in parallel goroutines.
A cross-language text fixer (`fixText`) runs first and gates both lanes.

If any formatter fails or the format phase produces a non-zero exit, the hook
stops immediately.

### Phase 2 — Analysis Groups (Fully Parallel)

All non-AI analysis groups run concurrently as independent goroutines:

- `syntax`, `docker`, `workflow`, `shell`
- `python-policy`, `python-quality`, `python-static`
- `docs`, `security`, `go`

Within a group, commands run sequentially by default. Groups may declare a
`ParallelAfter` index to split their command list into:

- **Sequential prefix** — prerequisites that must pass before parallel work
- **Parallel suffix** — independent commands that run concurrently

For example, the `go` group uses `ParallelAfter: 2`:

| Index | Command            | Phase      |
|-------|--------------------|------------|
| 0     | `go-format`        | Sequential |
| 1     | `go-vet`           | Sequential |
| 2     | `go-test`          | Parallel   |
| 3     | `go-coverage`      | Parallel   |
| 4     | `golangci-lint`    | Parallel   |

If any sequential prefix command fails, the parallel suffix is skipped for
that group.

### Phase 3 — AI (Gated)

AI review groups (e.g., `gemini-check`) run only after all Phase 2 groups
succeed. This avoids wasting API credits on code that has already failed
deterministic quality gates.

### Incremental Linting

During pre-commit, `golangci-lint` receives `--new-from-rev=HEAD` so it only
reports issues in changed code. During pre-push, it runs on all files for
complete coverage.

## Migration Direction

Runtime artifacts are built from an explicitly selected `coding-ethos`
authority and executed from the stable common Git projection. New hook behavior
must preserve that one-way authority-to-projection relationship: no hook may
select an arbitrary sibling checkout, and no installed executable may link back
to a checkout that can be retired.
