<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Policy Language Strategy

## Decision

Use CEL as the first embedded policy language for `coding-ethos`.

Keep OPA/Rego as a possible later backend for policy classes that demonstrably
need package-level rules, partial evaluation, large static data sets, or complex
set joins.

## Why CEL First

`coding-ethos` already owns the hard parts that must stay compiled and
deterministic:

- Git state discovery
- staged file resolution
- managed toolchain execution
- hook and agent event parsing
- normalized diagnostics
- ETHOS principle mapping
- skill hint rendering
- trace logging

The missing layer is a safe way for repo and organization owners to express
custom boolean policy over that normalized data without editing Go. CEL is a
good fit for this layer because it is embedded, typed, non-Turing-complete,
fast, and designed for safe expression evaluation.

That shape matches the first policy-language target:

```yaml
policy:
  expressions:
    - id: python.no_repo_specific_cache_import
      scope: diagnostics
      severity: block
      principle_ids:
        - protocol-first-design
      skill_id: conditional-imports
      when: >
        diagnostic.tool == "ruff" &&
        paths.exists(path, path.file.startsWith("lib/python/")) &&
        diagnostic.message.contains("private cache module")
      message: Import through the public cache protocol.
      advice: Define or reuse a protocol boundary instead of importing private cache internals.
```

The expression decides whether the normalized event matches. Go still controls
where data comes from, how paths are resolved, how policy is compiled, and how
decisions are rendered.

## Why Not Rego First

OPA/Rego is a full policy engine. It is excellent when the policy itself needs
rich data querying, package-level rule structure, partial evaluation, or reuse
across infrastructure systems.

Those strengths are not the first missing capability in `coding-ethos`.
Starting with Rego would add a larger language, larger runtime, and larger
authoring model before the project has proven it needs those features. It would
also make simple repo-local checks harder to read for developers who only need
expression-level policy.

Rego remains useful later if CEL policies become contorted around:

- joins across large static data sets
- multi-rule package composition
- partial evaluation or precomputed policy fragments
- enterprise reuse of existing OPA policy libraries
- CI, admission-control, or infrastructure policy that should share policy
  source with `coding-ethos`

## Policy Boundary

CEL policy must be pure policy over provided input. It must not perform host
inspection.

Allowed:

- inspect structured input fields
- inspect normalized diagnostics and findings
- compare strings, booleans, numbers, lists, and maps
- use reviewed helper functions supplied by Go
- reference static compiled bundle data

Not allowed:

- file IO
- Git execution
- shell execution
- network calls
- clock/time dependence unless the timestamp is supplied as input
- environment-variable reads
- access to paths outside the normalized input model

This keeps expression policy deterministic, replayable, and suitable for hook,
agent-hook, lint-capture, CI, and MCP execution.

## Configuration Model

Add expression-backed policy under config, not under ad hoc custom code:

```yaml
policy:
  expressions:
    - id: shell.no_inline_python_subprocess_git
      description: Block Python subprocess attempts to run Git.
      scope: command
      severity: block
      mode: block
      protected: true
      allow_override: false
      allow_severity_weaken: false
      hook_events: [PreToolUse]
      tools: [Bash]
      lint_scopes: [staged, files]
      command_patterns: [subprocess]
      principle_ids:
        - one-path-for-critical-operations
        - no-rationalized-shortcuts
      skill_id: safe-git-workflow
      when: >
        command.contains("subprocess") &&
        command.contains("git")
      message: Git must go through the coding-ethos wrapper.
      advice: Use the protected git wrapper path and keep hook failures visible.
```

Required fields:

- `id`
- `scope`
- `severity`
- `principle_ids`
- `when`
- `message`
- `advice`

Optional fields:

- `description`
- `skill_id`
- `mode`
- `hook_events`
- `tools`
- `lint_scopes`
- `command_patterns`
- `path_patterns`
- `protected`
- `override`
- `override_reason`
- `allow_override`
- `allow_severity_weaken`
- `tags`
- `metadata`

