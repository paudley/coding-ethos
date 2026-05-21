// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import "testing"

func TestApplySearchReplacePatchAppliesSequentialExactMatches(t *testing.T) {
	t.Parallel()

	result := ApplySearchReplacePatch(SearchReplacePatchRequest{
		Path:           "pkg/app.py",
		CurrentContent: "alpha\nbeta\ngamma\n",
		Blocks: []SearchReplaceBlock{
			{Search: "beta\n", Replace: "delta\n"},
			{Search: "delta\n", Replace: "epsilon\n"},
		},
	})

	if result.AppliedContent != "alpha\nepsilon\ngamma\n" {
		t.Fatalf("applied content = %q", result.AppliedContent)
	}
	if len(result.Blocks) != 2 ||
		result.Blocks[0].Status != SearchReplaceStatusOK ||
		result.Blocks[1].Status != SearchReplaceStatusOK {
		t.Fatalf("block results = %#v", result.Blocks)
	}
	if result.CurrentContentHash == "" || result.AppliedContentHash == "" ||
		result.CurrentContentHash == result.AppliedContentHash {
		t.Fatalf("content hashes were not recorded correctly: %#v", result)
	}
}

func TestApplySearchReplacePatchRejectsMissingSearchBlock(t *testing.T) {
	t.Parallel()

	result := ApplySearchReplacePatch(SearchReplacePatchRequest{
		CurrentContent: "alpha\n",
		Blocks: []SearchReplaceBlock{
			{Search: "missing\n", Replace: "replacement\n"},
		},
	})

	if len(result.Blocks) != 1 ||
		result.Blocks[0].Status != SearchReplaceStatusMissing ||
		result.Blocks[0].MatchCount != 0 {
		t.Fatalf("block results = %#v", result.Blocks)
	}
	if result.AppliedContent != "alpha\n" {
		t.Fatalf("content changed after missing search: %q", result.AppliedContent)
	}
}

func TestApplySearchReplacePatchRejectsAmbiguousSearchBlock(t *testing.T) {
	t.Parallel()

	result := ApplySearchReplacePatch(SearchReplacePatchRequest{
		CurrentContent: "alpha\nalpha\n",
		Blocks: []SearchReplaceBlock{
			{Search: "alpha\n", Replace: "replacement\n"},
		},
	})

	if len(result.Blocks) != 1 ||
		result.Blocks[0].Status != SearchReplaceStatusAmbiguous ||
		result.Blocks[0].MatchCount != 2 {
		t.Fatalf("block results = %#v", result.Blocks)
	}
	if result.AppliedContent != "alpha\nalpha\n" {
		t.Fatalf("content changed after ambiguous search: %q", result.AppliedContent)
	}
}

func TestApplySearchReplacePatchRejectsEmptySearchBlock(t *testing.T) {
	t.Parallel()

	result := ApplySearchReplacePatch(SearchReplacePatchRequest{
		CurrentContent: "alpha\n",
		Blocks: []SearchReplaceBlock{
			{Search: "", Replace: "replacement\n"},
		},
	})

	if len(result.Blocks) != 1 ||
		result.Blocks[0].Status != SearchReplaceStatusEmpty {
		t.Fatalf("block results = %#v", result.Blocks)
	}
	if result.AppliedContent != "alpha\n" {
		t.Fatalf("content changed after empty search: %q", result.AppliedContent)
	}
}
