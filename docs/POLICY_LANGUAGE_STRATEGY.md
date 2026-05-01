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
        diagnostic.file.startsWith("lib/python/") &&
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
- `tags`
- `metadata`

The compiler should reject expressions without ETHOS grounding. A custom rule
that cannot explain which principle it enforces is not mature enough to block
agent behavior.

## Input Schemas

Start with small stable input objects:

- `command`: raw command text, parsed argv, tool name, cwd, provider, event
- `path`: repo-relative path, extension, basename, directory, generated/test
  flags, protected-path classification
- `diagnostic`: tool, code, message, file, line, severity, existing policy ID
- `finding`: normalized lint/policy finding with ETHOS and skill metadata
- `repo`: repo root metadata, configured source roots, language settings,
  enabled capabilities
- `metadata`: event ID, scope, provider, and non-sensitive trace IDs

Do not expose raw environment, arbitrary filesystem contents, or host paths.

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

## Helper Functions

Keep helper functions small and reviewed. Initial candidates:

- `path.matchesGlob(pattern)`
- `path.hasSuffix(suffix)`
- `path.hasPrefix(prefix)`
- `path.isTest`
- `path.isGenerated`
- `path.inSourceRoot`
- `diagnostic.hasCode(code)`
- `any(list, predicate)` / native CEL comprehensions where possible

Avoid helper functions that hide IO, policy decisions, or broad framework logic.

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
