<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# TODO

## High Priority Runtime Self-Delegation

- [x] Remove the internal `coding-ethos-run agent-hook -> coding-ethos-hook`
  runtime self-delegation path. `coding-ethos-run agent-hook` invokes the hook
  CLI path in-process with the same compiled bundle and provider output
  contract, without spawning the separate `coding-ethos-hook` binary. This ban
  applies to Go runtime paths delegating back out to another generated Go
  executable; user-facing entrypoint shims such as `git` and managed-tool
  wrappers remain intentional integration surfaces.

## Project Discoverability

Goal: make `coding-ethos` easy to find, understand, trust, and try from GitHub,
search engines, and AI-agent policy/security searches.

- [x] Configure GitHub repository topics for the core search surface:
  `ai-agents`, `mcp-server`, `git-hooks`, `static-analysis`, `devsecops`,
  `policy-as-code`, and `cel`.
- [x] Update the GitHub repository description so it names the core product
  surface: AI-agent policy-as-code, MCP, CEL, Git hooks, SARIF, and static
  analysis.
- [x] Improve the README first screen with badges, a direct value statement,
  AI-agent search terms, and a 30-second start path.
- [x] Expand the source docs index so MCP, CEL, SARIF, runtime sandboxing,
  red-team, and roadmap documents are discoverable from one page.
- [x] Add a GitHub social-preview image sized for repository cards and shared
  links.
- [x] Add an OpenSSF Scorecard workflow and badge; the public score appears
  after the first successful `main` run publishes results.
- [ ] Track OpenSSF Scorecard gaps after the first release: fuzzing depth,
  Best Practices registration, tag/release detection, branch-protection
  ruleset visibility, sustained SAST history, and token-permission drift.
- [x] Register the OpenSSF Best Practices Badge project and add the badge once
  the public checklist has an issued project URL.
- [x] Document progress toward the OpenSSF Best Practices badge.
- [ ] Use OpenSSF Best Practices Gold as an active project checklist:
  - [x] Add checked-in `.bestpractices.json` repo-hosted evidence.
  - [ ] Drive all generated `Unmet` and `?` criteria to either repo-side
        remediation or an explicit external blocker.
  - [ ] Raise coverage evidence from the current 80% gate toward Gold-level
        statement and branch coverage expectations.
  - [ ] Add assertion-enabled dynamic analysis for critical runtime invariants.
  - [ ] Require assertion mode in release test/fuzz jobs.
  - [ ] Resolve review-capacity criteria such as two-person review and
        unassociated significant contributors when project governance supports
        them honestly.
- [x] Add a small demo walkthrough showing an agent using MCP instead of
  invoking lint directly.
- [x] Add a short docs landing page optimized for "policy as code for AI
  agents", "MCP server for static analysis", and "CEL Git hook policy" search
  terms.
- [x] Add comparison and integration docs for adoption-oriented searches.
- [x] Draft external launch-post copy outside the repo.
- [x] Add a threat model for security-focused project evaluation.
- [x] Add release process docs for public release readiness.
- [x] Add GitHub Discussions setup guidance.
- [x] Improve package metadata keywords, classifiers, and project URLs.
- [x] Add CEL, SARIF CI, command-block, and runtime-sandbox examples.
- [x] Add GitHub Pages Jekyll theme config for the `/docs` site.
- [x] Add a verified text demo for MCP command checks, managed lint checks,
  and SARIF output.
- [x] Add an asciinema recording and rendered GIF demo for MCP command checks,
  managed lint checks, and SARIF output.
- [x] Add docs asset regeneration instructions.
- [x] Cross-link examples to relevant MCP, CEL, SARIF, sandbox, and threat docs.
- [x] Update changelog for discoverability, Pages, examples, and demo assets.
- [x] Document PyPI metadata checks, Trusted Publishing, artifact
  attestations, SBOM generation, checksums, and verification as release
  prerequisites.
- [x] Add a local release dry-run target for package metadata, checksums, and
  GitHub workflow lint.
- [x] Add a package smoke target proving the wheel works outside the source
  checkout with packaged defaults.
- [x] Decide the correct Go hook runtime publication model for PyPI users:
  wheel data, companion platform wheels, GitHub release assets, or source
  checkout bootstrap. Do not bundle binaries into the wheel without a designed
  upgrade, checksum, and platform strategy.
- [x] Flesh out fuzzing beyond CI smoke coverage: add durable corpus seeds,
  parser and formatter invariants, crash artifact retention, and a long-running
  scheduled target for shell parsing, SARIF generation, CEL input construction,
  and hook event decoding.

Acceptance criteria:

- [x] A first-time visitor can identify the project purpose, supported agents,
  core enforcement surfaces, and first command within the first README screen.
- [x] GitHub shared links have a clear project visual.
- [x] Security-focused visitors can find trust signals and roadmap status
  without reading implementation docs first.

## Functional End-to-End Test Suite

Goal: prove the actual coding-ethos workflows users and agents depend on by
creating real temporary git checkouts, installing the real hook/runtime path,
running real commands, and inspecting real output and repository state.

- [x] Priority: enforce the `No Self-Promotion` branding ban in hooks and CI
  instead of relying on agent memory or plugin templates.
  - [x] Add CEL-backed policy expressions to the `no-self-promotion` principle
        in `coding_ethos.yml` so the source ethos explicitly rejects agent/tool
        branding in commits, pull request titles/bodies, documentation, and
        generated artifacts.
  - [x] Gate agent `PreToolUse` command payloads that create or edit pull
        requests, including `gh pr create`, `gh pr edit`, and connector-backed
        PR creation, when title/body arguments contain prefixes or attribution
        such as `[codex]`, `codex/`, `Generated with`, `Co-authored-by:
        Codex`, or equivalent tool branding.
  - [x] Gate `commit-msg` and staged-file scopes for self-promotion text in
        commit subjects, generated markdown, release notes, PR templates, and
        agent-authored docs while allowing legitimate references to product
        paths such as `.codex/` configuration files.
  - [x] Add real hook workflow regressions proving the plugin-suggested
        `[codex]` PR title is blocked before GitHub mutation and that a
        repo-native unbranded title is allowed.
  - [x] Regenerate `AGENTS.md`, `ETHOS.md`, provider skills, and prompt packs
        from the source ethos after adding the CEL policy.
- [x] Build a Go-based end-to-end test harness that creates isolated git
  checkouts with known sample files, initializes commits, installs
  coding-ethos hook/runtime artifacts, and runs ordinary git commands through
  the same path a user or agent uses.
- [x] Add a successful commit regression: stage a compliant file, run real
  `git commit`, assert the command succeeds, HEAD advances, hook traces are
  written, and no internal bookkeeping policy is surfaced as a user failure.
- [x] Add a failed commit regression: stage a known policy violation, run real
  `git commit`, assert the command fails with the original lint/policy finding,
  and assert `git.commit_head_advanced` does not replace or mask that failure.
- [x] Add managed lint capture workflow tests that run real managed tools
  against reference-repo source files for clean output, warning output with exit
  code 0, parseable diagnostics, and unparseable failures; assert
  TOON/JSON/SARIF and trace outputs preserve the evidence.
  - [x] Add real Ruff scenarios for clean output with exit code 0, parseable
        Ruff diagnostics, retained lint traces, and SARIF evidence.
  - [x] Treat empty machine-readable success payloads such as `[]` as silent
        clean output instead of user-visible linter warnings.
  - [x] Add JSON-output assertions for the same real managed-tool scenarios.
  - [x] Add a real managed-tool failure scenario that proves raw output remains visible and policy/CEL/SARIF evidence is retained.
