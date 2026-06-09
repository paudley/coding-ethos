// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"path/filepath"
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
