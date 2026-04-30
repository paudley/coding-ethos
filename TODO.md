<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# TODO

## Hook Runtime Bootstrap

Goal: make the checked-out `coding-ethos` repository the single build and
runtime source of truth. Consumer repository hooks should only discover,
repair, and dispatch.

### Phase 1 - Runtime Layout

- [x] Replace the consumer `.git/coding-ethos-hooks` runtime cache with
  checkout-local `coding-ethos/bin/` and `coding-ethos/build/` artifacts.
- [x] Change `make build` so it writes all required hook binaries and compiled
  runtime files into the `coding-ethos` checkout.
- [x] Add `.gitignore` entries for checkout-local runtime outputs:
  `bin/`, `build/`, and any transient bootstrap lock/log files.
- [x] Decide the final artifact paths and document them in
  `docs/HOOK_RUNTIME_BOOTSTRAP.md`.
- [x] Ensure `make clean` removes checkout-local runtime artifacts without
  touching source configuration.

Acceptance criteria:

- [x] `make build` from the `coding-ethos` checkout produces every artifact
  needed for hook execution.
- [x] No normal hook path requires `.git/coding-ethos-hooks`.
- [x] Runtime artifacts are ignored and never staged by default.

### Phase 2 - Shim Rewrite

- [x] Rewrite installed consumer hook shims so they only discover the repo,
  locate `coding-ethos`, repair missing artifacts with `make -C ... build`,
  and dispatch to the checkout-local hook binary.
- [x] Prefer `$consumer_root/coding-ethos` for checkout discovery and avoid
  broad filesystem search.
- [x] If the checkout is missing, print a direct submodule repair command:
  `git submodule update --init coding-ethos`.
- [x] Move versioned bootstrap logic into the `coding-ethos` checkout so the
  installed parent shim stays small and stable.
- [x] Ensure hook dispatch passes the consumer repo root explicitly so submodule
  and worktree path resolution cannot drift.

Acceptance criteria:

- [x] Installed parent hook files contain no policy selection or policy
  validation logic.
- [x] Hook dispatch uses binaries from the checked-out `coding-ethos` tree.
- [x] Running hooks from the consumer root and from inside `coding-ethos`
  resolves the same consumer repository.

### Phase 3 - Repair Semantics

- [x] Remove lifecycle-hook fatal checks based on policy/source mtimes.
- [x] Bootstrap repair only when required artifacts are missing, unreadable, or
  non-executable.
- [x] Add a bootstrap recursion guard for repair builds.
- [x] Add an interprocess lock around hook-triggered repair builds.
- [x] Preserve build output when bootstrap repair fails and print the exact
  command that failed.
- [x] Make Stop/agent lifecycle hooks warn, not fail, for freshness concerns
  that are not runtime-corrupting.

Acceptance criteria:

- [x] Missing artifacts self-repair by running `make -C "$coding_ethos_root"
  build`.
- [x] Two concurrent hooks do not corrupt the runtime output directory.
- [x] Failed repair exits with a precise error and no stale-cache language.
- [x] Runtime mtime drift cannot block an agent Stop hook.

### Phase 4 - Freshness Validation

- [x] Keep strict policy freshness checks only in explicit verification paths
  such as `make validate`, `make cutover-verify`, and CI.
- [x] Add a hash manifest for policy bundle inputs before reintroducing any
  strict freshness gate.
- [x] Record policy input paths, hashes, and consumer repo config path in the
  manifest.
- [x] Make `make validate` compare hash manifests instead of mtimes.
- [x] Remove or downgrade all runtime messages that say the bundle is stale
  solely because a source file has a newer mtime.

Acceptance criteria:

- [x] Touching `coding_ethos.yml` without changing contents does not make
  runtime validation fail.
- [x] Changing policy input contents is detected by `make validate`.
- [x] CI and maintainer verification remain strict without blocking lifecycle
  hooks.

### Phase 5 - Tests

- [x] Add tests for missing binary, missing policy bundle, missing submodule,
  failed build repair, concurrent bootstrap, and normal dispatch.
- [x] Add a regression test for consumer-root resolution in a Git worktree with
  `coding-ethos` as a submodule.
- [x] Add a regression test proving `CODE_ETHOS_CONSUMER_ROOT` is not required
  for normal installed hooks.
- [x] Add a test proving the installed shim contains no policy-specific logic.
- [x] Add a test proving mtime drift does not fail Stop/agent hook execution.

Acceptance criteria:

- [x] Tests fail against the legacy `.git/coding-ethos-hooks` runtime-cache
  model and pass against checkout-local runtime artifacts.
- [x] Worktree/submodule path resolution is covered.
- [x] Hook-triggered repair behavior is covered without relying on real global
  machine state.