- [x] Route every pre-commit gate through the normalized
  diagnostics/SARIF/CEL path. Gates such as `go-test`, `go-vet`, formatting,
  manifest checks, generated-config checks, and tool bootstrap checks must
  produce structured diagnostics that can become SARIF results and CEL inputs;
  they must not fall back to generic exit-code-only failures except for runner
  failures where no tool output exists.
  - [x] Emit file-specific diagnostics for generated-config freshness drift so
        stale repo-root tool configs appear as SARIF/CEL-addressable findings
        instead of pathless external-command failures.
  - [x] Emit file-specific diagnostics for generated Gemini prompt pack drift.
  - [x] Emit provider/skill-path diagnostics for generated agent skill surface
        drift.
  - [x] Convert tool bootstrap and managed-toolchain manifest failures into
        structured diagnostics with manifest paths, tool names, and repair
        commands.
  - [x] Verify `go-vet` failures produce parser-backed diagnostics in
        TOON/JSON/SARIF and retain bounded raw output only as supporting
        evidence.
  - [x] Verify formatter/checker gates (`go-format`, `format`, `shfmt`) report
        changed files as structured diagnostics rather than generic group
        failures.
  - [x] Add an end-to-end scenario that runs the real generated-config drift
        path and asserts trace plus SARIF evidence contains each stale file.
  - [x] Restore or replace the temporary GitHub ruleset drift from PR #64:
        code-owner review, Copilot review, and Scorecard code-scanning
        requirements need durable policy decisions before the next release.
- [x] Add Go test coverage tracking to the full diagnostics pipeline. Coverage
  output should be captured as structured evidence, flow through normalized
  diagnostics when thresholds fail, be eligible for SARIF output, and expose
  CEL facts so policy can distinguish missing coverage data, below-threshold
  packages, and ordinary test failures.
  - [x] Parse `go test -json -cover` package coverage output into non-blocking
    record diagnostics.
  - [x] Parse `go tool cover -func` file/function rows and totals into
    coverage diagnostics with file, line, package, and percent metadata.
  - [x] Expose Go coverage diagnostics through the shared CEL `coverage`
    collection and verify CEL can promote below-floor coverage into a
    blocking SARIF finding.
- [x] Add policy-yaml coverage threshold bands and enforcement modes. Coverage
  policy should support high/medium/low thresholds in `coding_ethos.yml`, with
  configurable floors for project, module/package, and file/function coverage.
  The default policy should require at least 80% coverage where coverage is
  enforced, expose warning bands below preferred thresholds, and allow CEL to
  promote below-threshold coverage records into blocking findings.
- [x] Suppress routine pass/noise lines in pre-commit gate output while
  preserving actionable failure context. For example, passing Go package
  lines such as `ok ...` should not appear in user-facing hook reports, but
  failing package names, test names, file/line locations, panic text, and
  unparseable failure excerpts must remain visible.
- [x] Add a shell-parser-backed hook rewrite that detects naked `python` or
  `python3` invocations and rewrites them to `uv run python` when the target
  repo is a uv-managed project. The policy must use parsed command structure,
  preserve arguments and quoting, avoid string matching, and only rewrite when
  repo evidence shows uv is the active Python environment contract.
- [x] Add hook workflow tests for PreToolUse and PostToolUse payloads from
  Codex, Claude, and Gemini shapes using real command text, file edits,
  apply-patch payloads, and provider-specific output fields.
- [x] Add MCP workflow tests that exercise the real stdio framing and request
  handling path for policy explanation, command checks, code-intel queries, and
  managed lint advice.
- [x] Add sandbox workflow tests that verify generated tool capabilities,
  filesystem write allowances, blocked capability requests, and trace/SARIF
  evidence using the real sandbox planner and available backend behavior.
  Covered by #129.
- [x] Evaluate whether Go's standard `testing` package remains sufficient or
  whether this needs a small internal scenario harness for fixture setup,
  command execution, output assertions, and trace inspection.

Acceptance criteria:

- [x] The suite fails if a real `git commit` path is broken, even when unit
  tests for individual evaluators pass.
- [x] The suite distinguishes product failures from internal telemetry; internal
  bookkeeping checks must not become user-facing policy blocks.
- [x] The suite runs in CI on pull requests and has clear local invocation
  instructions.
- [x] The Go E2E harness applies per-command timeouts and Unix process-group
  cleanup so a timed-out scenario does not leave child processes running.
- [ ] End-to-end tests use real commands, real managed tools, real filesystem
  state, real Git repositories, real MCP framing, and real trace/SARIF output.
  AI calls are the default allowed exception because live LLM behavior is
  nondeterministic and externally controlled.
- [ ] No pre-commit gate may bypass normalized diagnostics, SARIF evidence, or
  CEL policy evaluation. A failing gate must be represented as structured,
  actionable findings first, with bounded raw output only as supporting
  evidence for genuinely unparseable failures.
- [x] The end-to-end fake/mock admission policy is documented: every mock, fake
  executable, fake service, or synthetic fixture requires explicit admin
  approval before it is added.
- [x] The end-to-end fake/mock documentation requirement is documented: every
  approved exception must explain why no real alternative was safe or
  practical, what exact behavior is being replaced, and what risk remains
  uncovered.
- [x] The end-to-end fake/mock defect ledger is documented: every approved
  exception must be listed in `KNOWN_DEFECTS.md` with an owner, replacement
  plan, and removal condition.

## Code Intelligence Maintenance

- [x] Track explicit `rm` and `git rm` intent in code-intel traces instead of
  relying only on refresh-time missing-file detection.
- [ ] Define and implement code-intel database cleanup policy: retention
  windows, compaction triggers, stale diff-pattern pruning, and whether raw
  symbol text survives after source deletion.

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

- [x] Consolidate tool/runtime policy metadata currently split across
  `go/toolcatalog/catalog.go`, `go/internal/hooks/lint_tool_capture.go`,
  `go/cmd/coding-ethos-hook-runner/hook_groups.go`, and
  `go/cmd/coding-ethos-hook-runner/toolchain_groups.go`.
- [x] Move duplicated evidence-map policy out of hook group execution and the
  compiled policy path into one shared policy source.
- [x] Split `hooks.RunWithRegistry` so event parsing, policy evaluation,
  tool rewriting, output rendering, and trace logging have narrower ownership.
- [x] Keep lint and policy semantics out of generic hook-output formatting;
  output packages should render normalized results, not decide enforcement
  behavior.
- [x] Slim `toolcatalog.Tool` into smaller capability interfaces so adding a
  captured tool does not require unrelated fields and switch expansion.
- [x] Replace hook-group switch dispatch with registry-driven evaluators that
  can be extended from compiled config data.
- [x] Separate capture execution IO, parser selection, lint-log persistence,
  and user-facing rendering into testable components.
- [x] Replace the remaining shell-owned lint capture entrypoint with Go so
  capture, target resolution, config enforcement, and output normalization all
  live in compiled hook code instead of shell glue.
- [x] Parallelize hook execution: inter-group concurrency for analysis groups,
  intra-group concurrency via `ParallelAfter` for the `go` group, per-language
  parallel formatter lanes, AI groups gated on prior success.
- [x] Add incremental linting: `golangci-lint --new-from-rev=HEAD` for
  pre-commit, full lint for pre-push.

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

## Managed Tool Parsing And Capture Unification

Goal: make every managed tool, formatter, test gate, and quality gate flow
through one catalog-backed diagnostics contract. The tool catalog, parser
registry, managed-capture allow list, formatter handling, and SARIF/CEL evidence
must not drift.

- [x] Merge the tool catalog, parser registry, and managed-capture tool list
  into one source of truth. There should be one catalog, period: each tool entry
  declares execution, capabilities, parser/producer type, output contract,
  formatter behavior, SARIF/CEL support, and any explicit exception rationale.
- [x] Add a catalog invariant test requiring every hook-owned tool to declare
  one of:
  - a registered central diagnostics parser;
  - a first-class formatter changed-file producer;
  - a first-class internal structured diagnostic producer;
  - an explicitly justified generic fallback exception.
- [x] Add real-output parser fixtures for every managed tool. Each fixture must
  prove parse status is `parsed`, `empty`, or `changed_files`; `parse_error`
  should only appear for genuine runner/tool failures and must remain visible.
  - [x] Split diagnostics parser implementations into focused files for core
    registry/shared parsing, Go tools, Python tools, test-output tools, and
    static/config tools instead of growing one large parser file.
- [x] Parse stdout and stderr according to a tool-specific stream policy instead
  of using the first non-empty stream. Tools that emit actionable diagnostics on
  both streams must preserve both streams as structured diagnostics or bounded
  evidence.
- [x] Replace formatter fallback handling with first-class changed-file
  diagnostics. Mutating formatters should report which files changed, what tool
  changed them, and how that maps to SARIF/CEL evidence instead of relying on
  ad hoc stdout parsing.
  - [x] Preserve formatter argument context in changed-file diagnostic metadata
    so traces can explain the rewrite scope.
