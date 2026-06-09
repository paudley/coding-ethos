<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics(R) Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Code Intelligence Roadmap

`coding-ethos` should grow from policy enforcement into a local code
intelligence substrate for agents. The target is not a generic RAG database.
The target is ETHOS-grounded code and remediation memory that helps agents find
the right code, understand prior failures, and choose the enforced repair path
before they run broad shell commands or repeat failed edits.

## Design Position

Use one repo-local storage substrate with separate logical responsibilities:

- **DuckDB is the canonical fact store.** It owns traces, policy decisions,
  remediations, outcomes, code symbols, AST graph edges, file metadata, and
  full-text search.
- **duckdb-vss is the active vector backend.** It keeps embeddings, metadata
  filters, and similarity search inside repo-local DuckDB files through the
  pure-Go `github.com/duckdb/duckdb-go/v2` integration, with no native sidecar, daemon,
  hosted service, or conditional build path.

Vector indexes are derived artifacts. They must be rebuildable from the
canonical DuckDB store and the checked-out repository contents.

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
      +--> DuckDB canonical store
      |      files
      |      symbols
      |      AST nodes
      |      graph edges
      |      decisions
      |      remediations
      |      attempts
      |      DuckDB term indexes
      |
      +--> duckdb-vss vector backend
             code/remediation vectors
             metadata-filtered similarity search
	     pure-Go github.com/duckdb/duckdb-go/v2
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

CEL inputs also expose code-intelligence fields under `source`, `finding`, and
edit/diff facts. `proposed_symbol_changes` compares current and proposed
Tree-sitter symbols for Edit/Write/MultiEdit actions, while `changed_symbols`
maps staged diff hunks to the affected Tree-sitter symbols. Both surfaces report
the file, language, node kind, symbol kind/name/path, line spans, content
hashes, action (`added`, `deleted`, or `modified`), and line-count delta. This
lets principle-owned CEL block growth of oversized functions, classes/types,
shell functions, and YAML config entries while still allowing refactors that
shrink large files.

Source-aware policy follows
[`AST_CEL_SARIF_ARCHITECTURE.md`](AST_CEL_SARIF_ARCHITECTURE.md): Go collects
Tree-sitter facts, CEL evaluates configurable predicates, and SARIF reports
stable AST-backed findings. Code-intel storage and MCP retrieval build on those
same facts; they must not become a second parsing or policy interpretation path.

The AST layer follows the resolver pattern proven in `~/Active/pyqa_lint`:
language detection, parser binding, parser reuse, tree traversal, and
line-to-nearest-context lookup live behind one Go entrypoint. The active
resolver supports Go, Python, JavaScript/TypeScript, shell, YAML, JSON, and
TOML. JSON and TOML are treated as first-class config-policy surfaces, so
agents can retrieve precise config entries instead of reading whole config
files. Markdown remains intentionally deferred until the project selects a
maintained Go binding or a first-class adapter for its parser layout.

Tree-sitter-backed policy diagnostics carry AST metadata into SARIF result
properties and partial fingerprints. Code scanning can therefore track the
symbol-level finding across unrelated line movement instead of treating every
nearby edit as a new whole-file violation.

Python policy enforcement now uses the same AST foundation for import and
functional-idiom rules. The `python.conditional_imports` evaluator blocks
write-time attempts to introduce nested imports, `TYPE_CHECKING` import
branches, module `__getattr__` shims, `__import__`, and
`importlib.import_module`. The `python.functional_idioms` evaluator records
assigned lambdas and closure factories so agents get principle-grounded advice
to use `functools`, `operator`, or `itertools` helpers instead of ad-hoc
closures.

The storage layer lives in `go/internal/codeintel`. It creates the canonical
`.coding-ethos/code-intel.duckdb` DuckDB store, ingests retained lint and hook
traces, stores normalized findings/remediations/remediation events, indexes
Tree-sitter chunks, records SARIF-to-AST links, and builds DuckDB search-term
tables over policy IDs, skill IDs, paths, messages, code chunks, and
remediation text.
duckdb-vss is active for derived vector rows, but DuckDB facts remain the
auditable source of truth.