The compiler should reject expressions without ETHOS grounding. A custom rule
that cannot explain which principle it enforces is not mature enough to block
agent behavior.

Repo overlays append expression policies instead of replacing the primary
bundle list. Duplicate IDs are rejected unless the replacement declares
`override: true`, supplies `override_reason`, and the existing expression
declares `allow_override: true`. Built-in Go-backed policies and protected
expressions therefore cannot be shadowed accidentally. Severity weakening is
also rejected unless the existing expression declares
`allow_severity_weaken: true`, and protected expressions default to enabled
with `protected: true`.

## Input Schemas

Start with small stable input objects:

- `command`: raw command text, parsed argv, tool name, cwd, provider, event
- `path`: compatibility object populated only when exactly one path is in
  scope; multi-file policy must use `paths`
- `paths`: list of repo-relative path objects with file, extension, basename,
  directory, generated/test flags, and source-root classification
- `diagnostic`: populated only when the caller supplies a real diagnostic;
  includes tool, code, message, file, line, column, severity, and policy ID
- `finding`: populated only when the caller supplies a real normalized finding;
  includes tool, code, message, file, line, severity, policy ID, skill ID, and
  principle IDs
- `repo`: repo root metadata, configured source roots, language settings,
  enabled capabilities
- `metadata`: event ID, scope, provider, and non-sensitive trace IDs

The current CEL object model is versioned through
`metadata.schema_version == 1`. Do not expose raw environment, arbitrary
filesystem contents, or host paths.

Generic hook command and file/path policies should treat `diagnostic` and
`finding` as empty unless they are running in a diagnostic or finding-specific
evaluation path. The runtime must not synthesize those objects from the first
file, the current tool, or other partial context; missing facts are safer than
plausible fake facts.

## Runtime Plan

1. Add config schema and loader support for `policy.expressions`.
2. Compile CEL expressions into the policy bundle with a typed environment for
   each scope.
3. Store the original expression, checked AST/program metadata, input schema
   version, ETHOS mapping, and output template fields in the bundle.
4. Add a compiled expression evaluator to the existing evaluator registry.
5. Convert CEL matches into normal `policy.Decision` and
   `diagnostics.Diagnostic` values.
6. Render expression results through the existing TOON, JSON, human, trace, and
   skill-hint paths.
7. Add `policy explain` support for expression-backed policies.
8. Add golden tests for command, path, diagnostic, and finding policies.

## Generic Engine Completion Criteria

The first CEL milestone is intentionally smaller than a full generic policy
engine. It gives repositories a typed extension point for simple custom
predicates while preserving Go evaluators for core enforcement. CEL becomes a
complete generic policy engine only when policy authors can express most simple
and medium-complexity repo rules without knowing whether the implementation is
Go-backed or CEL-backed.

The required completion work is:

1. **Stable object model.** Define a versioned schema for every CEL-visible
   object. The current public surface includes `command`, `command_fact`,
   `argv`, `cwd`, `metadata`, `repo`, `path`, `paths`, `files`, `diagnostic`,
   `diagnostics`, `finding`, `findings`, `config`, `git`, `git_command`,
   `event`, `diff`, and non-sensitive metadata. Future provider-native event
   fields, diff hunks, line ranges, richer Git state, and richer config facts
   must be added only when every relevant runtime can populate them reliably.
   Once exposed, these fields are public policy API.
2. **Real typed inputs.** Remove aspirational fields. Hook command, file/path,
   lint finding, Git, config, and diff scopes must either populate each field
   reliably or not expose it. The current Git and config surfaces expose
   prepared facts such as hook event/provider/tool/scope, configured protected
   branches, current branch, protected path matches, changed/staged file sets,
   normalized Git argv/subcommand/flag facts, config candidates, config files
   present in the current file set, and normalized diagnostic/finding
   collections; they do not read Git or the filesystem from CEL.