- [x] Move `radon-complexity`, `radon-maintainability`, `vulture`,
  `gofmt-check`, `pytest-gate`, and `gemini-check` into the central
  diagnostics path or a formal diagnostic-producer interface. Bespoke hookrunner
  parsing must not remain a parallel reporting path.
  - [x] Add central `radon-complexity` and `radon-maintainability` parsers.
  - [x] Add a central `vulture` parser.
  - [x] Route `gofmt-check` hook findings through the central diagnostics
    parser.
  - [x] Add a central `pytest-gate` parser for file/line pytest failures.
  - [x] Route pytest gate failures through the central diagnostics parser when
    parseable file/line output is available.
  - [x] Add a central `gemini` parser for Gemini JSON violations.
- [x] Add a dedicated `go-vet` parser or diagnostic producer instead of relying
  on generic fallback parsing.
- [x] Verify `go-test` command construction and parser alignment end to end:
  the executed command must emit JSON, the parser must suppress routine passing
  package noise, and failures must become SARIF/CEL-addressable diagnostics.
- [x] Normalize internal quality gates that already construct findings directly
  so they are treated as first-class diagnostic producers with the same trace,
  SARIF, CEL, policy, skill, and remediation metadata as external tools.
  - [x] Route Radon complexity and maintainability commands through managed
    capture instead of bespoke hook-report output.
  - [x] Route Vulture through managed capture while preserving whitelist,
    confidence, and exclude arguments.
  - [x] Add a `hookReport` to `lint.Result` bridge so remaining internal
    reports can render SARIF from normalized diagnostics instead of bespoke
    text-only structures.
  - [x] Add shared trace IDs to `hookReport` output so internal gate denials
    expose a correlation ID in TOON, JSON, human, and SARIF formats.
  - [x] Add shared policy/code/skill defaults for `hookReport` diagnostics so
    older internal gates do not emit anonymous SARIF/CEL findings when callers
    omit per-finding metadata.
  - [x] Add a shared `emitHookReport` path that logs normalized internal
    reports as lint traces before rendering them.
  - [x] Migrate manifest validation, module documentation, comment suppression,
    and Python version consistency reports to the trace-logging emitter.
  - [x] Migrate Python policy, SQL/direct import, pytest-gate, plan-completion,
    gofmt-check, and external quality reports to the trace-logging emitter.
  - [x] Migrate runtime ignore checks to the trace-logging emitter with
    structured policy, skill, file, and remediation metadata.
  - [x] Add docstring coverage SARIF output backed by normalized hook findings
    for missing public symbol docstrings.
- [x] Assess `shfmt` for uplift from medium quality to high quality. Prefer
  exact changed-file or hunk diagnostics over file-only diff-header parsing.
  - [x] Parse unified-diff hunk locations so shfmt findings identify the
    changed region instead of defaulting every file to line 1.
- [x] Assess `yamllint` for uplift from medium quality to high quality. Confirm
  parser behavior for paths containing colons and add fixtures for multiline or
  unusual YAML diagnostics.
  - [x] Add a parser fixture for YAML file paths containing colons.
  - [x] Add parser coverage for multiple diagnostics in one parsable output.
- [x] Assess `tombi` for uplift from medium quality to high quality. Prefer
  structured output if available; otherwise harden ANSI stripping, multiline
  locations, and schema-error fixtures.
  - [x] Add parser coverage for ANSI-colored diagnostics, schema-style warning
    output, intervening help lines, and delayed `at file:line:column`
    locations.
- [x] Assess `dotenv-linter` for uplift from medium quality to high quality.
  Confirm stable plain output across versions and add fixtures for empty files,
  missing files, and multi-file output.
  - [x] Add parser coverage for routine/no-problem lines, multi-file output,
    diagnostics with and without line numbers, and missing-file diagnostics.
- [x] Assess `go-vet` for normalization and uplift to high quality. Capture
  package context, file/line diagnostics, and runner errors without raw-output
  leakage.
  - [x] Preserve Go package headers as diagnostic metadata while suppressing
    them as standalone user-facing noise.
- [x] Assess `gofmt-check`/`gofmt` for normalization and uplift to high quality.
  Treat filename-list output and formatter rewrites as structured changed-file
  diagnostics.
  - [x] Add a central filename-list parser for `gofmt` and `gofmt-check`.
  - [x] Use catalog-backed diagnostic contracts for both check and formatter
    modes so Go formatting results no longer need hookrunner-specific parsing.
- [x] Assess `python-complexity` for normalization and uplift to high quality.
  Move Radon JSON parsing into the central diagnostics layer and preserve symbol
  names, complexity values, thresholds, and AST identity where available.
  - [x] Add a central Radon complexity JSON parser with complexity metadata.
  - [x] Route the hook command through managed capture so complexity findings
    produce trace, SARIF, CEL, skill, and remediation metadata.
- [x] Assess `python-maintainability` for normalization and uplift to high
  quality. Remove tool-name drift, preserve maintainability index values, and
  decide whether advisory results are recorded as SARIF evidence.
  - [x] Add a central Radon maintainability JSON parser with MI metadata.
  - [x] Route the hook command through managed capture so advisory MI findings
    are recorded as normalized diagnostics instead of side-effect-only parsing.
- [x] Assess `python-vulture` for normalization and uplift to high quality.
  Preserve confidence, symbol kind, whitelist evidence, and file/line locations
  in structured diagnostics.
  - [x] Route the hook command through managed capture so unused-code findings
    produce trace, SARIF, CEL, skill, and remediation metadata.
- [x] Assess `gemini-check` for normalization and uplift to high quality.
  Preserve model/check/batch metadata while routing violations and API/parser
  failures into the same diagnostics/SARIF/CEL evidence model.
  - [x] Add a central Gemini JSON violation parser.
- [x] Assess `pyupgrade` for normalization and uplift to high quality. Treat
  syntax rewrites as changed-file diagnostics and preserve the configured Python
  target version as evidence.
  - [x] Add generic formatter changed-file diagnostics for successful rewrites.
  - [x] Preserve formatter invocation arguments in trace metadata, including the
    configured `--pyNN-plus` target flag used by pyupgrade.
- [x] Assess `ruff-format` for normalization and uplift to high quality. Use
  Ruff parser support or changed-file diagnostics rather than generic fallback.
- [x] Assess `golangci-lint-format` for normalization and uplift to high
  quality. Detect formatter rewrites and route any tool warnings through the
  central `golangci-lint` parser where possible.
  - [x] Add generic formatter changed-file diagnostics for successful rewrites.
  - [x] Preserve formatter invocation arguments in trace metadata.
- [x] Assess `golines` for normalization and uplift to high quality. Treat line
  wrapping rewrites as changed-file diagnostics and preserve configured width as
  evidence.
  - [x] Add generic formatter changed-file diagnostics for successful rewrites.
  - [x] Preserve formatter invocation arguments in trace metadata.
- [x] Assess `pytest-gate` for normalization and uplift to high quality. Prefer
  machine-readable pytest output where practical, preserve failing tests,
  file/line context, coverage facts, and traceback excerpts as bounded evidence.
  - [x] Add a central pytest text parser for file/line failures.
  - [x] Parse pytest `FAILED ... - ...` and `ERROR ... - ...` summary lines
    into structured diagnostics with test-name metadata where available.
  - [x] Parse pytest coverage `TOTAL` rows into diagnostics with coverage
    percentage metadata.
  - [x] Add dedicated CEL policy inputs for pytest coverage totals and
    per-package coverage rows.

Acceptance criteria:

- [x] Adding a managed tool requires changing one catalog entry and fixtures,
  not parallel parser, capture, hookrunner, and SARIF lists.
  - [x] Catalog tests now fail when a registered parser lacks a catalog
    declaration or when a hook-owned tool lacks a diagnostic contract.
  - [x] Finish removing direct hookrunner quality-gate emitters so no new tool
    needs a hookrunner-specific reporting branch.
- [x] Every non-empty tool output is either parsed, intentionally represented as
  changed-file evidence, or reported as an unparseable tool failure with bounded
  evidence.
  - [x] Managed capture tests cover parsed output, stdout/stderr preservation,
    formatter changed-file evidence, empty machine-readable success output, and
    unparseable tool failures.
