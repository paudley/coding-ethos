// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestStoreRecordsAndQueriesDecisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	record, err := store.RecordDecision(ctx, DecisionRecord{
		Title:     "Use local caches",
		Rationale: "Local caches keep managed-tool startup deterministic.",
		Status:    DecisionStatusAccepted,
		Links: []DecisionLink{{
			Path: "pkg/cache.go",
			Kind: DecisionLinkAffects,
		}},
	})
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}

	records, err := store.Decisions(ctx, DecisionQuery{
		Text:  "deterministic",
		Path:  "pkg/cache.go",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("decisions = %#v, want %q", records, record.ID)
	}
	if len(records[0].Links) != 1 || records[0].Links[0].Path != "pkg/cache.go" {
		t.Fatalf("decision links = %#v", records[0].Links)
	}
}

func TestLinkDecisionPreservesExistingLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	record, err := store.RecordDecision(ctx, DecisionRecord{
		Title:     "Use durable queues",
		Rationale: "Durable queues preserve ingestion work.",
		Status:    DecisionStatusAccepted,
		Links: []DecisionLink{{
			Path: "pkg/queue.go",
			Kind: DecisionLinkAffects,
		}},
	})
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}

	err = store.LinkDecision(ctx, record.ID, []DecisionLink{{
		Path: "pkg/worker.go",
		Kind: DecisionLinkAffects,
	}})
	if err != nil {
		t.Fatalf("link decision: %v", err)
	}

	records, err := store.Decisions(ctx, DecisionQuery{Path: "pkg/queue.go", Limit: 5})
	if err != nil {
		t.Fatalf("query original link: %v", err)
	}
	if len(records) != 1 || len(records[0].Links) != 2 {
		t.Fatalf("original path decision links = %#v", records)
	}

	records, err = store.Decisions(ctx, DecisionQuery{Path: "pkg/worker.go", Limit: 5})
	if err != nil {
		t.Fatalf("query added link: %v", err)
	}
	if len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("added path decisions = %#v, want %q", records, record.ID)
	}
}

func TestIndexedDecisionsSkipFencedExamples(t *testing.T) {
	t.Parallel()

	records := IndexedDecisions("README.md", []byte(`
WHY: real architectural note.

`+"```"+`
WHY: example marker only.
`+"```"+`
`))

	if len(records) != 1 {
		t.Fatalf("indexed decisions = %#v, want one real marker", records)
	}
	if records[0].SourceLine != 2 || records[0].Rationale != "real architectural note." {
		t.Fatalf("decision = %#v", records[0])
	}
}

func TestIndexedDecisionsUseFirstSentenceSeparator(t *testing.T) {
	t.Parallel()

	records := IndexedDecisions(
		"pkg/app.go",
		[]byte("// WHY: Prefer explicit startup; it fails before work. Later sentence.\n"),
	)

	if len(records) != 1 {
		t.Fatalf("indexed decisions = %#v, want one marker", records)
	}
	if records[0].Title != "WHY: Prefer explicit startup" {
		t.Fatalf("decision title = %q", records[0].Title)
	}
}

func TestParseDecisionDocumentRequiresExplicitOptIn(t *testing.T) {
	t.Parallel()

	_, found, err := ParseDecisionDocument("README.md", []byte(`---
title: Looks like a decision
---
# Decision

This is documentation prose, not an architectural decision record.
`))
	if err != nil {
		t.Fatalf("parse non-decision document: %v", err)
	}
	if found {
		t.Fatalf("generic decision-like document should not be imported")
	}
}

func TestParseDecisionDocumentReadsFrontMatterLinks(t *testing.T) {
	t.Parallel()

	record, found, err := ParseDecisionDocument("docs/decisions/startup.md", []byte(`---
coding_ethos_decision: true
title: Use explicit startup flow
status: accepted
rationale: Startup should fail before background work begins.
alternatives: Lazy validation hides configuration drift.
author: platform
recorded_at_utc: 2026-01-02T03:04:05Z
affected_paths:
  - pkg/app.go
affected_symbols:
  - path: pkg/app.go
    symbol_path: Run
---
# Use explicit startup flow
`))
	if err != nil {
		t.Fatalf("parse decision document: %v", err)
	}
	if !found {
		t.Fatalf("decision document was not imported")
	}
	if record.SourceKind != DecisionSourceDocument ||
		record.SourcePath != "docs/decisions/startup.md" ||
		record.ProvenanceClass != ProvenanceDocDerived {
		t.Fatalf("record provenance = %#v", record)
	}
	if len(record.Links) != 2 ||
		record.Links[0].Path != "pkg/app.go" ||
		record.Links[1].SymbolPath != "Run" {
		t.Fatalf("record links = %#v", record.Links)
	}
}

