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

- [ ] Implement a Model Context Protocol server for `coding-ethos`.
- [ ] Expose policy, skill, and repo-context queries such as protected-path
  checks, language-specific guidance, policy explanations, and remediation
  lookup.
- [ ] Keep static generated docs and skills as durable fallback context while
  allowing Claude, Codex, Gemini, Cursor, and compatible clients to request
  focused context on demand.
- [ ] Add tests proving MCP responses come from the same compiled policy bundle
  and ETHOS skill data used by hooks.

Acceptance criteria:

- [ ] Agents can query whether a proposed file path, command, or edit violates
  policy before attempting the action.
- [ ] MCP responses are compact, auditable, and linked to ETHOS principles and
  skill IDs.
- [ ] The server does not create a bypass path around hook enforcement.

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

- [ ] Define and version a stable policy object model for CEL inputs covering
  command, argv, tool, event, provider, cwd, repo, path, paths, file, files,
  diagnostic, finding, diff, Git facts, config facts, and safe metadata.
- [ ] Remove aspirational CEL fields: every exposed field must be populated
  reliably for its scope, or removed until the runtime can provide it.
- [ ] Populate real typed inputs for hook command scope, file/path scope, lint
  finding scope, Git scope, config scope, and diff scope.
- [ ] Replace first-file `path` semantics with explicit multi-file collection
  semantics such as `paths.exists(...)`, `paths.all(...)`,
  `files.changed_matching(...)`, and `findings.exists(...)`.
- [ ] Make dispatch policy-driven so expression config declares hook events,
  tools, lint tools, modes, defense layers, principle IDs, and skill IDs
  without hardcoded evaluator registration.
- [ ] Compile and cache CEL programs during bundle compilation or bundle load
  rather than recompiling expressions at evaluation time.
- [ ] Add controlled policy inheritance and override rules for expression
  policies, including forbidden shadowing of protected built-ins and explicit
  rules for severity weakening.
- [ ] Ensure every CEL policy emits the same normalized result shape as Go
  evaluators: policy ID, severity, decision, message, suggestion, principle
  IDs, skill ID, evidence, diagnostic location, remediation hint, and
  explanation metadata.
- [ ] Expand the reviewed helper library with pure typed helpers for glob
  matching, path classification, test/generated/protected detection, lint code
  matching, command-tool detection, inline-env detection, repo config
  presence, and protected-branch facts.
- [ ] Add first-class explain output for CEL decisions showing the expression,
  available input schema, helper functions, matched evidence, ETHOS grounding,
  and skill/remediation path.
- [ ] Keep the CEL boundary pure by design: Go prepares facts; CEL decides over
  facts. CEL must not read files, execute shell/Git, inspect environment,
  access the network, or depend on wall-clock time.
- [ ] Add operator documentation for supported scopes, input schemas, helper
  functions, dispatch, severity, examples, anti-patterns, and migration rules.
- [ ] Add a trust-building test matrix for unknown fields, type failures,
  unknown helpers, multi-file semantics, hook/lint dispatch, inheritance,
  shadowing, explain-output golden files, trace output, malicious config, and
  performance with many expression policies.

Acceptance criteria:

- [ ] Repo policy authors can express most simple and medium-complexity rules
  in checked-in ethos/config YAML without changing Go source.
- [ ] Policy authors get compile-time failures for unknown fields, invalid
  types, invalid helpers, unsafe host access, and invalid dispatch.
- [ ] Direct hook, agent-hook, lint-capture, explain, trace, CI, and future MCP
  paths cannot distinguish CEL-backed and Go-backed policies except by
  implementation metadata.
- [ ] Multi-file and multi-finding policies are explicit and deterministic; no
  policy depends on implicit "first file" ordering.
- [ ] Protected core policies remain non-shadowable and non-weakenable unless a
  protected source explicitly permits it.
- [ ] Go evaluators remain only for complex parsing, expensive analysis, Git
  state modeling, managed toolchain behavior, path normalization, and other
  reviewed security-sensitive operations.

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

- [ ] Add SARIF output for normalized policy and lint diagnostics.
- [ ] Provide native GitHub Actions and GitLab CI examples or reusable
  components.
- [ ] Ensure CI runs the same compiled policy bundle and managed toolchain
  versions as local hooks.
- [ ] Publish violations as PR annotations and, where supported, security/code
  scanning findings.

Acceptance criteria:

- [ ] A repo can gate PRs in CI even if local hooks are bypassed.
- [ ] SARIF includes policy IDs, ETHOS principle IDs, skill IDs, file/line
  locations, remediation advice, and stable rule metadata.
- [ ] CI output remains compact for agents while preserving full artifacts for
  audit.

### Adversarial Red-Team Test Suite

- [ ] Build an automated red-team harness that asks LLM agents to bypass or
  tamper with `coding-ethos` protections.
- [ ] Cover protected paths, raw Git bypasses, absolute binaries, nested shell
  execution, symlink/path traversal, config drift, hook deletion, and managed
  toolchain evasion.
- [ ] Capture both successful blocks and any missed bypass attempts as
  reproducible fixtures.
- [ ] Add regression tests for every bypass class discovered.

Acceptance criteria:

- [ ] Red-team scenarios can run in isolated sample repositories without
  touching the parent repo.
- [ ] Each bypass attempt produces either a clear block or a filed gap with a
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