- [x] Formatter, linter, test, AI-review, and internal policy outputs all reach
  SARIF and CEL through the same normalized diagnostics contract.
  - [x] Formatter changed-file evidence, linter parser diagnostics, Radon,
    Vulture, go-test, pytest-gate, and Gemini violations now use central
    diagnostics.
  - [x] Finish first-class diagnostic producer interfaces for generated-config,
    manifest, docstring coverage, and other internal policy-only reports.
  - [x] Add SARIF rendering support for existing `hookReport`-based internal
    reports while the remaining producers are migrated.
  - [x] Add SARIF diagnostics for docstring coverage threshold failures.
- [x] No hook-owned tool can silently bypass parser quality checks because it is
  absent from a secondary registry or allow list.
  - [x] `CapturedLintTools()` is derived from catalog metadata, and tests assert
    parser registry/catalog consistency.

## Major Strategic Work

These are larger roadmap items for moving `coding-ethos` from a local hook and
generated-context system into a broader policy platform for AI-assisted
engineering. The common goal is defense in depth: prevent bad actions early,
explain violations in agent-native formats, and keep organization-specific
policy editable without weakening the compiled enforcement core.

### Agent Proxy Foundation

Open issues #52 through #62 describe one coherent Agent Proxy program. The
feature work should not start as one-off wrappers around individual tools; it
needs a shared proxy substrate that routes agent/API/tool traffic through the
same AST/CEL/SARIF, code-intel, sandbox, and remediation architecture already
used by hooks.

- [x] Define the Agent Proxy threat model and trust boundary for outbound
  provider API inspection, inbound tool-call inspection, local tool output
  transforms, and file-edit mediation. Include explicit CA/TLS interception
  risks, provider-protocol maintenance risks, and operator opt-in requirements.
  Foundation for #52.
- [x] Define a provider-agnostic proxy event envelope for outbound prompts,
  inbound model responses, tool-call requests, tool-call outputs, file reads,
  directory listings, file edits, search requests, and remediation actions.
  The envelope must carry provider, model, session ID, tool name, target paths,
  payload hashes, token estimates, trace IDs, and policy evidence. Foundation
  for #52-#62.
- [ ] Add provider protocol adapters for OpenAI, Anthropic, and Gemini payload
  schemas behind a narrow interface. Adapters should extract messages,
  attachments, tool calls, tool results, and streaming chunks without exposing
  raw provider JSON to policy code. Foundation for #52, #56, and #57.
- [x] Add a proxy session ledger in the repo-local code-intel store for read
  events, directory-listing events, prompt/tool payload hashes, token counts,
  cache hits, truncation decisions, policy injections, and edit attempts.
  Foundation for #53, #55, #56, #57, #58, and #62.
- [ ] Add tokenizer abstraction and calibrated token-estimate tests. The first
  implementation may use a conservative local estimator, but the interface
  must support provider/model-specific tokenizers without making enforcement
  depend on network access. Foundation for #55, #57, #58, and #59.
- [ ] Build a content-transform pipeline for proxy outputs with ordered stages:
  DLP/policy inspection, exact diagnostic extraction, stack-trace preservation,
  token budgeting, semantic pagination, compression, and final trace/SARIF
  evidence. Foundation for #55, #57, and #58.
- [ ] Add code-intel query APIs for compact AST anatomy maps, repo maps,
  semantic chunk pagination, exported symbol summaries, approximate token
  sizes, and nearby related symbols. Reuse SQLite/FTS/sqlite-vec storage rather
  than reparsing in the proxy. Foundation for #54, #58, #59, and #61.
- [ ] Add semantic-search and grep-augmentation contracts that combine exact
  search, AST filters, FTS, vector search, path constraints, and result
  expansion. Results must cite file, symbol, line range, content hash, and
  index freshness. Foundation for #61.
- [ ] Add a SEARCH/REPLACE patch engine with exact-one-match validation,
  content-hash preconditions, AST-aware affected-symbol reporting, rollback on
  failure, and normalized diagnostics when a search block is missing,
  non-unique, or stale. Foundation for #62.
  - [x] First enforced slice: reusable exact-one-match patch validation plus
    PreToolUse blocking for existing-file `Write` rewrites and concrete
    `Edit`/`MultiEdit` search blocks that are empty, missing, or non-unique.
  - [ ] Extend patch outcomes with AST affected-symbol evidence and durable
    proxy trace/code-intel storage.
- [ ] Add a transactional edit/remediation workspace for lint shielding:
  apply proposed edit, run managed autofixers and syntax checks, classify
  autofix-only changes versus semantic changes, emit a diff, and require policy
  approval before returning a modified result to the agent. Foundation for #60.
- [ ] Add just-in-time policy injection selection that maps proxy events to
  exact ETHOS principles, skills, and MCP policy explanations. Injection must
  be deterministic and evidence-backed, not generic vector-RAG over policy
  prose. Foundation for #56.
- [x] Add DLP facts and CEL scopes for outbound provider payloads and local
  tool outputs: secret-like values, protected paths, large binary payloads,
  credential filenames, ignored directories, and policy-sensitive source
  snippets. Foundation for #52 and #57.
- [ ] Extend trace, SARIF, and code-intel schemas for proxy denials,
  transformations, cache hits, token truncation, policy injections, semantic
  search results, and patch outcomes. Foundation for #52-#62.
  - [x] Add the first code-intel proxy session/event ledger for provider calls, file reads/listings, payload hashes, token counts, cache hits, injections, truncations, edits, and transform records.
  - [x] Add proxy event correlation, DLP facts, policy evidence, payload kind,
    direction, cache key, and transform metadata to the code-intel ledger.
  - [x] Add proxy result properties and ingestion fields for SARIF.
  - [ ] Add first-class proxy trace files and trace ingestion for proxy
    decisions and transformations.
- [x] Add an Agent Proxy E2E harness with fake provider endpoints and real
  local tools/files. The harness must test API inspection, file-read caching,
  anatomy map injection, output compression, token hard stops, semantic
  pagination, semantic search, search/replace patching, and lint shielding.
  Fake provider behavior must be documented under the existing E2E mock/fake
  exception policy. Foundation for #52-#62.
- [x] Document the Agent Proxy operator model: opt-in installation, CA
  lifecycle, supported providers, sandbox routing, privacy boundaries,
  failure modes, and how proxy decisions relate to existing hooks and MCP.
  Foundation for #52-#62.

Acceptance criteria:

- [ ] Agent Proxy feature work has one event model, one session ledger, one
  policy-evaluation path, one trace/SARIF evidence path, and one code-intel
  retrieval path.
- [ ] Proxy decisions can be explained through the same MCP policy tools and
  ETHOS/skill mappings as hook decisions.
- [ ] No proxy feature silently edits, truncates, injects, or suppresses data
  without a traceable policy decision and replayable evidence.
- [ ] TLS/API interception remains an explicit, documented operator choice and
  never becomes an invisible default.

### Real-Time Context Through MCP

- [x] Branch plan: deliver MCP over stdio first, backed by the compiled policy
  bundle and generated skill data already used by hooks.
- [x] Branch plan: expose `coding-ethos-mcp` as a repo-local Go binary and
  route `bin/coding-ethos-run mcp` through it.
- [x] Implement a Model Context Protocol server for `coding-ethos`.
- [x] Expose policy and skill queries for command checks, proposed edit checks,
  managed lint capture, compiled lint checks, lint advice, policy explanations,
  remediation lookup, task-based skill recommendation, and capability metadata.
- [x] Keep static generated docs and skills as durable fallback context while
  allowing Claude, Codex, Gemini, Cursor, and compatible clients to request
  focused context on demand.
- [x] Add tests proving MCP responses come from the same compiled policy bundle
  and ETHOS skill data used by hooks.
- [x] Go-ify branch task: replace the `start_hook_log` shell wrapper in
  `bin/coding-ethos-run` with a Go-owned logging dispatcher so
  metadata, stdout/stderr capture, repo-ignore validation, and sanitized event
  traces share one compiled implementation.

Acceptance criteria:

- [x] Agents can query whether a proposed file path, command, or edit violates
  policy before attempting the action.
