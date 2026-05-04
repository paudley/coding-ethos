<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: MIT -->

# Code Intelligence Roadmap

`coding-ethos` should grow from policy enforcement into a local code
intelligence substrate for agents. The target is not a generic RAG database.
The target is ETHOS-grounded code and remediation memory that helps agents find
the right code, understand prior failures, and choose the enforced repair path
before they run broad shell commands or repeat failed edits.

## Design Position

Use two storage layers with separate responsibilities:

- **SQLite is the canonical fact store.** It owns traces, policy decisions,
  remediations, outcomes, code symbols, AST graph edges, file metadata, and
  full-text search.
- **LanceDB is the first vector backend for code-aware search.** It owns
  nearest-neighbor retrieval over AST chunks and remediation/code embeddings.
- **sqlite-vec/vec1 is the fallback vector backend.** It keeps the system
  single-file for environments that cannot or should not use LanceDB.

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
      +--> vector backend interface
      |      LanceDB code/remediation vectors
      |      sqlite-vec fallback vectors
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

CEL inputs also expose the future-facing code-intelligence fields under
`source` and `finding`: language, symbol name, symbol kind, chunk hash, line
counts, changed lines, prior failures, and recent remediations. These fields
are intentionally available before the AST indexer exists so principles can
move toward source-aware policy without changing the CEL contract later.

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
bin/coding-ethos-run code-intel search --text 'unused import'
```

These commands read retained `.coding-ethos` traces and write only the
repo-local `.coding-ethos/code-intel.db` store.

## Vector Backends

Define a narrow vector backend interface before binding to any implementation:

- `UpsertEmbedding(record)`
- `DeleteEmbedding(chunk_id, model_id)`
- `Search(query_embedding, filters, limit)`
- `Stats()`
- `Rebuild(collection)`

The initial backend priority:

1. LanceDB for AST-aware code and remediation vectors.
2. sqlite-vec/vec1 for single-file fallback and constrained environments.
3. Future optional service backends only if team-scale deployment needs them.

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

- `code_intel_search`: hybrid semantic/FTS search over AST chunks and
  remediation memory.
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
- [ ] Persist SARIF result references into normalized tables.
- [ ] Track remediation outcomes after follow-up attempts.

Acceptance criteria:

- [x] Existing `.coding-ethos` lint and hook traces can be imported.
- [x] Search can answer "show repeated failures for this policy/skill/file."
- [x] No vector backend is required for this phase.

### Phase 2 - AST Indexing

- Add Tree-sitter extraction for the first language set: Go, Python, Markdown,
  YAML, shell, JavaScript/TypeScript.
- Store AST chunks, symbol metadata, and basic graph edges in SQLite.
- Add incremental reindex by file hash and chunk hash.

Acceptance criteria:

- Editing one function reindexes only that file and re-embeds only changed
  chunks.
- Search results include stable file/line spans and symbol identity.

### Phase 3 - LanceDB Vector Backend

- Add the vector backend interface.
- Implement LanceDB storage for AST chunk and remediation embeddings.
- Record model ID, dimension, provider, and input kind for every vector.
- Add rebuild and stale-index detection.

Acceptance criteria:

- Code and remediation vectors can be rebuilt from SQLite plus repo contents.
- Hybrid search can filter by path/language/policy before ranking.

### Phase 4 - sqlite-vec Fallback

- Add sqlite-vec/vec1 backend behind the same interface.
- Use it for single-file and constrained local environments.
- Document capability differences from LanceDB.

Acceptance criteria:

- Users can run code intelligence without LanceDB.
- Backend selection is explicit and visible in index status.

### Phase 5 - MCP Search Tools

- Add code intelligence MCP tools.
- Return compact, traceable result packets suitable for agent context windows.
- Include follow-up MCP calls for expanding results or explaining policy links.

Acceptance criteria:

- Agents can find relevant code through MCP before broad grep/file reads.
- Results include enough evidence to justify why the match was returned.

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
- Should LanceDB be an optional dependency, a managed runtime asset, or a
  separate install extra?
- How much graph detail is useful before it becomes expensive to maintain?
- Should code intelligence be enabled by default, or only after explicit
  `make code-intel-index` setup?
