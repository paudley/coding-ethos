<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Hook Runtime Bootstrap Model

## Decision

The consumer repository hook shim must not own policy behavior.

Its job is limited to:

- discover the consumer repository root
- locate the checked-out `coding-ethos` bundle for that repository
- verify that required built artifacts exist inside that checkout
- repair missing artifacts by running supported `make` targets in the
  `coding-ethos` checkout
- dispatch to the built hook binary

Policy evaluation, policy freshness, generated prompt packs, runtime command
selection, and hook behavior belong to the `coding-ethos` checkout. The shim is
only a bootstrap and dispatch layer.

## Rationale

The `coding-ethos` checkout is already required for normal operation. Keeping a
second runtime cache under the consumer repository `.git` directory creates two
possible sources of truth:

- the checked-out `coding-ethos` source tree
- the installed `.git/coding-ethos-hooks` runtime cache

That split is fragile. Worktrees, submodules, generated policy files, touched
configuration, and branch switches can cause the hook shim, `make build`, and
runtime validation to resolve different roots. When that happens, lifecycle
hooks can fail even though the correct repair command has been run elsewhere.

The simpler invariant is:

```text
make in the coding-ethos checkout builds the hook runtime.
hooks run the hook runtime from the coding-ethos checkout.
```

If the `coding-ethos` checkout is present and can build, the hook path should
self-heal missing runtime artifacts. If the checkout is missing or cannot build,
the error should name the exact checkout path and command required to fix it.

## Target Runtime Layout

Runtime artifacts live under the `coding-ethos` checkout and are ignored by
Git:

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
```

Consumer repository hooks should not install or validate policy bundles under
the consumer `.git` directory.

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

## Hook Entrypoint Contract

The installed consumer repository hook entrypoint should be a symlink to the
compiled `bin/coding-ethos-run` binary. The runner infers the Git hook name
from `argv[0]`.

The compiled runner owns the bootstrap contract:

1. Resolve the consumer repository root from Git.
2. Locate `coding-ethos`, preferably at `$consumer_root/coding-ethos`.
3. Fail with a clear submodule checkout instruction if it is missing.
4. Check for required artifacts in the `coding-ethos` checkout.
5. If artifacts are missing, run:

   ```bash
   make -C "$coding_ethos_root" build
   ```

6. Re-check the required artifacts.
7. Exec the built hook binary from the `coding-ethos` checkout.

The hook entrypoint contract must not:

- compare source mtimes to compiled policy during lifecycle hook execution
- compile policy directly
- select policy files
- inspect policy source configuration
- rewrite generated protected files by hand
- maintain a second runtime cache in the consumer `.git` directory
- write response caches or other transient runtime state into `.git`

## Repair Rules

Bootstrap repair should run only when required artifacts are missing or invalid
enough that they cannot be executed. It should not run because a timestamp looks
old.

Examples that should trigger repair:

- missing hook binary
- non-executable hook binary
- missing compiled policy bundle
- unreadable compiled policy bundle
- missing managed toolchain manifest
- missing required managed tool, such as `build/toolchain/go-bin/shfmt`,
  `build/toolchain/github-bin/dotenv-linter`, or
  `build/toolchain/github-bin/shellcheck`

Examples that should not block lifecycle hooks:

- source config has a newer mtime than the compiled bundle
- generated files were touched by checkout tools
- another worktree has a different runtime cache

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
- Keep build outputs under ignored `bin/` and `build/` directories.
- Keep transient repo-local runtime caches under ignored `.code-ethos/cache/`
  paths, not under `.git`.
- Keep installed hook entrypoints stable symlinks and move versioned behavior
  into the `coding-ethos` checkout.

## Migration Direction

The current `.git/coding-ethos-hooks` runtime cache should be treated as a
legacy implementation detail. New work should move toward a single source of
truth: runtime artifacts built and executed from the checked-out
`coding-ethos` repository.
