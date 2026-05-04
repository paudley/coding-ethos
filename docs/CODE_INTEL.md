<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Code Intelligence Roadmap

`coding-ethos` should grow from policy enforcement into a local code
intelligence substrate for agents. The target is not a generic RAG database.
The target is ETHOS-grounded code and remediation memory that helps agents find
the right code, understand prior failures, and choose the enforced repair path
before they run broad shell commands or repeat failed edits.

## Design Position

Use one repo-local storage substrate with separate logical responsibilities:

- **SQLite is the canonical fact store.** It owns traces, policy decisions,
  remediations, outcomes, code symbols, AST graph edges, file metadata, and
  full-text search.
- **sqlite-vec is the active vector backend.** It keeps embeddings, metadata
  filters, and similarity search inside repo-local SQLite files through the
  pure-Go `modernc.org/sqlite/vec` integration, with no native sidecar, daemon,
  hosted service, or conditional build path.

Vector indexes are derived artifacts. They must be rebuildable from the
canonical SQLite store and the checked-out repository contents.

## Goals

- Give agents semantic code search without arbitrary line chunking.
- Connect policy failures to the code structures that caused them.
- Let agents search prior remediations by policy, skill, file, command shape,
  and semantic similarity.
- Support local-first, repo-owned operation under `.coding-ethos/`.
- Keep hosted services optional and never required for local enforcement.
- Make every retrieved result auditable back to a trace, file, symbol, policy,
  skill, or SARIF result.

## Non-Goals

- Do not make vector storage the source of truth.
- Do not require a daemon, hosted vector database, or network access for normal
  local policy enforcement.
- Do not index secrets, ignored credential directories, `.git` internals, or
  generated enforcement artifacts that should remain protected.
- Do not replace exact policy checks, CEL rules, static analysis, or hooks with
  probabilistic retrieval.

## Architecture

```text
repository
  files
  traces
  SARIF
  policy bundle
      |
      v
Tree-sitter AST extraction
      |
      +--> SQLite canonical store
      |      files
      |      symbols
      |      AST nodes
      |      graph edges
      |      decisions
      |      remediations
      |      attempts
      |      FTS5 indexes
      |
      +--> sqlite-vec vector backend
             code/remediation vectors
             metadata-filtered similarity search
             pure-Go modernc.org/sqlite/vec
```

## Foundation Already In Place

The first enabling layer is the normalized evidence model in
`go/internal/evidence`. It gives CEL, SARIF, traces, lint diagnostics, hook
decisions, and future code intelligence one shared contract:

- `SourceSpan`: path, language, line/column span, byte span, symbol name,
  symbol kind, and content hash.
- `Finding`: stable finding ID, rule/tool/code/message, policy ID, skill ID,
  ETHOS principle IDs, evaluator kind, search text, and source span.
- `Envelope`: policy-evidence wrapper for future ingestion and explanation.
- `RemediationEvent`: lifecycle event that links a remediation ID to a finding
  ID and trace ID.
- `FindingStore`, `CodeFactStore`, `VectorIndex`, and `TraceIngestor`: narrow
  interfaces that keep storage and vector backends replaceable.

SARIF result properties now carry the normalized finding, source span, search
text, remediation payload, and remediation events. Hook and lint traces carry a
schema version, trace ID, normalized findings, remediation summaries, and
remediation events. This makes code intelligence an ingestion problem instead
of a second policy interpretation layer.

CEL inputs also expose code-intelligence fields under
`source` and `finding`: language, symbol name, symbol kind, chunk hash, line
counts, changed lines, prior failures, and recent remediations. These fields
let principles move toward source-aware policy without changing the CEL
contract.

The first storage layer now lives in `go/internal/codeintel`. It creates the
canonical `.coding-ethos/code-intel.db` SQLite store, ingests retained lint and
hook traces, stores normalized findings/remediations/remediation events, and
builds an FTS5 search table over policy IDs, skill IDs, paths, messages, and
remediation text. It intentionally treats vectors as a later derived index:
the SQLite store is the auditable source of truth.

## Canonical SQLite Store

The first implementation should create `.coding-ethos/code-intel.db` with
tables for:

