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

- [x] Consolidate tool/runtime policy metadata currently split across
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
- [x] Slim `toolcatalog.Tool` into smaller capability interfaces so adding a
  captured tool does not require unrelated fields and switch expansion.
- [x] Replace hook-group switch dispatch with registry-driven evaluators that
  can be extended from compiled config data.
- [x] Separate capture execution IO, parser selection, lint-log persistence,
  and user-facing rendering into testable components.
- [x] Replace the remaining shell-owned lint capture entrypoint with Go so
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

## Major Strategic Work

These are larger roadmap items for moving `coding-ethos` from a local hook and
generated-context system into a broader policy platform for AI-assisted
engineering. The common goal is defense in depth: prevent bad actions early,
explain violations in agent-native formats, and keep organization-specific
policy editable without weakening the compiled enforcement core.

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
    `off`/`auto`/`required` modes, backend-unavailable denials, and unit tests.
  - [x] Wire managed lint capture to opt-in sandbox execution through
    `--sandbox-mode`, preserving current default behavior while the profile is
    hardened.
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
- [x] Add a fallback strategy for platforms without Linux namespace/seccomp
  support: fail closed for required sandbox profiles in CI, and emit a clear
  degraded-enforcement warning only for explicitly advisory local modes.
- [ ] Evaluate future high-isolation backends such as gVisor, eBPF-based
  telemetry/enforcement, and Wasm/WASI execution for untrusted extension code,
  but keep the first implementation focused on rootless local hook execution.

Acceptance criteria:

- [x] A managed linter can run in a rootless sandbox with no network, read-only
  `.git`, hidden credential directories, bounded resources, and declared
  writable paths only.
- [x] Sandbox capability requests are visible in policy explanations, traces,
  SARIF, and MCP responses.
- [ ] A tool cannot gain broader filesystem, network, process, or syscall
  access by bypassing shell wrappers, using static binaries, or spawning child
  processes.
- [ ] Sandbox denials are normalized into policy-linked diagnostics with ETHOS
  principle IDs and remediation guidance.
- [ ] CI can require sandbox enforcement for high-risk tool classes while local
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

- [ ] Define a standardized machine-readable violation payload for agents.
- [ ] Emit XML, JSON, or TOON remediation blocks that include policy ID,
  ETHOS principle ID, skill ID, file/line, failed action, and concrete next
  steps.
- [ ] Feed hook failures back into Claude, Codex, Gemini, and future MCP clients
  in the strongest native format each provider supports.
- [ ] Track whether remediation hints reduce repeated failures in
  `.coding-ethos` traces.

Acceptance criteria:

- [ ] Agents can self-correct common hook failures without reading raw terminal
  noise.
- [ ] Remediation output is compact enough for context windows and precise
  enough to prevent guessing.
- [ ] Human output and agent output share the same normalized data model.