3. **Explicit multi-file semantics.** Replace implicit first-file behavior with
   collection expressions such as `paths.exists(path, ...)`, `paths.all(path, ...)`,
   `files.changed_matching(...)`, and `findings.exists(...)`.
4. **Dispatch as policy.** Expression config declares where a policy runs:
   hook events, tool matchers, lint tools, modes, defense layers, principle
   IDs, and skill IDs. Runtime registration is derived from compiled policy
   data rather than special-case wiring. Scope-only expressions keep
   compatibility defaults, but new policies should specify `hook_events`,
   `tools`, `lint_scopes`, and `mode` explicitly.
5. **Compiled and cached programs.** CEL syntax and type checking must fail
   bundle compilation. Runtime should reuse checked programs from the compiled
   bundle or from a load-time cache instead of compiling on each evaluation.
6. **Controlled inheritance and overrides.** Custom policies must not shadow
   protected built-ins. Repo overlays append expression policies; duplicate IDs
   require `override: true`, `override_reason`, and `allow_override: true` on
   the existing expression. Severity weakening additionally requires
   `allow_severity_weaken: true`; protected expressions default to enabled and
   cannot be disabled.
7. **Standard result model.** CEL-backed policies must emit the same decision
   data as Go evaluators: policy ID, severity, decision, message, suggestion,
   principle IDs, skill ID, evidence, diagnostic location, remediation hint,
   and explanation metadata.
8. **Reviewed helper library.** Helpers encode pure, reusable policy facts such
   as glob matching, path classification, generated/test/protected path
   detection, lint-code matching, command-tool detection, inline-env detection,
   repo-config presence, and protected-branch facts.
9. **Explainability.** `policy explain` and trace output must show the
   expression, available schema, helper functions, matched evidence, ETHOS
   grounding, and skill/remediation path.
10. **Pure safety boundary.** Go prepares facts; CEL decides over facts. CEL
    must not read files, execute shell or Git, inspect environment variables,
    access the network, or depend on wall-clock time.
11. **Operator documentation.** Public docs must cover supported scopes, input
    schemas, helper functions, dispatch, severity, examples, anti-patterns,
    migration guidance, and when Go evaluators are still the right tool.
12. **Trust-building test matrix.** Tests must cover unknown fields, type
    failures, unknown helpers, multi-file semantics, hook and lint dispatch,
    inheritance and shadowing, explain-output golden files, trace output,
    malicious config, and runtime performance with many expression policies.

Until those criteria are met, CEL should be described as a typed custom-policy
extension point. Go evaluators remain the right implementation for complex
parsing, expensive analysis, Git state modeling, managed toolchain behavior,
path normalization, and other reviewed security-sensitive operations.

## Helper Functions

Keep helper functions small and reviewed. Initial candidates:

- `glob_match(pattern, value)`
- `any_glob_match(patterns, value)`
- `has_suffix(value, suffix)`
- `has_prefix(value, prefix)`
- `is_test_path(path)`
- `is_generated_path(path)`
- `is_protected_path(path, protected_paths)`
- `in_source_root(path, source_roots)`
- `lint_code_matches(code, pattern)`
- `command_invokes(command, tool)`
- `argv_invokes(argv, tool)`
- `has_inline_env(command, name)`
- `repo_config_present(files, candidates)`
- `is_protected_branch(branch, protected_branches)`
- `list_contains(values, value)`
- `any_has_prefix(values, prefix)`
- `any_has_suffix(values, suffix)`

Avoid helper functions that hide IO, policy decisions, or broad framework logic.

## Operator Reference

Supported expression scopes:

- `command`: evaluates command text, argv, tool metadata, and command facts.
- `files` and `staged`: evaluate explicit file/path collections and any lint
  or Git facts supplied by the caller.
- `diagnostic` and `finding`: evaluate one normalized diagnostic or finding at
  a time. Empty typed objects are used only when no diagnostic/finding scope is
  present, so expressions do not match fake lint data.

Core inputs:

- `event`: provider, event name, tool, scope, and mode prepared by the caller.
- `command_fact`: raw command, argv, tool, and inline-env detection over the
  raw command text.