Graph facts expose provenance classes wherever repo maps, graph reports, and
MCP graph surfaces show those facts. `EXTRACTED` marks parser/static-analysis
facts, while `GIT_DERIVED`, `POLICY_DERIVED`, `TRACE_DERIVED`, and
`DOC_DERIVED` mark deterministic derived evidence. `INFERRED` and `AMBIGUOUS`
are advisory only and must not become enforcement inputs without a deterministic
source fact behind them. Existing records without explicit provenance default to
`EXTRACTED` so migrated indexes stay conservative.

Code health scoring is another derived DuckDB surface. `code-intel health` and
MCP `code_intel_health` compute deterministic refactoring rankings from indexed
files/chunks, large files/functions, complex functions, complex conditionals,
exact structural clones, git hotspots, co-change coupling, ownership risk,
repeated lint/hook failures, and LCOV line coverage. Snapshots, targets,
evidence rows, and bounded trend history are persisted so every score is
explainable by biomarker and evidence ID. Refreshes persist repo-wide snapshots
and apply path filters only when reading the ranked targets, so targeted review
queries cannot replace the repository health baseline. Consumer repos can
reweight or disable biomarkers by glob under `code_intel.health` in
`repo_config.yaml` for generated, vendor, legacy, or test paths.

Session snapshots are exposed as `code-intel session-snapshot` and MCP
`code_intel_session_snapshot`. They derive the stable
`coding_ethos.session.v1` contract from existing hook traces, proxy
sessions/events/transforms, memory trace activity, and code-intel freshness
metadata. Provider-specific details stay nested under `provider.adapters`, so
agents can inspect current blockers and linked trace IDs without coupling to
provider-specific event files or doing broad source reads.

Automatic output pruning applies the configured `code_intel_db` row-retention
policy after high-volume code-intel writes. The default keeps 90 days of trace
and proxy-event rows, leaves current AST/code chunks to the index refresh path,
checkpoints the DuckDB store, and leaves DuckDB WAL files to checkpoint-managed
cleanup instead of retention deletion. Use explicit output-prune maintenance
with `--vacuum` when database compaction is needed.

Hook traces are also normalized into analytics tables. Each hook event stores
provider, tool, status, tracking ID, operation kind, target kind, risk category,
command and target-set fingerprints, runtime, rewrite state, and target paths.
Each decision stores policy/skill IDs, implementation, severity, principle IDs,
diagnostic counts, and message/suggestion variant hashes. This lets later
analysis answer which hook checks create the most friction, which advice text
reduces repeat violations, which operations are rewritten versus blocked, and
which targets are frequently involved without reparsing raw provider payloads.
Operator review rows can label a hook event as a correct block, false positive,
unclear message, over-broad policy, or missing allow-list case.

Agent Proxy foundation data uses the same store. Proxy events are
provider-neutral records for outbound provider calls, tool calls, file reads,
file listings, payload injections, payload truncations, cache hits, and edit
proposals. The session ledger stores request counts, file-read/listing counts,
edit counts, cache hits, injection/truncation/denial counts, payload hashes,
cache keys, trace/tracking IDs, direction, payload kind, DLP facts, policy
evidence, token usage, payload byte counts, and ordered transform records. This
is the storage substrate for future context-economy controls; it is not a
separate shadow database. The trust boundary and event contract are documented
in [AGENT_PROXY.md](AGENT_PROXY.md).

## Canonical DuckDB Store

The first implementation should create `.coding-ethos/code-intel.duckdb` with
tables for:

- repositories and worktrees
- indexed files with content hashes, parser metadata, index timestamps, and
  stale-result metadata
- AST chunks with stable chunk IDs, byte ranges, line ranges, language, symbol
  type, symbol name, parent symbol, and parent chunk
- graph edges for containment, imports, references, calls, inheritance, tests,
  and documentation links when language support allows
- AST-to-finding links that connect SARIF/CEL findings back to the exact
  indexed chunk where symbol identity is available
- hook traces, lint traces, SARIF result references, policy decisions, and
  remediation payloads
- hook usage analytics: event intent, target category, risk category,
  fingerprints, decision rows, message variants, runtime, rewrites, and target
  paths
- hook review metadata for false positives, unclear messages, over-broad
  policies, missing allow-list cases, and confirmed correct blocks
- proxy session/event metadata for provider calls, tool calls, file reads,
  listings, payload hashes, token counts, cache hits, injections, truncations,
  edits, and ordered transforms
- remediation attempts and outcomes keyed by stable remediation ID
- embedding metadata: model, provider, dimension, input kind, chunk hash, and
  vector backend row ID