func TestImportDecisionRecordsScansDefaultCodingEthosDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	payload := []byte(`---
tags: [architecture-decision]
title: Keep startup explicit
rationale: Explicit startup exposes missing dependencies before work starts.
affected_paths:
  - pkg/app.go
---
`)
	_, found, err := ParseDecisionDocument(".coding-ethos/decisions/startup.md", payload)
	if err != nil {
		t.Fatalf("parse decision fixture: %v", err)
	}
	if !found {
		t.Fatalf("decision fixture should parse before import")
	}

	writeFile(t, filepath.Join(root, ".coding-ethos", "decisions", "startup.md"), payload)

	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	summary, err := store.ImportDecisionRecords(ctx, root, nil)
	if err != nil {
		t.Fatalf("import decision records: %v", err)
	}
	if summary.DecisionsImported != 1 {
		t.Fatalf("import summary = %#v, want 1 imported decision", summary)
	}

	records, err := store.Decisions(ctx, DecisionQuery{Path: "pkg/app.go", Limit: 5})
	if err != nil {
		t.Fatalf("query imported decisions: %v", err)
	}
	if len(records) != 1 || records[0].Title != "Keep startup explicit" {
		t.Fatalf("imported records = %#v", records)
	}
}

func TestASTIndexerReplacesIndexedDecisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.go")
	writeFile(t, sourcePath, []byte(`package pkg

func Run() {
	// WHY: Run keeps startup explicit.
}
`))

	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	indexer := NewASTIndexer(store)
	if _, err := indexer.IndexPaths(ctx, root, []string{"pkg/app.go"}); err != nil {
		t.Fatalf("index first source: %v", err)
	}

	records, err := store.Decisions(ctx, DecisionQuery{Path: "pkg/app.go", Limit: 5})
	if err != nil {
		t.Fatalf("query indexed decisions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("indexed decisions = %#v, want one", records)
	}

	writeFile(t, sourcePath, []byte(`package pkg

func Run() {}
`))
	if _, err = indexer.IndexPaths(ctx, root, []string{"pkg/app.go"}); err != nil {
		t.Fatalf("index updated source: %v", err)
	}

	records, err = store.Decisions(ctx, DecisionQuery{Path: "pkg/app.go", Limit: 5})
	if err != nil {
		t.Fatalf("query replaced decisions: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("indexed decisions after replacement = %#v, want none", records)
	}
}

func TestASTIndexerRefreshesIndexedDecisionsForTooManyLines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "large.go")
	writeFile(t, sourcePath, []byte(`package pkg

func Run() {
	// WHY: Run keeps startup explicit.
}
`))

	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	indexer := NewASTIndexer(store)
	if _, err := indexer.IndexPaths(ctx, root, []string{"pkg/large.go"}); err != nil {
		t.Fatalf("index first source: %v", err)
	}

	var large strings.Builder
	large.WriteString("package pkg\n")
	large.WriteString("// WHY: Large file keeps generated declarations together.\n")
	for index := 0; index < 5001; index++ {
		large.WriteString("// generated line\n")
	}
	writeFile(t, sourcePath, []byte(large.String()))

	if _, err := indexer.IndexPaths(ctx, root, []string{"pkg/large.go"}); err != nil {
		t.Fatalf("index oversized source: %v", err)
	}

	records, err := store.Decisions(ctx, DecisionQuery{Path: "pkg/large.go", Limit: 5})
	if err != nil {
		t.Fatalf("query refreshed decisions: %v", err)
	}
	if len(records) != 1 ||
		!strings.Contains(records[0].Rationale, "generated declarations") {
		t.Fatalf("indexed decisions after too-many-lines refresh = %#v", records)
	}
}

func TestDecisionHealthNormalizesPathFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	err := store.ReplaceCodeFileIndex(ctx, CodeFile{
		Path:        "pkg/app.go",
		Language:    "Go",
		ContentHash: "hash-a",
		LineCount:   10,
	}, nil, nil)
	if err != nil {
		t.Fatalf("seed code file: %v", err)
	}

	health, err := store.DecisionHealth(ctx, DecisionQuery{Path: ".", Limit: 5})
	if err != nil {
		t.Fatalf("query decision health: %v", err)
	}
	if health.Summary.UngovernedCount != 1 {
		t.Fatalf("ungoverned count = %d, want 1", health.Summary.UngovernedCount)
	}
}

func TestDecisionHealthDoesNotOverlapDuplicateDecisionPathLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	_, err := store.RecordDecision(ctx, DecisionRecord{
		Title:     "Use explicit handlers",
		Rationale: "Explicit handlers keep routing inspectable.",
		Status:    DecisionStatusAccepted,
		Links: []DecisionLink{
			{
				Path:       "pkg/app.go",
				SymbolPath: "Run",
				Kind:       DecisionLinkAffects,
			},
			{
				Path:       "pkg/app.go",
				SymbolPath: "Stop",
				Kind:       DecisionLinkAffects,
			},
		},
	})
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}

	health, err := store.DecisionHealth(ctx, DecisionQuery{Path: "pkg/app.go", Limit: 5})
	if err != nil {
		t.Fatalf("query decision health: %v", err)
	}
	if health.Summary.OverlapCount != 0 {
		t.Fatalf(
			"overlap count = %d, want 0: %#v",
			health.Summary.OverlapCount,
			health.Overlapping,
		)
	}
}