- [x] MCP responses are compact, auditable, and linked to ETHOS principles and
  skill IDs.
- [x] The server does not create a bypass path around hook enforcement.

### Standardized Policy Language

Decision: use CEL as the first policy-language backend. CEL is the better
initial fit because `coding-ethos` needs fast, embedded, deterministic,
typed expressions over already-normalized hook/lint inputs. Keep OPA/Rego as a
future optional backend for larger set/query policies only if CEL expressions
become too limited.

- [x] Add `docs/POLICY_LANGUAGE_STRATEGY.md` as the design record for the
  CEL-first decision and Rego deferral.
- [x] Add a `policy.expressions` section to `config.yaml` and
  `repo_config.yaml` overlays with explicit fields for `id`, `description`,
  `scope`, `severity`, `principle_ids`, `skill_id`, `when`, `message`, and
  `advice`.
- [x] Compile CEL expressions into the policy bundle during
  `coding-ethos-policy compile`; syntax, type, and unknown-variable failures
  must fail bundle compilation.
- [x] Define stable typed CEL input objects for the first supported command
  policy slice: `command`, `argv`, `files`, `cwd`, `scope`, and `metadata`.
- [x] Extend typed CEL input objects to diagnostic, finding, repo, and path
  scopes.
- [x] Keep all host access out of CEL. CEL policy may inspect only the input
  object and static bundle data; file IO, Git calls, network access, time, and
  environment access remain first-party Go responsibilities.
- [x] Add an expression evaluator to the existing compiled evaluator registry
  so CEL-backed policies emit normal `policy.Decision` and
  `diagnostics.Diagnostic` values.
- [x] Support deterministic reusable helpers only through reviewed Go-provided
  CEL functions, starting with path classification, glob matching, suffix/prefix
  helpers, and collection checks.
- [x] Require every expression-backed policy to map to ETHOS principles and,
  where possible, a generated skill ID so output remains explanatory rather than
  bare rule text.
- [x] Add a CLI explain mode that shows CEL source, compiled input schema,
  matched evidence fields, and the ETHOS/skill mapping for an expression policy.
- [x] Add golden tests for TOON, JSON, and human output for CEL-backed command,
  file, diagnostic, and lint-finding policies.
- [x] Add negative tests for unsafe functions, unknown variables, type errors,
  non-boolean `when` expressions, missing ETHOS mappings, and invalid override
  merges.
- [x] Add migration guidance for moving small hardcoded evaluators into CEL
  only when doing so reduces Go code without weakening diagnostics or safety.
- [x] Revisit OPA/Rego only after CEL ships and real policies demonstrate a
  need for package-level rules, partial evaluation, large static data sets, or
  complex set joins.

Acceptance criteria:

- [x] A consuming repo can define a non-trivial custom policy without changing
  Go source.
- [x] CEL expressions are compiled and type checked before hook runtime.
- [x] Expression policies emit the same normalized diagnostics, ETHOS links,
  skill hints, traces, and TOON/human output as compiled evaluators.
- [x] Unsafe, non-deterministic, networked, or host-dependent policy execution
  is impossible from expression policy.
- [x] Direct hook, agent-hook, lint-capture, and future MCP paths all evaluate
  the same compiled expression policies.
- [x] Rego is not introduced unless a written design record identifies a
  concrete CEL limitation and a bounded integration surface.

#### CEL Generic Policy Engine Completion

The current CEL work is a typed custom-policy extension point. These items
define what is required before CEL can be treated as a complete generic policy
engine rather than a companion to first-class Go evaluators.

- [x] Define and version a stable policy object model for CEL inputs covering
  command, argv, tool, event, provider, cwd, repo, path, paths, file, files,
  file changes, diagnostic, finding, diff, Git facts, config facts, and safe
  metadata.
- [x] Remove aspirational CEL fields: every exposed field must be populated
  reliably for its scope, or removed until the runtime can provide it.
- [x] Populate real typed inputs for hook command scope, file/path scope, lint
  finding scope, Git scope, config scope, and diff scope.
- [x] Add typed `git_command` CEL facts for normalized Git argv, subcommand,
  global options, subcommand args, flags, targets, and `git -C` detection.
- [x] Add typed `file_changes` CEL facts for staged file status, byte size,
  line count, generated/test/protected classification, and original line count
  when Git can provide it.
- [x] Migrate the first tiny Git evaluators to CEL-backed policies:
  `git.change_dir_flag`, `git.destructive_worktree`, and
  `git.stash_blocked`.
- [x] Move the large-file and line-limit policies out of config-owned Go
  evaluators and into principle-local CEL expressions in `coding_ethos.yml`.
- [x] Treat `coding_ethos.yml` as the policy backbone: new shared policy should
  live with the ETHOS principle it enforces; config remains an artifact and
  overlay surface for policy not yet expressed properly in ETHOS.
- [x] Replace hand-rolled shell command tokenization in agent hook paths with a
  proper shell AST parser (`mvdan.cc/sh/v3/syntax`), deny malformed shell text
  at the hook boundary, and feed normalized `shell_commands` facts into Go and
  CEL policy. Keep Git wrapper execution on argv-based Git option parsing
  because wrapper commands have already been parsed by the shell.
- [x] Migrate more brittle command-string CEL examples and hook predicates to
  `shell_commands` facts instead of raw `command.contains(...)` matching.
- [x] Use parser-backed shell facts to distinguish direct `git`, `command git`,
  `env git`, `bash -c 'git ...'`, pipelines, grouped commands, and background
  commands before deciding whether to rewrite or block agent hook input.
- [x] Use parser-backed shell facts to route lint tools consistently through
  capture for direct invocations, `uv run`, `python -m`, leading assignments,
  chained commands, redirects, and pipelines.
- [x] Add higher-level CEL shell command facts such as `is_git`,
  `is_lint_tool`, `is_shell_exec`, `uses_path_override`,
  `has_command_substitution`, `has_process_substitution`, and
  `has_dynamic_expansion` so CEL policies stay readable.
- [x] Block or constrain ambiguous shell constructs around protected tools:
  `eval`, shell functions/aliases masking protected commands, command
  substitution, process substitution, here-doc command execution, and
  `bash -c`/`sh -c` unless recursively parsed and approved.
- [x] Improve agent remediation messages for shell policies so they identify
  the exact command node, argument, redirect, assignment, or pipeline segment
  that triggered the decision.
- [x] Reuse the shell AST parser for `.sh` policy: reject parse errors,
  detect raw protected-tool invocations, unsafe `eval`, risky redirects, and
  shell-script bypass patterns structurally instead of regex-only checks.
- [x] Use shell AST positions in SARIF where possible so shell-script and
  hook-command findings can point to exact command spans rather than only the
  whole command or whole file.
- [x] Replace first-file `path` semantics with explicit multi-file collection
  semantics such as `paths.exists(...)`, `paths.all(...)`,
  `files.changed_matching(...)`, and `findings.exists(...)`.
- [x] Make dispatch policy-driven so expression config declares hook events,
  tools, lint tools, modes, defense layers, principle IDs, and skill IDs
  without hardcoded evaluator registration.
- [x] Compile and cache CEL programs during bundle compilation or bundle load
  rather than recompiling expressions at evaluation time.
- [x] Add controlled policy inheritance and override rules for expression
  policies, including forbidden shadowing of protected built-ins and explicit
  rules for severity weakening.
- [x] Ensure every CEL policy emits the same normalized result shape as Go
  evaluators: policy ID, severity, decision, message, suggestion, principle
  IDs, skill ID, evidence, diagnostic location, remediation hint, and
  explanation metadata.
- [x] Expand the reviewed helper library with pure typed helpers for glob
  matching, path classification, test/generated/protected detection, lint code
  matching, command-tool detection, inline-env detection, repo config
  presence, and protected-branch facts.
- [x] Add first-class explain output for CEL decisions showing the expression,
  available input schema, helper functions, matched evidence, ETHOS grounding,
  and skill/remediation path.
- [x] Keep the CEL boundary pure by design: Go prepares facts; CEL decides over
  facts. CEL must not read files, execute shell/Git, inspect environment,
  access the network, or depend on wall-clock time.
- [x] Add operator documentation for supported scopes, input schemas, helper
  functions, dispatch, severity, examples, anti-patterns, and migration rules.