### Phase 6 - Documentation And Migration

- [x] Update `pre-commit/PRE-COMMIT.md`, `pre-commit/hooks/HOOKS.md`, and
  README install guidance once the implementation changes land.
- [x] Add a migration note explaining that `.git/coding-ethos-hooks` is legacy
  and can be deleted after checkout-local runtime is installed.
- [x] Update `make help` descriptions to distinguish build, install, validate,
  and cutover responsibilities.
- [x] Update hook failure messages so they point to `make -C <coding-ethos>
  build` or `git submodule update --init coding-ethos`, not generic stale
  runtime repair.

Acceptance criteria:

- [x] A new contributor can recover from missing runtime artifacts by following
  the hook error text alone.
- [x] Documentation no longer describes `.git/coding-ethos-hooks` as the target
  runtime model.
- [x] The migration path is explicit and does not require manual edits inside
  `.git`.

### Phase 7 - Managed Toolchain

- [x] Add a checkout-local Go bin directory for third-party Go tools.
- [x] Add a source-install wrapper that installs into managed checkout-local
  prefixes instead of host-global paths.
- [x] Add a GitHub-release binary installer as the starting point for pinned
  binary tool installs.
- [x] Install `shfmt` through the managed Go bin path and treat it as a
  required runtime artifact.
- [x] Prepend managed toolchain directories to hook runtime `PATH` before
  dispatching to Go hook code.
- [x] Migrate ShellCheck, actionlint, hadolint, and golangci-lint to pinned
  managed installers.
- [x] Record managed tool versions and checksums in a toolchain manifest.

Acceptance criteria:

- [x] Hook execution does not require host `shfmt` on `PATH`.
- [x] Missing managed `shfmt` self-repairs through `make -C <coding-ethos>
  build`.
- [x] All binary linters used by hook groups resolve from the managed
  toolchain before host `PATH`.

## Go Hook Architecture Follow-Ups

These items came from an adversarial SOLID review of the hook runtime. They are
not blockers for the package-relative path fix, but they should be handled
before the next broad hook expansion.

- [ ] Consolidate tool/runtime policy metadata currently split across
  `go/toolcatalog/catalog.go`, `go/internal/hooks/lint_tool_capture.go`,
  `pre-commit/hooks/go-hooks/hook_groups.go`, and
  `pre-commit/hooks/go-hooks/toolchain_groups.go`.
- [x] Move duplicated evidence-map policy out of the legacy hook path and the
  compiled policy path into one shared policy source.
- [x] Split `hooks.RunWithRegistry` so event parsing, policy evaluation,
  tool rewriting, output rendering, and trace logging have narrower ownership.
- [x] Keep lint and policy semantics out of generic hook-output formatting;
  output packages should render normalized results, not decide enforcement
  behavior.
- [ ] Slim `toolcatalog.Tool` into smaller capability interfaces so adding a
  captured tool does not require unrelated fields and switch expansion.
- [ ] Replace hook-group switch dispatch with registry-driven evaluators that
  can be extended from compiled config data.
- [x] Separate capture execution IO, parser selection, lint-log persistence,
  and user-facing rendering into testable components.
- [ ] Replace the remaining shell-owned lint capture entrypoint with Go so
  capture, target resolution, config enforcement, and output normalization all
  live in compiled hook code instead of shell glue.

## Go Lint Capture Replacement Prep

Do these before replacing the remaining shell-owned lint capture entrypoint.

- [x] Move lint target resolution into Go, including package-relative roots,
  invocation subdirectories, globs, missing paths, and repo-escape rejection.
- [x] Expose merged consumer config and policy-root data through one Go helper
  instead of rediscovering config paths in each command.
- [x] Define a Go capture request model containing tool name, original argv,
  invocation cwd, consumer root, ethos root, managed tool path, output format,
  and trace root.
- [x] Move managed tool executable and wrapper path resolution behind
  `toolcatalog` capability APIs.
- [x] Generate lint tool shims from `toolcatalog.CapturedLintTools()` rather
  than maintaining a shell-owned tool array.
- [x] Make generated-tool-config integrity checking callable as Go code before
  any captured linter executes.
- [x] Add behavior-preserving tests for rewritten commands, `ruff`, `mypy`, one
  managed binary linter, malicious absolute paths, globs, package-relative
  paths, and drifted generated configs.
- [x] Slim `toolcatalog.Tool` or add focused capability views such as
  `CaptureSpec`, `RuntimeSpec`, and `FileMatchSpec`.
- [x] Document the intended Go lint capture flow: shim -> Go dispatcher ->
  capture request -> managed tool -> normalized lint result.