- repositories and worktrees
- indexed files with content hashes
- AST chunks with stable chunk IDs, byte ranges, line ranges, language, symbol
  type, symbol name, and parent symbol
- graph edges for imports, definitions, references, calls, inheritance, tests,
  and documentation links when language support allows
- hook traces, lint traces, SARIF result references, policy decisions, and
  remediation payloads
- remediation attempts and outcomes keyed by stable remediation ID
- embedding metadata: model, provider, dimension, input kind, chunk hash, and
  vector backend row ID

Use FTS5 for text, symbol, file path, policy, skill, command, and advice
search. FTS is not a fallback; it is part of hybrid retrieval.

Initial command surface:

```bash
bin/coding-ethos-run code-intel ingest-traces
bin/coding-ethos-run code-intel stats
bin/coding-ethos-run code-intel repeated-failures --policy-id python.unused_imports
bin/coding-ethos-run code-intel index-code pkg scripts config.yml
bin/coding-ethos-run code-intel code-chunks --path pkg/app.go --symbol-name BuildMessage
bin/coding-ethos-run code-intel ingest-sarif --file policy.sarif
bin/coding-ethos-run code-intel sarif-results --policy-id python.unused_imports
bin/coding-ethos-run code-intel remediation-outcomes --outcome repeated
bin/coding-ethos-run code-intel remediation-effectiveness --policy-id python.unused_imports
bin/coding-ethos-run code-intel embedding-candidates --record-kind remediation_outcome
bin/coding-ethos-run code-intel upsert-vector --id rem-1 --model-id voyage-code-3 --vector '0.1,0.2,0.3'
bin/coding-ethos-run code-intel embedding-records --backend sqlite-vec
bin/coding-ethos-run code-intel hybrid-search --text 'unused import' --model-id voyage-code-3 --vector '0.1,0.2,0.3'
bin/coding-ethos-run code-intel index-status --model-id voyage-code-3
bin/coding-ethos-run code-intel search --text 'unused import'
```

These commands read retained `.coding-ethos` traces and write only the
repo-local `.coding-ethos/code-intel.db` store.

## `remediation_outcomes_1` Branch Plan

This branch should make the local store useful as the durable evidence ledger
for CEL, SARIF, and remediation outcomes before AST/vector indexing lands.
The target is a complete storage foundation, not a minimal placeholder.

SQLite remains canonical and should gain:

- SARIF run records keyed by stable run ID, trace ID, source path, tool,
  automation/category metadata, baseline/run GUIDs, and raw payload.
- SARIF result references keyed by stable result ID, rule ID, fingerprint,
  policy ID, skill ID, ETHOS principle IDs, path/span, severity, message,
  normalized finding ID, remediation ID, CEL/evaluator provenance, and raw
  result JSON.
- Remediation outcomes keyed by remediation ID, finding ID, source trace,
  follow-up trace, policy ID, skill ID, file/path, provider/tool, attempt
  ordinal, and outcome (`suggested`, `attempted`, `fixed`, `repeated`,
  `superseded`, or `unknown`).
- Embedding metadata records for sqlite-vec rows: backend,
  model ID, dimension, input kind, record kind, record ID, content hash,
  provider, policy ID, skill ID, path, and backend row ID.
- FTS rows for SARIF results, remediation outcomes, and vector-ready source
  text so exact search works before embeddings exist.
- Embedding candidate rows for SARIF results, emitted remediation packets, and
  remediation outcomes so an approved embedding producer can write vectors back
  without reading raw trace JSON.

The first query/CLI surface should answer:

- which SARIF results are stored for a policy, skill, path, or trace;
- which remediation advice later led to fixed or repeated outcomes;
- which policies/skills produce repeated findings after advice was issued;
- which records are ready for embedding and which vector backend metadata rows
  already exist.
- which prior fixes are most relevant through hybrid FTS + sqlite-vec search,
  with fixed outcomes boosted and repeated/superseded outcomes downranked.

Vector work on this branch uses always-built sqlite-vec tables. Metadata stays
in normal SQLite tables for auditability and filtering, while vectors live in
dimension-specific `vec0` virtual tables. This preserves the ETHOS one-path
build contract and keeps vector indexes rebuildable from SQLite facts.

