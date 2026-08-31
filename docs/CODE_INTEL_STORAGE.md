<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Code-Intel Storage

## Decision

`coding-ethos` uses immutable content-addressed files for exact source facts,
append-only event files for durable telemetry, and DuckDB for derived local
analytics.

- `<git-common-dir>/coding-ethos/code-intel/v2/` contains immutable shared
  fragments and HEAD base manifests.
- `<state-root>/.coding-ethos/code-intel-v2/lanes/<worktree-id>/` contains one
  worktree's immutable delta manifests, tombstones, and current receipt.

- `.coding-ethos/events/*.jsonl` is the durable live telemetry surface.
- `.coding-ethos/code-intel.duckdb` is the local analytical query index.

Manifest repository identity is independent of these storage paths: it hashes
the normalized origin URL and repository root-commit set. Repositories without
an origin use a documented path-local common-directory fallback; shallow
repositories cannot produce an exact identity.

Hook, lint, proxy, SARIF, remediation, and embedding entrypoints write a
structured event before they update the DuckDB query index. The DuckDB index is
rebuildable from event files and retained trace logs. Runtime paths do not use
DuckDB as a shared multi-process write target.

## Rationale

Downstream analysis is analytical and evidence-heavy: blocked policies, affected
commands, large-file pressure, remediation loops, toolchain health, and
log/index discrepancies. A single live database write path makes missing
ingestion and lock evidence difficult to interpret.

DuckDB is a better local query engine for support analysis, but it is not the
right live multi-process hook write path. Append-only JSONL files keep hook
telemetry durable and low-contention; DuckDB gives downstream-analysis a
rebuildable analytical surface.

## Operational Contract

- `code-intel sync` creates or reuses an exact v2 source generation without
  deleting or rewriting the v1 store.
- Normal hook, lint, commit, and push paths never rebuild or refresh the whole
  repository. They append bounded event evidence and incrementally index only
  files already selected by managed capture. Whole-repository work requires an
  explicit code-intel maintenance command.
- Eligible RDF or SPARQL requires an identity-validated pinned PurRDF helper;
  missing or mismatched helpers fail sync instead of degrading silently.
- `code-intel status` compares the recorded source snapshot to the current
  worktree and reports exact, partial, failed, missing, or stale coverage.
- `code-intel rebuild-derived` creates or refreshes `code-intel.duckdb` and
  removes obsolete pre-DuckDB store artifacts without importing them.
- `code-intel rebuild-index` is a deprecated alias for `rebuild-derived`.
- `code-intel downstream-analysis` reads DuckDB and retained logs. Its default
  `json` output is stable for automation; `--format toon` provides the compact
  operator handoff, and `--format human` provides a short readable summary.
- `coding-ethos-run status` combines runtime artifact readiness,
  output-surface inventory, code-intel DuckDB stats, recent hook failures,
  hook-review and false-positive counts, and recommended next actions into a
  compact operator handoff. `--write status.md` writes the human handoff without
  changing derived stores.
- SessionStart performs the same obsolete-artifact cleanup before agents rely on
  startup code-intel context.
- No external database, daemon, service, or network dependency is required.
- A missing or stale DuckDB index is repairable by rebuilding; it is not source
  evidence loss.

## Report Contract

Downstream reports expose storage and support fields directly:

- `storage_health`
- `affected_commands`
- `remediation_loops`
- `large_file_pressure`
- `toolchain_health`
- `evidence_gaps`
- `issue_summary`

These fields are intended for issue-ready support summaries without manual DB or
log spelunking. Compact downstream output ranks blocking friction ahead of
allowed activity and includes command-ready repair guidance such as
`coding-ethos-run code-intel rebuild-derived --root <repo>`.
