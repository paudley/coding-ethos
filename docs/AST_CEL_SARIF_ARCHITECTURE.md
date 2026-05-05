<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# AST, CEL, and SARIF Architecture

`coding-ethos` policy work follows one architecture:

1. Go collects facts.
2. CEL decides policy when the rule can be expressed over those facts.
3. SARIF reports exact, stable, remediation-ready findings.
4. The code-intelligence store retains the evidence for later agent search.

This is the first path to use for new source-aware enforcement. Do not add a
new ad hoc text scanner, one-off AST traversal, or policy-specific parser unless
the shared fact path cannot represent the required input yet. In that case,
extend the fact collector first, then expose the new fact through CEL, SARIF,
MCP, and code-intelligence storage from the same normalized record.

The goal is not "AST everywhere" for its own sake. The goal is one inspectable
pipeline for source-aware policy:

```text
file/edit/diff/command
|
v
Go parser and context collectors
|
+--> normalized facts
|    python_ast
|    shell_commands
|    proposed_symbol_changes
|    changed_symbols
|    tool_capabilities
|
+--> CEL policy decisions
|
+--> diagnostics and agent_remediation
|
+--> SARIF with AST identity and fingerprints
|
+--> SQLite code-intel store, FTS5, and sqlite-vec metadata
|
+--> MCP search, policy explanation, and remediation tools
```

## Responsibilities

### Go Fact Collection

Go owns parsing and host inspection. It is responsible for:

- Tree-sitter parsing and parser lifecycle.
- File, command, shell, Git, diff, and hook context collection.
- Normalized fact records with stable field names.
- Syntax recovery and fail-fast errors where policy cannot run safely.
- Diagnostic location metadata: file, line, column, end line, node kind, symbol
  kind, symbol path, and parent symbol path.

For Python, the reusable fact surface is `python_ast`. It is populated from the
same Tree-sitter fact collector used by the compiled Python evaluators. It
includes imports, calls, functions, classes, assignments, lambdas, exception
handlers, symbol context, ancestry flags, and initial function signature facts.

For shell, the reusable fact surface is `shell_commands`. It is populated from
the full shell parser, not substring matching. It exposes command names, argv,
assignments, redirects, write targets, heredocs, command substitutions, process
substitutions, subshells, dynamic expansion, background execution, and
line/column metadata. Git routing, lint capture, malformed shell checks, and
command-safety CEL rules should consume these parsed facts instead of scanning
raw command text.

For changed source, `proposed_symbol_changes` and `changed_symbols` map
Edit/Write/MultiEdit payloads and staged diff hunks to Tree-sitter symbols.
They include the file, language, node kind, symbol kind/name/path, line spans,
content hashes, action, and line-count delta. Large-file and large-symbol
policies use these symbol deltas so shrinking refactors remain allowed while
growth is blocked before commit.

For persisted context, code-intelligence indexing uses the same parser
foundation to store code chunks, config entries, parent/child relationships,
graph edges, parser metadata, and AST-to-finding links in
`.coding-ethos/code-intel.db`. This keeps MCP retrieval and future embedding
search on the same source identity used by CEL and SARIF.

### CEL Policy Decisions

CEL owns configurable policy predicates. If a decision can be expressed as a
boolean over normalized facts, put that decision beside its ETHOS principle in
`coding_ethos.yml` or the compiled policy configuration.

Examples:

```cel
python_ast.exists(fact, fact.is_dynamic_import)
```

```cel
python_ast.exists(
  fact,
  fact.node_kind == "function_definition" &&
  fact.parameter_count > 5
)
```

Do not put parsing logic, path probing, Git execution, or semantic extraction in
CEL. Add or extend Go facts instead, then keep the policy expression small and
auditable.

### SARIF Reporting

SARIF owns durable machine-readable output. AST-backed findings must preserve:

- `ruleId` from the policy ID.
- Exact artifact location and region.
- AST properties such as `ast_node_kind`, `ast_symbol_kind`,
  `ast_symbol_path`, and `ast_parent_symbol_path`.
- Partial fingerprints that remain stable across unrelated line movement.
- Principle and skill metadata so GitHub code scanning and MCP remediation
  advice point to the same explanation.

The same normalized finding should also be ingestible by the code-intelligence
store. SARIF should carry enough stable metadata for later joins to code
chunks, remediation outcomes, hook usage analytics, and vector metadata without
reinterpreting the original policy.

## Extension Workflow

When porting guidance from `pyqa_lint` or adding another source-aware policy:

1. Identify the fact shape needed by the rule.
2. Extend the shared Go fact collector if the fact does not exist.
3. Add CEL schema coverage for the fact when the decision should be
   configurable.
4. Express the policy in CEL when possible.
5. Emit diagnostics through the existing policy path so SARIF receives the AST
   metadata automatically.
6. Persist or link the evidence in code-intelligence storage when it should be
   searchable later.
7. Add tests at the fact, CEL, evaluator, SARIF, and storage layers proportional
   to the
   new behavior.

## Anti-Patterns

- Do not use substring matching for shell or source policy when a parsed fact
  surface exists.
- Do not add a policy-specific Tree-sitter traversal when the shared chunk/fact
  collector can be extended.
- Do not put parsing, path probing, or Git execution inside CEL.
- Do not emit SARIF locations that are not grounded in real files and spans.
- Do not create a second MCP-only interpretation of a policy. MCP should query
  the compiled bundle, retained traces, SARIF, and code-intel records.
- Do not hide current gaps behind optional build tags or degraded paths. If a
  fact is required for enforcement, it must be in the normal build and test
  path.

## Current Python Fact Uses

The current Python AST policies already use this path for:

- Conditional import enforcement:
  - nested imports
  - `TYPE_CHECKING` imports
  - `except ImportError` / `ModuleNotFoundError`
  - module `__getattr__`
  - `__import__`
  - `importlib.import_module`
- Functional idiom guidance:
  - assigned lambdas
  - returned or assigned nested closure factories

Future ports from `pyqa_lint` should add facts or CEL predicates for strict
typing, signature width, docstring sections, value-type dunder inference,
interface boundaries, DI composition roots, cache wrappers, Python hygiene, and
package documentation conventions.