- git-history signals: indexed HEAD freshness, per-file churn, ownership
  percentages, hotspot score, co-change partners, hidden coupling, and
  deterministic reviewer suggestions

Use the DuckDB term index for text, symbol, file path, policy, skill, command,
and advice search. Text search is not a fallback; it is part of hybrid
retrieval.

Initial command surface:

```bash
bin/coding-ethos-run code-intel ingest-traces
bin/coding-ethos-run code-intel stats
bin/coding-ethos-run code-intel hook-usage --risk-category bypass
bin/coding-ethos-run code-intel record-hook-review --trace-id hook-1 --disposition false_positive
bin/coding-ethos-run code-intel hook-reviews --disposition false_positive
bin/coding-ethos-run code-intel repeated-failures --policy-id python.unused_imports
bin/coding-ethos-run code-intel index-code pkg scripts config.yml
bin/coding-ethos-run code-intel git-signals --path pkg/app.go --paths pkg/app.go,pkg/store.go
bin/coding-ethos-run code-intel anatomy-map --path pkg --format toon
ls pkg | bin/coding-ethos-run code-intel enrich-listing --command 'ls pkg'
bin/coding-ethos-run code-intel code-chunks --path pkg/app.go --symbol-name BuildMessage
bin/coding-ethos-run code-intel repo-map --path pkg/app.go
bin/coding-ethos-run code-intel centrality --path pkg --format toon
bin/coding-ethos-run code-intel surprises --path pkg --format toon
bin/coding-ethos-run code-intel decisions add --title 'Use explicit startup' --rationale 'Startup should be inspectable.' --path pkg/app.go
bin/coding-ethos-run code-intel decisions list --path pkg/app.go --query startup
bin/coding-ethos-run code-intel decisions health --path pkg/app.go
bin/coding-ethos-run code-intel compact-context --path pkg/app.go
bin/coding-ethos-run code-intel ingest-sarif --file policy.sarif
bin/coding-ethos-run code-intel sarif-results --policy-id python.unused_imports
bin/coding-ethos-run code-intel proxy-file-read --session-id sess-1 --path pkg/app.go
bin/coding-ethos-run code-intel record-proxy-event --event-id evt-1 --session-id sess-1 --kind file_read --provider codex --target-path pkg/app.go
bin/coding-ethos-run code-intel proxy-sessions --provider codex
bin/coding-ethos-run code-intel proxy-events --session-id sess-1
bin/coding-ethos-run code-intel session-snapshot --session-id sess-1 --format toon
bin/coding-ethos-run code-intel remediation-outcomes --outcome repeated
bin/coding-ethos-run code-intel remediation-effectiveness --policy-id python.unused_imports
bin/coding-ethos-run code-intel embedding-candidates --record-kind remediation_outcome
bin/coding-ethos-run code-intel upsert-vector --id rem-1 --model-id voyage-code-3 --vector '0.1,0.2,0.3'
bin/coding-ethos-run code-intel embedding-records --backend duckdb-vss
bin/coding-ethos-run code-intel hybrid-search --text 'unused import' --model-id voyage-code-3 --vector '0.1,0.2,0.3'
bin/coding-ethos-run code-intel index-status --model-id voyage-code-3
bin/coding-ethos-run code-intel search --text 'unused import'
```

These commands read retained `.coding-ethos` traces and write only the
repo-local `.coding-ethos/code-intel.duckdb` store.

Workspace commands are the exception to repo-local state because they model a
parent directory or worktree family. They store only registry and derived
workspace facts under `.coding-ethos-workspace/` at the workspace root. Each
registered repository keeps its own `.coding-ethos/code-intel.duckdb` store;
workspace queries federate across those stores and annotate results with repo
aliases instead of merging databases. `scan`, `add`, `remove`, `list`,
`status`, and `refresh` manage the registry, stale HEAD/store metadata,
conservative git-history co-change candidates, and contract links derived from
existing AST graph edges.

`git-signals` records the HEAD commit and indexed timestamp before exposing
stored signals. A refresh is skipped when the current HEAD is already indexed;
`--force` rebuilds the bounded default window, and `--commits` raises or lowers
that window for large repositories. Reviewer suggestions are deterministic:
direct authorship percentage, co-change ownership, and recency relative to the
indexed history are reported as score inputs. Co-change rows also mark hidden
coupling when no direct static edge explains the pair.