- `paths`: explicit normalized file collection; `path` is populated only for
  single-file compatibility.
- `diagnostics` and `findings`: normalized collections supplied by lint/finding
  contexts. Single `diagnostic` and `finding` remain available for
  one-at-a-time policies.
- `git`: current branch, protected-branch flag, configured protected branches,
  protected-path file matches, staged files, and changed files.
- `diff`: the prepared file-level diff set: all files, changed files, staged
  files, and whether any change facts are present. Hunk and line-range facts
  are deliberately not exposed until a reviewed Go diff parser owns them.
- `config`: configured repo override candidates and candidates present in the
  current file set.

Dispatch is declared in `policy.expressions` with `hook_events`, `tools`,
`lint_scopes`, `mode`, `severity`, `principle_ids`, and `skill_id`.
Compatibility defaults exist for older expressions, but new policies should
declare dispatch explicitly so hook, lint, CI, trace, explain, and future MCP
paths all see the same compiled policy.

CEL remains pure. Go code prepares facts from trusted runtime context and
compiled configuration, then CEL decides over those facts. CEL helpers must not
read files, execute shell or Git, inspect process environment, open the network,
or depend on wall-clock time.

Anti-patterns:

- expression policies that depend on implicit `path` for multi-file operations;
  use `paths.exists(...)` or `paths.all(...)`
- command string matching where a first-class Go evaluator already provides
  safer parsing and diagnostics
- helpers that mix host inspection with policy decisions
- severity weakening or duplicate IDs without explicit controlled override
  metadata
- policies that expose new fields before every runtime path can populate them

Test requirements for new CEL features:

- compile-time rejection for unknown fields, unknown helpers, and invalid types
- positive and negative evaluation tests for every helper
- multi-file and multi-finding tests that avoid implicit first-file ordering
- hook, agent-hook, lint, explain, trace, and CI/SARIF parity tests when a
  feature affects output
- inheritance, shadowing, protected-policy, and severity-weakening tests for
  config behavior

## Migration Rule

Do not rush to replace compiled evaluators.

Move a hardcoded evaluator into CEL only when all of the following are true:

- the evaluator is pure matching logic over normalized input
- CEL makes the rule clearer than Go
- diagnostics remain at least as precise as before
- the rule has stable ETHOS and skill mappings
- tests prove parity against the previous evaluator

Critical safety primitives such as Git wrapper enforcement, staged file
resolution, managed toolchain resolution, config hash validation, and path
normalization stay in Go.

The first migrated built-ins prove the intended pattern:

- `git.change_dir_flag` is CEL over `git_command.has_change_dir`.
- `git.destructive_worktree` is CEL over `git_command.subcommand`,
  `git_command.args`, and `git_command.flags`.
- `git.stash_blocked` is CEL over `git_command.subcommand`.

These policies still use Go for argv normalization and fact preparation. CEL
only decides over the prepared facts.

Migration workflow:

1. Identify the smallest hardcoded evaluator branch that is pure matching over
   existing normalized input.
2. Write the CEL expression beside the Go evaluator in tests first and prove it
   matches the same positive and negative cases.
3. Confirm the generated decision keeps the same severity, policy ID, ETHOS
   principle IDs, skill ID, message, advice, and file attribution.
4. Run TOON, JSON, human, and trace-output tests before deleting the Go branch.
5. Keep the Go implementation if the CEL version needs extra host inspection,
   hidden helper complexity, weaker diagnostics, or broader suppressions.

## Rego Reconsideration Gate

Open a Rego design record only if CEL cannot express a real policy without
awkward or unsafe workarounds.

The design record must include:

- the concrete policy CEL cannot express cleanly
- why a Go evaluator is not the better answer
- how Rego would be compiled, cached, sandboxed, and traced
- how Rego output maps to existing diagnostics, ETHOS IDs, skill IDs, and TOON
  output
- how bundle size and runtime performance will be measured