## Vector Backends

Define a narrow vector backend interface before binding to any implementation:

- `UpsertEmbedding(record)`
- `DeleteEmbedding(chunk_id, model_id)`
- `Search(query_embedding, filters, limit)`
- `Stats()`
- `Rebuild(collection)`

The initial backend priority:

1. sqlite-vec search for AST-aware code and remediation vectors.
2. Future service backends only if team-scale deployment needs them and they
   can be kept outside local enforcement.

Backend records must include enough metadata to filter before or during vector
search:

- repo/worktree ID
- file path
- language
- symbol kind
- policy ID
- skill ID
- provider/tool
- trace ID
- remediation ID
- model ID

## AST Chunking

Use Tree-sitter to produce semantic chunks instead of line windows:

- module/package
- class/type/interface
- function/method
- test case
- block-level fallback for large functions
- doc/comment chunk linked to the nearest symbol

Every chunk should have:

- stable chunk ID based on repo ID, path, language, node kind, symbol path, and
  content hash
- byte and line span
- parent/child relationships
- extracted signature where the language supports it
- imports/references edge data when available

Incremental indexing is required. A changed file should only re-embed changed
chunks and dependent summary chunks when their content or structural hash
changes.

The active implementation indexes Go, Python, JavaScript/TypeScript, shell,
and YAML through Tree-sitter. Each indexed file is written to the canonical
SQLite store with a content hash and line count. Each extracted symbol or
configuration entry is written as a stable `code_chunk`, mirrored into FTS5,
and exposed as an embedding candidate with `record_kind=code_chunk`. Markdown
remains planned until the selected parser exposes a maintained Go binding or
the project adds a first-class adapter for its split parser layout.

## Embedding Strategy

Embedding providers must be pluggable and recorded per vector row.

Recommended initial model classes:

- code-optimized remote model for best retrieval quality, such as
  `voyage-code-3`
- local/open model for offline operation, such as a Jina code embedding model
  or an Ollama-served code-capable embedding model
- general embedding fallback only when a code-specific model is unavailable

Queries should use the matching query/document input mode when the provider
supports it. The system must refuse to compare vectors across incompatible
model IDs or dimensions.

## Hybrid Retrieval

Search should combine exact filters, FTS, vectors, and reranking:

1. Apply hard filters: repo, path prefix, language, policy, skill, symbol kind,
   provider, or time range.
2. Retrieve candidates from FTS5.
3. Retrieve candidates from the vector backend.
4. Fuse ranks with reciprocal rank fusion.
5. Boost exact symbol/path/policy matches.
6. Return traceable results with file spans and policy/remediation context.

Vector-only retrieval should be available for diagnostics, but not the default
agent path.

## MCP Tool Surface

Add tools only after the store has a stable schema:

- `code_intel_search`: hybrid semantic/FTS search over stored SARIF,
  remediation, and AST chunk memory.
- `code_intel_index_code`: refresh Tree-sitter code chunks for selected paths.
- `code_intel_code_chunks`: return focused symbol/config chunks by path,
  language, symbol kind, or symbol name.
- `code_intel_embedding_candidates`: return compact SARIF/remediation/code
  chunk records for an approved embedding producer.
- `code_intel_context`: expand a selected chunk into parent, children,
  references, related tests, and recent policy failures.
- `remediation_history_search`: find prior remediations by policy, skill,
  command shape, file path, semantic similarity, and outcome.
- `code_intel_index_status`: report freshness, changed files, embedding model,
  backend, and failed indexing tasks.
- `code_intel_explain_result`: explain why a search result was returned,
  including FTS score, vector score, filters, and policy links.

All tools are advisory. They must not bypass hooks or edit files.

## Phases

### Phase 1 - Schema and Trace Ingestion

- [x] Create the SQLite store and migrations.
- [x] Persist hook/lint trace summaries into normalized tables.
- [x] Index `agent_remediation` payloads and remediation events.
- [x] Add FTS5 over policies, skills, messages, advice, commands, files, and tool
  output summaries.
- [x] Persist SARIF result references into normalized tables.
- [x] Track remediation outcomes after follow-up attempts.
- [x] Store CEL/evaluator provenance beside findings and SARIF results.
- [x] Store vector-backend metadata records for sqlite-vec
  derived indexes.
