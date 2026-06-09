// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestCapWorkspaceSearchResultsRespectsLimit(t *testing.T) {
	t.Parallel()

	results := []codeintel.HybridSearchResult{
		{RecordID: "one"},
		{RecordID: "two"},
		{RecordID: "three"},
	}

	capped := capWorkspaceSearchResults(results, 2)

	if len(capped) != 2 {
		t.Fatalf("capped results = %d, want 2", len(capped))
	}
	if capped[0].RecordID != "one" || capped[1].RecordID != "two" {
		t.Fatalf("capped results = %#v, want first two results", capped)
	}
}

func TestWorkspaceSearchResultsRankBeforeCap(t *testing.T) {
	t.Parallel()

	results := []codeintel.HybridSearchResult{
		{RecordID: "repo-a-low", Score: 1},
		{RecordID: "repo-a-mid", Score: 3},
		{RecordID: "repo-b-high", Score: 10},
	}

	rankHybridSearchResults(results)
	capped := capWorkspaceSearchResults(results, 2)

	if len(capped) != 2 {
		t.Fatalf("capped results = %d, want 2", len(capped))
	}
	if capped[0].RecordID != "repo-b-high" || capped[1].RecordID != "repo-a-mid" {
		t.Fatalf("capped results = %#v, want highest-scoring workspace hits", capped)
	}
}

func TestCodeIntelRootlessPathsUseCodeIntelError(t *testing.T) {
	t.Parallel()

	server := Server{}

	_, _, err := server.openCodeIntelStoreForRoot(" ")
	if !errors.Is(err, errCodeIntelRootUnavailable) {
		t.Fatalf("openCodeIntelStoreForRoot error = %v, want code-intel root error", err)
	}

	_, err = server.codeIntelIndexCode([]byte(`{"paths":["README.md"]}`))
	if !errors.Is(err, errCodeIntelRootUnavailable) {
		t.Fatalf("codeIntelIndexCode error = %v, want code-intel root error", err)
	}
}