`anatomy-map` is inspired by Aider's repo map, which gives agents a compact
symbol-level view before they spend context on full file reads. coding-ethos
keeps the same core idea but uses the repo-local Go AST/code-intel store rather
than Aider's prompt-time Python parser/cache and global PageRank ranking. The
first implementation is intentionally directory-local so proxy listing
enrichment can append concise file anatomy without replacing the original tool
output. The `directory-anatomy-map` proxy transform preserves the raw directory
listing and appends a compact TOON block with in-memory transform hashes and
token counts returned by the shared agent proxy pipeline. The live Bash
PostToolUse path uses the shared shell parser plus the conservative `ls`/`tree`
classifier, refreshes the listed source files, and emits the enriched listing
as context with `proxy.directory_anatomy` evidence. `ls` stays direct-child
only, while `tree` includes nested files and honors `tree -L N` as an anatomy
depth cap. `enrich-listing` accepts raw listing output from stdin or
`--listing-file` and can infer the target directory and recursive depth from a
static, single-target `ls` or `tree` command for replay and debugging.

`repo-map` and the startup `coding_ethos_repo_map` hook context provide the
global variant. The hook refreshes the repo-local AST index at `SessionStart`,
ranks indexed files by compact symbol/chunk signals, and emits the most useful
file/symbol signatures as TOON. The same renderer backs MCP
`code_intel_repo_map` and the read-only
`coding-ethos://code-intel/repo-map` resource so agents can request the current
map explicitly before broad exploration.

MCP workspace support adds `code_intel_workspace_status` plus an optional
`repo` argument on `code_intel_search`, `code_intel_answer`, and
`code_intel_repo_map`. Omitting `repo` preserves repo-local behavior.
`repo="<alias>"` opens that registered repo's independent code-intel store and
reports provenance in the result metadata. `repo="all"` is bounded to read-only
aggregate queries and returns per-repo provenance rather than a synthetic merged
repository.

`graph-report` composes the repo map, code-intel store counts, and the latest
stored health snapshot into a human, JSON, or TOON orientation report. It is
read-only with respect to indexing and tells agents when the AST index or health
snapshot needs an explicit refresh.

`graph-report` also includes deterministic topology communities derived from
existing file-level `code_edges` and `git_cochanges`. The v1 implementation uses
weighted connected components with stable ordering rather than Leiden or another
external graph dependency. Community IDs are advisory orientation hints exposed
in graph-report, repo-map, and context-card output; enforcement decisions must
not depend on community detection.

`centrality` and `surprises` expose deterministic graph-orientation views over
the same DuckDB facts. Central nodes rank files by explainable structural degree,
git co-change, health-priority, finding, and remediation-outcome signals.
Surprise edges highlight deterministic cross-boundary relationships such as
cross-directory, cross-language, policy/config-to-code, documentation-to-code,
test-to-production, and hidden git co-change links. These views are included in
`graph-report` and available as standalone `code-intel centrality` and
`code-intel surprises` commands. They carry provenance and explanation fields
and remain advisory: they help agents choose what to inspect before editing, but
they do not block or permit policy decisions.

Markdown documentation is also indexed into the same graph when it contains
explicit repo path references. Headings and code blocks remain ordinary Markdown
chunks; path references such as `go/internal/codeintel/store.go` create
`documents` edges, and `path#symbol` references create `mentions` edges.
Repo-root paths stay repo-relative; Markdown `./` and `../` references resolve
relative to the referencing Markdown file's directory before being stored.
Rationale-like headings such as "Rationale", "Decision", "Reasoning", or "Why"
classify explicit references as advisory `rationale_for` links. Documentation
links use `DOC_DERIVED` provenance for extracted references and `INFERRED`
provenance for rationale classification, and `graph-report` exposes them in a
`document_links` section. This is deterministic Markdown-only graph context: it
does not parse PDFs or images, does not call an LLM, and does not turn examples
or prose into policy decisions.

Decision intelligence stores explicit architectural rationale in the same
DuckDB index. The `decisions` CLI group records manual decisions, links them to
paths or symbols, indexes inline `WHY:`, `DECISION:`, and `TRADEOFF:` markers
found during code indexing, and reports stale, conflicting, overlapping, or
ungoverned decision areas through `decisions health`.