- [x] Add a trust-building test matrix for unknown fields, type failures,
  unknown helpers, multi-file semantics, hook/lint dispatch, inheritance,
  shadowing, explain-output golden files, trace output, malicious config, and
  performance with many expression policies.

Acceptance criteria:

- [x] Repo policy authors can express most simple and medium-complexity rules
  in checked-in ethos/config YAML without changing Go source.
- [x] Policy authors get compile-time failures for unknown fields, invalid
  types, invalid helpers, unsafe host access, and invalid dispatch.
- [x] Direct hook, agent-hook, lint-capture, explain, trace, CI, and future MCP
  paths cannot distinguish CEL-backed and Go-backed policies except by
  implementation metadata.
- [x] Multi-file and multi-finding policies are explicit and deterministic; no
  policy depends on implicit "first file" ordering.
- [x] Protected core policies remain non-shadowable and non-weakenable unless a
  protected source explicitly permits it.
- [x] Go evaluators remain only for complex parsing, expensive analysis, Git
  state modeling, managed toolchain behavior, path normalization, and other
  reviewed security-sensitive operations.
- [x] Add richer diff hunk and line-range facts once the runtime has a reviewed
  Go diff parser shared by hook, lint, CI/SARIF, and MCP paths.
- [x] Add provider-native event fields beyond provider/event/tool/scope only
  after all supported agents can populate the field consistently.

### Native IDE And Cursor Integration

- [ ] Build a VS Code/Cursor extension that invokes `coding-ethos-policy` and
  `coding-ethos-lint` in the background.
- [ ] Surface policy and lint diagnostics at edit time instead of waiting for
  Git hooks.
- [ ] Provide quick access to relevant ETHOS principles and generated skills
  from diagnostics.
- [ ] Detect protected-path edits, hook tampering attempts, bare exception
  patterns, suppressions, and other high-value failures before a diff is
  applied.

Acceptance criteria:

- [ ] The extension uses the same compiled bundle and managed toolchain as CLI
  and hook execution.
- [ ] Diagnostics are low-noise, actionable, and grouped by file and policy.
- [ ] The extension is advisory by default but can enforce blocking behavior in
  managed workspaces.

### CI/CD Components And SARIF

- [x] Add SARIF output for normalized policy and lint diagnostics.
- [x] Provide native GitHub Actions and GitLab CI examples.
- [x] Package reusable GitHub Actions and GitLab CI components.
- [x] Ensure CI runs the same compiled policy bundle and managed toolchain
  versions as local hooks.
- [x] Publish violations as PR annotations and, where supported, security/code
  scanning findings.
- [x] Document expanded SARIF product uses in `docs/SARIF_USES.md`.

#### SARIF Evidence Ledger Expansion

- [x] Keep code-scanning SARIF actionable: record-only policy context remains
  in TOON/JSON traces and must not upload as root-level warning results.
- [x] Omit pathless policy results from code-scanning SARIF so GitHub does not
  reject uploads and coding-ethos does not invent noisy alerts at `.` line 0.
- [x] Add MCP remediation endpoints that accept a SARIF run or trace ID and
  return focused ETHOS-grounded repair advice.
  - [x] Add `sarif_remediation_advice` for SARIF JSON payloads, selected
    results, CEL provenance, skill context, and MCP `lint_check` rerun
    guidance.
  - [x] Add retained lint trace ID lookup through the configured consumer
    root's `.coding-ethos/lint-runs/` directory.
- [x] Add cross-tool finding grouping using SARIF fingerprints, policy IDs,
  skill IDs, and source locations.
- [x] Emit policy coverage summaries that show which ETHOS principles,
  policies, skills, and tool families ran for a commit or PR.
  - [x] Start the coverage ledger by emitting SARIF
    `runs[].properties.policy_coverage` for normalized decisions and
    diagnostics, including CEL provenance when available.
- [x] Add SARIF trend analysis for newly introduced, reopened, fixed, and
  worsening findings across commits.
  - [x] Add first `sarif_trend_analysis` MCP compare path for introduced,
    fixed, and persisting findings across SARIF payloads or retained lint
    traces.
  - [x] Add baseline-history semantics for reopened and worsening findings.
- [x] Produce compact PR risk summaries from SARIF and `.coding-ethos` traces
  for agents and reviewers.
  - [x] Add first SARIF-only `sarif_risk_summary` MCP tool for result counts,
    blocking/security counts, policy/skill/tool/file hotspots, finding groups,
    and next remediation calls.
  - [x] Fold retained lint trace evidence into the summary through the same
    safe trace ID lookup.
- [x] Retain SARIF plus trace artifacts as an audit evidence bundle in CI.
- [x] Define an IDE/editor diagnostic integration path that consumes the same
  SARIF output used by hooks and CI.
- [x] Report unmapped diagnostics, noisy rules, missing skill IDs, and weak
  severity mappings as policy-authoring feedback.

Acceptance criteria:

- [x] A repo can gate PRs in CI even if local hooks are bypassed.
- [x] SARIF includes policy IDs, ETHOS principle IDs, skill IDs, file/line
  locations, remediation advice, and stable rule metadata.
- [x] CI output remains compact for agents while preserving full artifacts for
  audit.
- [x] SARIF can act as a shared evidence ledger for hooks, CI, MCP, agent
  remediation, audit review, and editor diagnostics without creating a second
  policy interpretation path.

### Runtime Sandboxing And Capability Enforcement

Goal: complement CEL and Go policy evaluation with an OS-level data-plane
boundary for managed hook, lint, and agent-invoked tool execution. CEL remains
the control plane that decides which action and capabilities are allowed; the
sandbox enforces filesystem, network, process, syscall, timeout, and resource
limits at runtime.

- [x] Write a design record for runtime sandboxing that explicitly rejects
  `LD_PRELOAD` as a security boundary because it is bypassed by static
  binaries, direct syscalls, environment scrubbing, and modern Go/Rust
  toolchains.
- [x] Define a tool capability model in `toolcatalog` and compiled policy data:
  read paths, write paths, network access, process visibility, Git access,
  environment access, timeout, memory, CPU, and sandbox profile.
- [x] Add CEL-visible capability facts so policies can require explicit ETHOS
  approval for risky capabilities such as network, broad write access, raw Git,
  process inspection, or privileged filesystem paths.
- [x] Add the first CEL deny-by-default network capability policy: managed
  tools must declare `requires_network`, and agent-invoked network-capable
  tools require explicit approval before execution.
- [x] Prototype a Linux sandbox runner using Bubblewrap (`bwrap`) before
  writing raw namespace code, with a Go-owned request model and no shell glue.
  - [x] Add `go/internal/sandbox` with Bubblewrap command construction,
    platform-derived sandbox requirements, backend-unavailable denials, and
    unit tests.
  - [x] Wire managed lint capture to sandbox profile execution from the tool
    catalog without an operator sandbox mode switch.
- [x] Add mount namespace support for read-only root, read-only `.git`, hidden
  credential directories, and minimal read/write bind mounts for declared repo
  paths.
  - [x] Build Bubblewrap args with read-only `/`, tmpfs `/home` and `/root`,
    read-only repo binding, read-only `.git`, and declared writable paths only.
  - [x] Filter attempted `.git` write bind paths from both Bubblewrap args and
    sandbox evidence.
- [x] Add PID and network namespace profiles for managed tools so ordinary
  linters cannot inspect host processes or exfiltrate data.
- [x] Add seccomp-bpf profile support for managed hooks and lint tools,
  starting with a conservative profile that blocks privilege escalation,
  `ptrace`, mount/unshare, and other abnormal syscalls.
- [x] Add cgroup-backed resource quotas for sandboxed tool execution, including
  hard timeouts and memory limits to prevent local denial-of-service failures.
- [x] Record sandbox profiles, declared capabilities, and runtime denials in
  `.coding-ethos` traces and SARIF properties so failures are auditable and
  agent-remediation friendly.
  - [x] Record sandbox evidence in lint traces under
    `result.capture.sandbox`.
  - [x] Record sandbox evidence in SARIF run properties under
    `runs[].properties.sandbox`.
  - [x] Normalize required-mode backend failures as
    `runtime.sandbox_denial` findings.
