// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/policy"
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

func TestAnnotateWorkspaceSearchResultsAddsRepoMetadata(t *testing.T) {
	t.Parallel()

	repo := codeintel.WorkspaceRepo{Alias: "api", Path: "/workspace/api"}
	results := annotateWorkspaceSearchResults(repo, []codeintel.HybridSearchResult{
		{RecordID: "one"},
		{RecordID: "two", Metadata: map[string]string{"kind": "code"}},
	})

	if len(results) != 2 {
		t.Fatalf("annotated results = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Metadata["repo"] != "api" ||
			result.Metadata["repo_root"] != "/workspace/api" {
			t.Fatalf("metadata = %#v, want repo annotations", result.Metadata)
		}
	}
	if results[1].Metadata["kind"] != "code" {
		t.Fatalf("metadata = %#v, want existing metadata preserved", results[1].Metadata)
	}
}

func TestWorkspaceReposForScopeLoadsAllOrAlias(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	api := filepath.Join(root, "api")
	web := filepath.Join(root, "web")
	mkdirAll(t, api)
	mkdirAll(t, web)
	err := codeintel.SaveWorkspaceRegistry(root, codeintel.WorkspaceRegistry{
		Repos: []codeintel.WorkspaceRepo{
			{Alias: "api", Path: api},
			{Alias: "web", Path: web},
		},
	})
	if err != nil {
		t.Fatalf("save workspace registry: %v", err)
	}

	server := NewServerWithRuntime(policy.Bundle{}, Runtime{ConsumerRoot: root})

	repos, err := server.workspaceReposForScope("all")
	if err != nil {
		t.Fatalf("workspace repos for all: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("repos = %#v, want two repos", repos)
	}

	repos, err = server.workspaceReposForScope(" api ")
	if err != nil {
		t.Fatalf("workspace repos for alias: %v", err)
	}
	if len(repos) != 1 || repos[0].Alias != "api" {
		t.Fatalf("repos = %#v, want api only", repos)
	}

	if _, err := server.workspaceReposForScope("missing"); !errors.Is(
		err,
		errWorkspaceRepoAliasMissing,
	) {
		t.Fatalf("missing scope error = %v, want alias missing", err)
	}
}

func TestRenderWorkspaceHelpersTOON(t *testing.T) {
	t.Parallel()

	repoMaps := []map[string]any{
		{"repo": "api", "toon": "api_map:\n  files: 1\n"},
		{"repo": "ignored"},
	}
	renderedMaps := renderWorkspaceRepoMapsTOON(repoMaps)
	if !strings.Contains(renderedMaps, "repo: api\napi_map:") {
		t.Fatalf("rendered repo maps = %q, want api map", renderedMaps)
	}
	if strings.Contains(renderedMaps, "ignored") {
		t.Fatalf("rendered repo maps = %q, want invalid map omitted", renderedMaps)
	}

	status := codeintel.WorkspaceStatus{
		Root: "/workspace",
		Stats: codeintel.WorkspaceStatusStatistics{
			Repos:     1,
			Stale:     1,
			CoChanges: 2,
			Contracts: 3,
		},
		Repos: []codeintel.WorkspaceRepoStatus{
			{
				Alias:        "api",
				Path:         "/workspace/api",
				Stale:        true,
				StaleWarning: "needs refresh",
			},
		},
	}
	renderedStatus := renderWorkspaceStatusTOON(status)
	for _, want := range []string{
		"coding_ethos_workspace_status:",
		`root: "/workspace"`,
		"cochanges: 2",
		`alias: "api"`,
		`warning: "needs refresh"`,
	} {
		if !strings.Contains(renderedStatus, want) {
			t.Fatalf("rendered status missing %q:\n%s", want, renderedStatus)
		}
	}
}

func TestCodeIntelAnswerHelpers(t *testing.T) {
	t.Parallel()

	results := []codeintel.HybridSearchResult{
		{
			Kind:     "finding",
			RecordID: "one",
			Path:     "pkg/app.go",
			PolicyID: "policy",
			SkillID:  "skill",
			Message:  "search text",
		},
		{Kind: "chunk", RecordID: "two"},
	}
	citations := citationsFromHybridResults(results, 1)
	if len(citations) != 1 {
		t.Fatalf("citations = %#v, want one citation", citations)
	}
	if citations[0].RecordID != "one" || citations[0].SearchText != "search text" {
		t.Fatalf("citation = %#v, want first hybrid result", citations[0])
	}

	if got := retrievalQuality(0, codeIntelTaskMeta{Fresh: true}); got != "none" {
		t.Fatalf("retrievalQuality no citations = %q, want none", got)
	}
	if got := retrievalQuality(
		3,
		codeIntelTaskMeta{Fresh: true},
	); got != codeIntelQualityHigh {
		t.Fatalf("retrievalQuality fresh high = %q, want high", got)
	}
	if got := retrievalQuality(
		1,
		codeIntelTaskMeta{Fresh: true},
	); got != codeIntelQualityMedium {
		t.Fatalf("retrievalQuality fresh medium = %q, want medium", got)
	}
	if got := retrievalQuality(1, codeIntelTaskMeta{}); got != "partial" {
		t.Fatalf("retrievalQuality stale = %q, want partial", got)
	}

	if got := answerConfidence(codeIntelQualityHigh); got != codeIntelQualityMedium {
		t.Fatalf("answerConfidence high = %q, want medium", got)
	}
	if got := answerConfidence("partial"); got != codeIntelQualityLow {
		t.Fatalf("answerConfidence partial = %q, want low", got)
	}

	if !strings.Contains(answerSummaryForRetrieval("none"), "No indexed evidence") {
		t.Fatal("answerSummaryForRetrieval none did not explain missing evidence")
	}
	if !strings.Contains(
		answerSummaryForRetrieval(codeIntelQualityHigh),
		"Relevant indexed evidence",
	) {
		t.Fatal("answerSummaryForRetrieval high did not explain available evidence")
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

func mkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