`proxy-file-read` is the current bridge for read deduplication. It reads a
repo-relative file, computes the current content hash, records the first read as
a `file_read` proxy event, and records later unchanged reads in the same session
as `cache_hit` events with a `file-read-cache` transform. Changed content is a
cache miss and returns the full file body again. The command gives transparent
proxy work a tested cache primitive without requiring provider/API interception
to exist first.

## Implemented Storage Foundation

The local store is the durable evidence ledger for CEL, SARIF, AST facts, hook
analytics, remediation advice, vector metadata, and remediation outcomes. The
target is a complete storage foundation, not a minimal placeholder.

DuckDB remains canonical and should gain:

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
- Embedding metadata records for duckdb-vss rows: backend,
  model ID, dimension, input kind, record kind, record ID, content hash,
  provider, policy ID, skill ID, path, and backend row ID.
- Search-term rows for SARIF results, remediation outcomes, and vector-ready
  source text so exact search works before embeddings exist.
- Embedding candidate rows for SARIF results, emitted remediation packets, and
  remediation outcomes so an approved embedding producer can write vectors back
  without reading raw trace JSON.
- Proxy session and event rows that summarize context access, transformations,
  payload hashes, cache behavior, token pressure, and edit attempts without
  requiring raw provider transcripts for routine analysis.

The first query/CLI surface should answer:

- which SARIF results are stored for a policy, skill, path, or trace;
- which remediation advice later led to fixed or repeated outcomes;
- which policies/skills produce repeated findings after advice was issued;
- which records are ready for embedding and which vector backend metadata rows
  already exist.
- which prior fixes are most relevant through hybrid term-index + duckdb-vss
  search, with fixed outcomes boosted and repeated/superseded outcomes
  downranked.
- which source files and symbols should be injected into compact agent context
  through `repo-map`, `code-chunks`, and `compact-context` without reparsing.
- which proxy sessions repeatedly read the same files, exceed token budgets,
  trigger payload truncation, or receive policy injections.

Vector work uses always-built duckdb-vss tables. Metadata stays in normal
DuckDB tables for auditability and filtering, while vectors live in
dimension-specific `vec0` virtual tables. This preserves the ETHOS one-path
build contract and keeps vector indexes rebuildable from DuckDB facts.

## Vector Backends

Define a narrow vector backend interface before binding to any implementation:

- `UpsertEmbedding(record)`
- `DeleteEmbedding(chunk_id, model_id)`
- `Search(query_embedding, filters, limit)`
- `Stats()`
- `Rebuild(collection)`

The initial backend priority:

1. duckdb-vss search for AST-aware code and remediation vectors.
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
DuckDB store with a content hash, line count, parser metadata, and index
timestamp. Each extracted symbol or configuration entry is written as a stable
`code_chunk`, mirrored into the term index, and exposed as an embedding
candidate with `record_kind=code_chunk`. Parent chunk IDs, containment edges,
import edges, and same-file reference edges are stored in DuckDB. SARIF/CEL results that
carry AST identity are linked back to matching chunks through
`ast_finding_links`. Markdown remains planned until the selected parser exposes
a maintained Go binding or the project adds a first-class adapter for its split
parser layout.

Code indexing always ignores repository metadata, runtime cache directories,
and nested `coding-ethos/` tool checkouts. Consumer-specific generated output
belongs in repo-local configuration rather than global code. For example, a
static-site repo can exclude generated output in `repo_config.yaml`:

```yaml
code_intel:
  exclude_paths:
    - "**/dist/**"
```

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

Search should combine exact filters, the term index, vectors, and reranking:

1. Apply hard filters: repo, path prefix, language, policy, skill, symbol kind,
   provider, or time range.
2. Retrieve candidates from the DuckDB term index.
3. Retrieve candidates from the vector backend.
4. Fuse ranks with reciprocal rank fusion.
5. Boost exact symbol/path/policy matches.
6. Return traceable results with file spans and policy/remediation context.

Vector-only retrieval should be available for diagnostics, but not the default
agent path.

## MCP Tool Surface

Add tools only after the store has a stable schema:

- `code_intel_search`: hybrid semantic/text search over stored SARIF,
  remediation, and AST chunk memory.
- `semantic_search`: code-focused hybrid search that returns exact indexed code
  chunks with path, symbol, raw text, and line metadata.
- `code_intel_overview`: task-shaped repository orientation with ranked files,
  freshness metadata, evidence counts, and follow-up MCP calls.