- [x] Require Bubblewrap for sandboxed execution and fail closed with clear
  denial evidence when Linux namespace/seccomp support is unavailable.
- [x] Remove sandbox `auto` mode so sandbox-declared tools have only explicit
  `off` or fail-closed `required` execution paths.
- [ ] Evaluate future high-isolation backends such as gVisor, eBPF-based
  telemetry/enforcement, and Wasm/WASI execution for untrusted extension code,
  but keep the first implementation focused on rootless local hook execution.

Acceptance criteria:

- [x] A managed linter can run in a rootless sandbox with no network, read-only
  `.git`, hidden credential directories, bounded resources, and declared
  writable paths only.
- [x] Sandbox capability requests are visible in policy explanations, traces,
  SARIF, and MCP responses.
- [x] A tool cannot gain broader filesystem, network, process, or syscall
  access by bypassing shell wrappers, using static binaries, or spawning child
  processes.
- [x] Sandbox denials are normalized into policy-linked diagnostics with ETHOS
  principle IDs and remediation guidance.
- [x] CI can require sandbox enforcement for high-risk tool classes while local
  developer workflows remain recoverable and explicit about unsupported
  platforms.

### Adversarial Red-Team Test Suite

- [x] Build an automated red-team harness for attempts to bypass or tamper with
  `coding-ethos` protections.
- [x] Cover protected paths, raw Git bypasses, absolute binaries, nested shell
  execution, symlink replacement, path traversal, config drift, hook deletion,
  and managed toolchain evasion.
- [x] Cover protected paths, raw Git bypasses, absolute binaries, nested shell
  execution, symlink replacement, path traversal, generated config drift, hook
  deletion, and managed toolchain evasion in deterministic Go red-team
  scenarios.
- [x] Capture both successful blocks and any missed bypass attempts as
  reproducible fixtures.
- [x] Add regression tests for the initial deterministic bypass classes.
- [ ] Add live Claude, Codex, and Gemini prompt-based red-team runs where
  practical.

Acceptance criteria:

- [x] Red-team scenarios can run in isolated sample repositories without
  touching the parent repo.
- [x] Each bypass attempt produces either a clear block or a filed gap with a
  failing regression test.
- [ ] The suite validates Claude, Codex, Gemini, and generic shell workflows
  where practical.

### Centralized ETHOS Registry And Inheritance

- [ ] Support policy and ethos inheritance, such as extending a local preset,
  GitHub-hosted preset, or enterprise registry preset.
- [ ] Define merge rules for inherited principles, skills, evidence maps,
  generated tool config, and repo-specific overrides.
- [ ] Validate inherited sources with pins, hashes, and provenance metadata.
- [ ] Provide curated presets such as strict Python, strict Go, agent-safe Git,
  and security-first repositories.

Acceptance criteria:

- [ ] A repo can inherit a baseline ETHOS and override only the local context.
- [ ] Inheritance is deterministic, auditable, and visible in policy trace
  output.
- [ ] Unpinned remote policy inputs are rejected unless explicitly allowed.

### Agent Remediation Loop

- [x] Define a standardized machine-readable violation payload for agents.
- [x] Emit XML, JSON, or TOON remediation blocks that include policy ID,
  ETHOS principle ID, skill ID, file/line, failed action, and concrete next
  steps.
- [x] Feed hook failures back into Claude, Codex, Gemini, and future MCP clients
  in the strongest native format each provider supports.
- [x] Add stable remediation IDs, skill-loading instructions, MCP
  `remediation_explain`, provider-output golden fixtures, and exact
  remediation examples.
- [x] Add normalized source spans, stable finding IDs, evidence envelopes,
  SARIF result evidence properties, trace schema versions, and remediation
  lifecycle events as the substrate for code intelligence storage.
- [x] Add CEL-facing source/finding fields and backend-neutral evidence store,
  code fact store, vector index, and trace ingestor interfaces.
- [x] Add per-run remediation summaries to hook and lint traces so later
  storage can measure repeated policy failures without parsing provider text.
- [x] Add an initial SQLite code-intelligence store under
  `.coding-ethos/code-intel.db` that ingests retained hook/lint traces,
  normalized findings, remediation payloads, and remediation events.
- [x] Add repeated-failure and FTS search commands over imported trace data.
- [x] Add hook-usage intelligence storage for allow/block/rewrite events:
  tracking ID, session/provider/tool, operation kind, target kind, risk
  category, command shape fingerprint, target-set fingerprint, runtime,
  decision rows, message/suggestion variant hashes, and target paths.
- [x] Expose hook-usage summaries through `code-intel hook-usage` and
  `code_intel_hook_usage` so agents and maintainers can identify recurring
  friction, bypass attempts, rewrite patterns, and policy hotspots.
- [x] Add hook review storage and CLI surfaces so admin/operator review can
  mark correct blocks, false positives, unclear messages, over-broad policies,
  and missing allow-list cases.
- [x] Track whether remediation hints reduce repeated failures in
  `.coding-ethos` traces by linking follow-up attempts to outcomes.
- [x] Extend local-first remediation storage to SARIF trace references with
  full-text plus embedding search over remediation IDs, policy IDs, skills,
  command/file context, outcomes, and follow-up attempts.
- [x] Complete the `remediation_outcomes_1` storage foundation:
  normalized SQLite tables for SARIF runs/results, remediation outcomes,
  CEL/evaluator provenance, embedding metadata, and vector-backend row
  references.
- [x] Add code-intelligence CLI/query surfaces for SARIF result references,
  remediation outcomes, remediation effectiveness, and vector metadata status.
- [x] Add embedding candidate, sqlite-vec upsert, hybrid search, and index
  status surfaces so agents can retrieve prior fixes before broad file reads.
- [x] Implement sqlite-vec as the active SQLite vector backend fed from
  canonical SQLite records; do not let vector rows become the only copy of
  policy, SARIF, CEL, or remediation evidence.
- [x] Add Tree-sitter AST indexing for Go, Python, JavaScript/TypeScript,
  shell, and YAML into the canonical SQLite store with stable `code_chunk`
  records, FTS rows, and embedding-candidate export.
- [x] Refactor Tree-sitter extraction toward a resolver-style AST service
  inspired by `~/Active/pyqa_lint`: parser reuse, shared traversal helpers,
  and line-to-nearest-context lookup.
- [x] Add JSON and TOML config-entry AST chunks so policy, SARIF, MCP, and
  code-intel retrieval can target precise config entries.
- [x] Expose AST code intelligence through `code-intel index-code`,
  `code-intel code-chunks`, `code_intel_index_code`, and
  `code_intel_code_chunks` so agents can retrieve focused symbol context
  before broad file reads.
- [x] Expose line-based AST code context lookup through `code-intel
  code-context --path ... --line ...` and `code_intel_code_context`.
- [ ] Add Markdown AST chunking after choosing a maintained Go binding or a
  first-class adapter for the markdown parser layout.
- [x] Add initial AST graph edges for containment, imports, and same-file
  references.
- [ ] Extend AST graph edges to language-specific calls, inheritance,
  test-to-source links, and documentation links.
- [x] Store parser metadata, index timestamp, and content hashes beside
  AST-derived code files/chunks.
- [x] Add stale code-chunk embedding invalidation when reindexing changes chunk
  hashes.
- [x] Add incremental reindex invalidation that compares file hashes even when
  mtime and size appear unchanged.
- [ ] Use Tree-sitter facts to augment CEL source inputs with symbol kind,
  symbol name, symbol path, enclosing function/class/type/config entry, byte
  span, line span, content hash, parent symbol, and nearest test/doc chunk.
- [ ] Add CEL helper functions over AST facts: `symbol.kind_is(...)`,
  `symbol.name_matches(...)`, `source.enclosing_symbol(...)`,
  `source.changed_symbol_count()`, `source.has_nearby_test()`,
  `source.has_doc_chunk()`, and `source.symbol_too_large(...)`.
- [x] Move more size/complexity policy into principle-owned CEL by evaluating
  Tree-sitter chunks instead of whole files: block growing oversized functions,
  classes/types, shell functions, and YAML config entries while allowing
  shrinking edits.
- [x] Add AST-aware edit preflight for agents: classify whether an Edit/Write
  grows, shrinks, adds, deletes, or rewrites an existing symbol before CEL
  decides whether the action is allowed.
