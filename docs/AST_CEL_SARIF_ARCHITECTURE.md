<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

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
|    similarity_facts
|    tool_capabilities
|
+--> CEL policy decisions
|
+--> diagnostics and agent_remediation
|
+--> SARIF with AST identity and fingerprints
|
+--> DuckDB code-intel store, FTS5, and duckdb-vss metadata
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

For hook command implementations, `hook_commands` maps Go command functions to
ordered call facts. Hook-stage commands that run path-sensitive checks in
`pre-commit` or `pre-push` must apply the hook-provided changed-file list before
invoking whole-surface or configured-path runners. CEL owns the policy decision;
Go owns the ordered call facts and diagnostic location for the offending command
function.

For persisted context, code-intelligence indexing uses the same parser
foundation to store code chunks, config entries, parent/child relationships,
graph edges, parser metadata, and AST-to-finding links in
`.coding-ethos/code-intel.duckdb`. This keeps MCP retrieval and future embedding
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

SARIF owns durable machine-readable output and is the superset of CEL evidence.
Everything coding-ethos observes and can safely retain belongs in SARIF:
normalized diagnostics, pathless tool-level failures, parser state, raw
stdout/stderr payloads, exit status, sandbox evidence, remediation metadata,
and source identity. CEL receives only the understood subset: stable facts and
diagnostics that can support deterministic policy decisions. Do not discard
observed evidence merely because it is not yet expressible in CEL.

AST-backed findings must preserve:

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

## Code Similarity Facts

The `similarity_facts` CEL variable exposes MinHash LSH-based code clone
detection results. The pipeline:

1. **Token normalization**: Tree-sitter chunks are normalized (identifiers →
   `$ID`, literals → `$STR`/`$NUM`) and hashed for Type-2 clone detection.
2. **MinHash signatures**: each chunk gets a 128-value MinHash signature stored
   in `code_chunks.minhash_sig`.
3. **LSH band storage**: signatures are split into 16 bands × 8 rows and stored
   in the `lsh_bands` table for sub-linear candidate retrieval.
4. **Exact match**: `normalized_hash` equality finds structurally identical chunks
   across files (Type-2 clones at 100% similarity).
5. **LSH candidate retrieval + Jaccard refinement**: band hash collisions produce
   candidates; full Jaccard estimation filters below the configured candidate
   threshold.

CEL receives `similarity_facts` as a list of match records, each carrying:

- `file`, `symbol_name`, `symbol_kind`, `symbol_path`, `language` (source chunk)
- `match_path`, `match_symbol_name`, `match_symbol_kind`, `match_start_line` (target)
- `similarity` (float64, 0.0–1.0)
- `exact_normalized` (bool, true when normalized_hash matches exactly)

The default policy expression:

```cel
similarity_facts.exists(fact, fact.similarity >= 0.8)
```

When the policy fires, `applySimilarityDiagnostic` enriches the diagnostic with:

- A human-readable message listing each match with path, line, and similarity
  percentage.
- SARIF `relatedLocations` entries pointing at each matching symbol so IDE
  integrations can navigate directly to the existing code.

The default `config.yaml` settings use a 0.7 LSH candidate threshold and 0.8
structural threshold. Repos can tune `similarity.candidate_threshold`,
`similarity.structural_threshold`, `similarity.min_symbol_lines`, and MinHash
shape parameters without changing the compiled code.

## Current Python Fact Uses

Future ports from `pyqa_lint` should add facts or CEL predicates for strict
typing, signature width, docstring sections, value-type dunder inference,
interface boundaries, DI composition roots, cache wrappers, Python hygiene, and
package documentation conventions.