- [x] Expose embedding candidates for SARIF/remediation records.

Acceptance criteria:

- [x] Existing `.coding-ethos` lint and hook traces can be imported.
- [x] Search can answer "show repeated failures for this policy/skill/file."
- [x] No vector backend is required for this phase.
- [x] Search can answer "show SARIF results for this policy/skill/file."
- [x] Search can answer "which remediation suggestions were fixed, repeated,
  or attempted again."
- [x] SQLite can identify records that are ready for embedding and the
  repo-local sqlite-vec backend can search stored embeddings.

### Phase 2 - AST Indexing

- [x] Add Tree-sitter extraction for the first language set: Go, Python, YAML,
  shell, JavaScript/TypeScript.
- [x] Store AST chunks, symbol metadata, byte ranges, line ranges, content
  hashes, and search text in SQLite.
- [x] Expose AST chunks through FTS, embedding candidates, CLI, and MCP.
- [ ] Add Markdown once the parser binding strategy is explicit.
- [ ] Store basic graph edges in SQLite.
- [ ] Add incremental reindex by file hash and chunk hash.

Acceptance criteria:

- [ ] Editing one function reindexes only that file and re-embeds only changed
  chunks.
- [x] Search results include stable file/line spans and symbol identity.

### Phase 3 - sqlite-vec Vector Backend

- [x] Add the vector backend interface.
- [x] Implement sqlite-vec storage for remediation embeddings.
- [x] Record model ID, dimension, provider, and input kind for every vector.
- [x] Add rebuild and index-status reporting.
- [x] Keep the vector backend in the normal `make build` and `make check`
  path with no build tags, native artifacts, or daemon requirements.

Acceptance criteria:

- [x] Code and remediation vectors can be rebuilt from SQLite plus repo
  contents.
- [x] Hybrid search can filter by path/language/policy before ranking.

### Phase 4 - Hybrid Retrieval Hardening

- Tune FTS + sqlite-vec ranking once AST chunks and remediation embeddings are
  populated.
- Add stale-index reporting for changed files and changed remediation records.
- Document capability and ranking differences from FTS-only search.

Acceptance criteria:

- Users can run code intelligence with the default sqlite-vec backend.
- Index status reports backend, model, stale row counts, and rebuild needs.

### Phase 5 - MCP Search Tools

- [x] Add code intelligence MCP tools for search, index status, embedding
  candidates, code indexing, and chunk lookup.
- [x] Return compact, traceable result packets suitable for agent context
  windows.
- [ ] Include follow-up MCP calls for expanding results or explaining policy
  links.

Acceptance criteria:

- [x] Agents can find relevant stored remediation/SARIF/code-chunk evidence
  through MCP before broad grep/file reads.
- [ ] Results include enough evidence to justify why the match was returned.

### Phase 6 - Outcome Measurement

- Track whether remediation suggestions reduce repeated failures.
- Link remediation attempts to later hook/lint outcomes.
- Add local reports for repeat failures, stale embeddings, noisy policies, and
  missing skill mappings.

Acceptance criteria:

- The project can measure repeated policy failures before and after
  remediation guidance.
- Policy authors can identify which rules need better examples, skills, or
  evidence maps.

## Risks and Mitigations

- **Secret leakage:** respect protected paths, ignored credential directories,
  and sandbox read policies before indexing.
- **Stale vectors:** store chunk hashes and model IDs; refuse stale results
  unless explicitly requested.
- **Backend lock-in:** keep vectors derived and route all backend access
  through a narrow interface.
- **Poor code retrieval:** use AST chunks, code-specific embeddings, hybrid
  retrieval, and result explanation instead of vector-only search.
- **Context bloat:** return compact result packets and require explicit
  expansion calls for surrounding context.

## Open Questions

- Which code embedding model should be the initial default for local/offline
  users?
- When does sqlite-vec search become too slow for repo-local remediation and
  AST retrieval?
- How much graph detail is useful before it becomes expensive to maintain?
- Should code intelligence be enabled by default, or only after explicit
  `make code-intel-index` setup?
