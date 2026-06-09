// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
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