- [ ] Extend AST-aware edit preflight to classify symbol renames explicitly
  instead of representing them as delete/add pairs.
- [x] Add AST-aware diff facts that map changed lines to affected symbols so
  policy can target the edited function/config entry rather than the entire
  file.
- [ ] Add SARIF locations for Tree-sitter-backed findings using exact symbol
  start/end lines, byte offsets, and region snippets.
- [x] Include AST node kind and symbol identity in SARIF properties for
  Tree-sitter-backed CEL findings.
- [x] Document the required AST/CEL/SARIF architecture in
  `docs/AST_CEL_SARIF_ARCHITECTURE.md`: Go collects facts, CEL evaluates
  configurable decisions, SARIF reports stable remediation-ready findings.
- [x] Add a shared Python AST fact surface for policy evaluators and CEL:
  imports, calls, functions, classes, assignments, lambdas, exception handlers,
  symbol context, ancestry flags, and initial signature facts.
- [x] Expose Python AST facts to CEL as `python_ast` so future pyqa_lint ports
  can move decision logic into principle-owned expressions before adding new Go
  evaluators.
- [x] Extend SARIF AST identity to diagnostics with AST node metadata even when
  the finding has no named symbol path, and include parent-symbol metadata for
  source-backed findings.
- [x] Use Tree-sitter-backed Python policy checks to block conditional import
  workarounds at write time: nested imports, `TYPE_CHECKING` import branches,
  module `__getattr__`, `__import__`, and `importlib.import_module`.
- [x] Add pyqa-inspired Python functional idiom diagnostics for assigned
  lambdas and closure factories, grounded in the Functional Idioms principle.
- [ ] Port pyqa_lint's strict typing AST guidance into principle-owned Python
  policy: flag `Any`, `typing.Any`, `object`, and `builtins.object` in
  parameter, return, `*args`, `**kwargs`, and annotated-assignment positions;
  ground it in No Optional Types for Required Dependencies and Static Analysis
  as the First Line of Defense.
- [ ] Port pyqa_lint's signature-width AST guidance into a SOLID/SRP policy:
  count positional-only, positional, keyword-only, varargs, and kwargs on
  function definitions; recommend parameter objects or smaller seams when
  signatures exceed the configured threshold or rely on `**kwargs`.
- [ ] Port pyqa_lint's Tree-sitter docstring structure checks into
  Documentation as Contract: index module/class/function docstrings, require
  summaries, enforce Args/Returns/Yields sections from actual AST parameters
  and return/yield behavior, and emit symbol-level SARIF regions.
- [ ] Port pyqa_lint's generic value-type Tree-sitter analysis as configurable
  class-trait policy: derive dataclass/frozen/slots/enum/iterable/sequence/
  mapping/value traits from decorators, bases, methods, and `__slots__`, then
  require or recommend dunder methods such as `__eq__`, `__hash__`, `__repr__`,
  `__str__`, `__len__`, `__bool__`, `__iter__`, and `__contains__`.
- [ ] Generalize pyqa_lint's interface-first AST rules into configurable
  architecture policy: detect concrete imports across configured layer/domain
  boundaries, forbid concrete functions/classes/assignments in configured
  interface modules except Protocol/ABC/TypedDict/Enum/dataclass-like
  contracts, and link violations to Protocol-First Design.
- [ ] Generalize pyqa_lint's DI composition-root AST rule: detect configured
  service-container registration calls such as `container.register(...)` and
  allow them only in configured composition roots or bootstrap modules.
- [ ] Generalize pyqa_lint's cache-wrapper AST rule: resolve imported aliases
  and decorator calls for banned cache decorators such as `functools.lru_cache`,
  then require the repo's configured cache abstraction when a principle says
  caching must be centralized or observable.
- [ ] Replace remaining text-only Python hygiene checks with AST-backed policy
  facts inspired by pyqa_lint: detect debug breakpoints/tracing, bare except,
  broad `except Exception` without justification, debug imports, direct
  `SystemExit`/`sys.exit` outside CLI modules, module `__main__` blocks, and
  unsanctioned `print` calls while avoiding strings/comments false positives.
- [ ] Port pyqa_lint's package module documentation convention as configurable
  Documentation as Contract policy: discover package directories from
  `__init__.py`, require configured module docs such as `MODULE.md` or
  package-derived docs, and verify required sections.
- [x] Reuse pyqa_lint's AST visitor/reporting pattern conceptually by adding a
  shared Go policy visitor helper for Python AST evaluators: one parse path,
  one suppression check path, consistent diagnostic metadata, and no duplicate
  ad-hoc Tree-sitter traversals per policy.
- [x] Add SARIF partial fingerprints based on path, language, symbol path,
  node kind, rule/policy ID, and content hash so findings remain stable across
  unrelated line movement.
- [x] Link SARIF/CEL findings that carry AST identity back to matching
  `code_chunk` rows through `ast_finding_links`.
- [ ] Emit SARIF code flows/thread flows for policy findings that involve
  relationships, such as unsafe call chains, missing tests for changed symbols,
  imports from forbidden layers, or generated-config edits from source files.
- [ ] Add AST-backed SARIF suppression guidance that points agents to the
  principle and symbol-level remediation path instead of allowing broad
  `noqa`, `nolint`, or config weakening.
- [ ] Use Tree-sitter graph edges to enforce architecture policies in CEL:
  layer boundaries, forbidden imports, test-to-source coverage proximity,
  generated-artifact/source ownership, and shell wrapper boundaries.
- [ ] Add policy articulation output that explains Tree-sitter-backed
  decisions in human terms: "this edit grows function X from N to M lines",
  "this YAML key is enforcement config", or "this import crosses a forbidden
  layer."
- [ ] Extend `policy_explain` and `remediation_explain` to include relevant
  AST context, including the exact symbol, enclosing parent, related test/doc
  chunks, and the next MCP calls (`code_intel_code_chunks`,
  `code_intel_search`, or `lint_check`).
- [ ] Add AST-aware guidance packets for agents that include focused code
  chunks, related prior SARIF/remediation history, and symbol-specific rerun
  instructions before suggesting broad file reads.
- [x] Add CLI context expansion for code chunks with parent, children,
  graph edges, and linked SARIF/CEL findings.
- [x] Add MCP context expansion for code chunks with parent, children,
  graph edges, and linked SARIF/CEL findings.
- [x] Extend context expansion with sibling chunks.
- [ ] Extend context expansion with language-specific callers/callees,
  related tests, related docs, recent policy failures, and prior fixes for the
  same symbol.
- [ ] Promote diagnostic signature tokens into a first-class stored column or
  relation once repeated-failure clustering needs querying beyond FTS search.
- [ ] Add query-driven AST capture specs for imports, references, config keys,
  headings, and documentation chunks; keep enforcement policy in
  `coding_ethos.yml` and use capture specs only to supply facts.
- [x] Add first staleness and trust metadata to indexed AST files: indexed
  content hash, index timestamp, and parser metadata.
- [x] Add stale-result refusal behavior and current-content validation for
  `CodeContext` lookups.
- [x] Add stale-result refusal behavior and current-content validation for
  compact code context, repo maps, and other AST-derived context lookups.
- [ ] Add stale-result refusal behavior and current-content validation for
  every AST-derived CEL/SARIF result.
- [ ] Add regression tests proving Tree-sitter facts are identical across hook,
  lint, CLI, MCP, and CI/SARIF paths so AST-backed policy cannot drift between
  enforcement surfaces.
- [x] Build the code intelligence roadmap in `docs/CODE_INTEL.md`: Tree-sitter
  AST chunking, SQLite canonical storage, sqlite-vec vector search, hybrid
  retrieval, and MCP code/remediation search tools.

Acceptance criteria:

- [x] Agents can self-correct common hook failures without reading raw terminal
  noise.
- [x] Remediation output is compact enough for context windows and precise
  enough to prevent guessing.
- [x] Human output and agent output share the same normalized data model.
- [x] The local code-intelligence database can answer which SARIF/CEL findings
  repeated, which remediation guidance was issued, and whether later attempts
  fixed or repeated the finding.
- [x] The local code-intelligence database can answer which hook
  provider/tool/operation/target/risk groups are blocked, rewritten, or
  repeatedly advised.