- `code_intel_answer`: cited retrieval packet for a repository question with
  `retrieval_quality` reported separately from answer `confidence`.
- `code_intel_repo_map`: return compact ranked files, symbols, and signatures
  from the repo-local AST index for session orientation.
- `code_intel_context_card`: compact file/symbol triage card combining chunks,
  local graph context, linked findings, freshness, and next MCP calls.
- `code_intel_change_risk`: modification-risk summary for target files using
  indexed chunks, repeated failure evidence, and recommended checks.
- `code_intel_why`: architectural decisions and decision-health signals for a
  query, path, symbol, or status before changing code.
- `code_intel_index_code`: refresh Tree-sitter code chunks for selected paths.
- `code_intel_code_chunks`: return focused symbol/config chunks by path,
  language, symbol kind, or symbol name.
- `code_intel_code_context`: expand a selected chunk into parent, children,
  graph edges, and linked SARIF/CEL findings.
- `code_intel_embedding_candidates`: return compact SARIF/remediation/code
  chunk records for an approved embedding producer.
- `remediation_history_search`: find prior remediations by policy, skill,
  command shape, file path, semantic similarity, and outcome.
- `code_intel_index_status`: report freshness, changed files, embedding model,
  backend, and failed indexing tasks.
- `code_intel_hook_usage`: summarize normalized hook usage by provider,
  operation, target, risk, status, policy, and skill.
- `code_intel_explain_result`: explain why a search result was returned,
  including term-index score, vector score, filters, and policy links.

All tools are advisory. They must not bypass hooks or edit files.

## Phases

### Phase 1 - Schema and Trace Ingestion

- [x] Create the DuckDB store.
- [x] Persist hook/lint trace summaries into normalized tables.
- [x] Persist hook usage analytics for allow/block/rewrite events with
  operation kind, target kind, risk category, tracking ID, runtime, command and
  target fingerprints, decision rows, and target paths.
- [x] Persist hook review metadata so operators can mark correct blocks, false
  positives, unclear messages, over-broad policies, and missing allow-list
  cases without changing raw traces.
- [x] Index `agent_remediation` payloads and remediation events.
- [x] Add a DuckDB term index over policies, skills, messages, advice,
  commands, files, and tool output summaries.
- [x] Persist SARIF result references into normalized tables.
- [x] Track remediation outcomes after follow-up attempts.
- [x] Store CEL/evaluator provenance beside findings and SARIF results.
- [x] Store vector-backend metadata records for duckdb-vss
  derived indexes.
- [x] Expose embedding candidates for SARIF/remediation records.

Acceptance criteria:

- [x] Existing `.coding-ethos` lint and hook traces can be imported.
- [x] Search can answer "show repeated failures for this policy/skill/file."
- [x] No vector backend is required for this phase.
- [x] Search can answer "show SARIF results for this policy/skill/file."
- [x] Search can answer "which remediation suggestions were fixed, repeated,
  or attempted again."
- [x] Search can answer "which hook operation/target/risk groups are blocked,
  rewritten, or repeatedly advised."
- [x] DuckDB can identify records that are ready for embedding and the
  repo-local duckdb-vss backend can search stored embeddings.

### Phase 2 - AST Indexing

- [x] Add Tree-sitter extraction for the first language set: Go, Python, YAML,
  shell, JavaScript/TypeScript.
- [x] Add JSON and TOML config-entry extraction to the AST resolver.
- [x] Store AST chunks, symbol metadata, byte ranges, line ranges, content
  hashes, and search text in DuckDB.
- [x] Expose AST chunks through the term index, embedding candidates, CLI, and
  MCP.
- [x] Expose line-to-nearest-symbol/config lookup through CLI and MCP code
  context.
- [x] Expose AST-backed proposed symbol changes to CEL edit preflight.
- [x] Store parser metadata, parent chunk IDs, graph edges, and AST finding
  links in DuckDB.
- [x] Expose focused code context with parent, children, graph edges, and
  linked findings through CLI and MCP.
- [x] Add Markdown support using Goldmark for robust documentation chunking.
- [x] Add incremental reindex by file hash and chunk-level invalidation.
- [x] Add MinHash LSH-based code similarity detection with normalized hashing,
  128-signature MinHash, 16-band LSH indexing, and CEL policy integration.

Acceptance criteria:

- [x] Editing one function reindexes only that file and re-embeds only changed
  chunks.
- [x] Search results include stable file/line spans and symbol identity.
- [x] Structurally similar code across files is detected at write time and
  reported as a CEL policy warning with SARIF relatedLocations.

### Phase 3 - duckdb-vss Vector Backend

- [x] Add the vector backend interface.
- [x] Implement duckdb-vss storage for remediation embeddings.
- [x] Record model ID, dimension, provider, and input kind for every vector.
- [x] Add rebuild and index-status reporting.
- [x] Keep the vector backend in the normal `make build` and `make check`
  path with no build tags, native artifacts, or daemon requirements.

Acceptance criteria:

- [x] Code and remediation vectors can be rebuilt from DuckDB plus repo
  contents.
- [x] Hybrid search can filter by path/language/policy before ranking.

### Phase 4 - Hybrid Retrieval Hardening

- Tune term-index + duckdb-vss ranking once AST chunks and remediation
  embeddings are populated.
- Add stale-index reporting for changed files and changed remediation records.
- Document capability and ranking differences from term-only search.

Acceptance criteria:

- Users can run code intelligence with the default duckdb-vss backend.
- Index status reports backend, model, stale row counts, and rebuild needs.

### Phase 5 - MCP Search Tools

- [x] Add code intelligence MCP tools for search, semantic search, index
  status, embedding candidates, code indexing, and chunk lookup.
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

## Code Similarity Detection

The code-intelligence store supports real-time structural clone detection
through MinHash LSH (Locality-Sensitive Hashing). This enables CEL policies to
warn agents about duplicate or near-duplicate code at write time.

### Storage Schema

Three fields in `code_chunks` support similarity:

- `normalized_hash` — SHA-256 of the token-normalized chunk content (identifiers
  replaced with `$ID`, string literals with `$STR`, numeric literals with
  `$NUM`). Exact equality finds Type-2 clones across files.
- `minhash_sig` — 128-value MinHash signature stored as a little-endian
  `[]uint64` blob. Used for Jaccard similarity estimation.

A separate `lsh_bands` table stores band hashes for sub-linear candidate
retrieval:

| Column | Purpose |
| --- | --- |
| `chunk_id` | FK to `code_chunks.chunk_id` |
| `band_index` | Band number (0–15) |
| `band_hash` | Hash of the 8-row band slice |
| `path` | File path (for filtering self-matches) |
| `symbol_name` | Symbol name (for diagnostics) |

### Detection Pipeline

1. AST indexing normalizes each chunk's tokens and computes `normalized_hash`
   and `minhash_sig`.
2. LSH bands are computed (16 bands × 8 rows per band) and stored in
   `lsh_bands`.
3. At policy evaluation time, `similarity_facts` queries:
   - Exact matches via `normalized_hash` equality (100% similarity).
   - LSH candidates via band hash collisions, refined with full Jaccard
     estimation. The default config uses ≥0.7 for candidates and ≥0.8 for
     policy activation.
4. Results are exposed to CEL as `similarity_facts` and reported through SARIF
   `relatedLocations`.
5. Agents can call MCP `code_similarity_check` with proposed code to inspect
   matches before writing a duplicate implementation.

### Reconciliation

LSH bands are transactionally reconciled with code chunks. When a file is
reindexed, bands for the old path are deleted before new chunks and bands are
inserted within the same transaction. This prevents stale band entries from
producing false-positive candidates.

### Configuration

The default `similarity` config uses 128 hash functions, 16 bands, and 8 rows
per band. This produces a candidate threshold of approximately 0.54 Jaccard
similarity (the S-curve inflection point), which means most pairs above ~54%
similarity will share at least one band hash. The runtime candidate threshold
defaults to 0.7, and the structural threshold defaults to 0.8.

```yaml
similarity:
  enabled: true
  minhash_size: 128
  shingle_size: 5
  lsh_bands: 16
  lsh_rows: 8
  min_symbol_lines: 5
  exact_normalized: true
  candidate_threshold: 0.7
  structural_threshold: 0.8
  max_matches: 10
```

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
- When does duckdb-vss search become too slow for repo-local remediation and
  AST retrieval?
- How much graph detail is useful before it becomes expensive to maintain?
- Should code intelligence be enabled by default, or only after explicit
  `make code-intel-index` setup?
