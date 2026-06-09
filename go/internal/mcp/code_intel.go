// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

const (
	codeIntelDefaultTaskLimit  = 5
	codeIntelAuthorRiskCount   = 3
	codeIntelHotspotThreshold  = 25
	codeIntelMaxTaskLimit      = 25
	codeIntelMediumRiskReason  = 1
	codeIntelQualityHigh       = "high"
	codeIntelQualityMedium     = "medium"
	codeIntelQualityLow        = "low"
	contextCardCommunityLimit  = 200
	codeIntelReviewerLimit     = 3
	contextCardTOONHeaderLines = 3
	semanticSearchContextLimit = 5
)

type codeIntelTaskMeta struct {
	RepoHeadCommit string   `json:"repo_head_commit,omitempty"`
	IndexAge       string   `json:"index_age,omitempty"`
	IndexedAtUTC   string   `json:"indexed_at_utc,omitempty"`
	StaleWarning   string   `json:"stale_warning,omitempty"`
	Compression    string   `json:"compression"`
	DataSources    []string `json:"data_sources"`
	ReadyRecords   int      `json:"ready_records"`
	MissingVectors int      `json:"missing_vectors"`
	IndexedFiles   int      `json:"indexed_files"`
	IndexedChunks  int      `json:"indexed_chunks"`
	SchemaVersion  int      `json:"schema_version"`
	Fresh          bool     `json:"fresh"`
	Truncated      bool     `json:"truncated"`
}

type codeIntelCitation struct {
	Kind       string `json:"kind"`
	RecordID   string `json:"record_id"`
	Path       string `json:"path,omitempty"`
	PolicyID   string `json:"policy_id,omitempty"`
	SkillID    string `json:"skill_id,omitempty"`
	SearchText string `json:"search_text,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
}

type codeIntelContextTarget struct {
	Context     *codeintel.CodeContext `json:"context,omitempty"`
	Path        string                 `json:"path"`
	CommunityID string                 `json:"community_id,omitempty"`
	Chunks      []codeintel.CodeChunk  `json:"chunks,omitempty"`
	File        codeintel.CodeFile     `json:"file,omitzero"`
	Found       bool                   `json:"found"`
	IndexFresh  bool                   `json:"index_fresh"`
}

type gitReviewerSuggestions []codeintel.GitReviewerSuggestion

type codeIntelRiskTarget struct {
	File               *codeintel.CodeFile          `json:"file,omitempty"`
	Path               string                       `json:"path"`
	RiskLevel          string                       `json:"risk_level"`
	GitSignalFreshness *codeintel.GitSignalSummary  `json:"git_signal_freshness"`
	Chunks             []codeintel.CodeChunk        `json:"chunks,omitempty"`
	GitSignals         []codeintel.GitFileSignal    `json:"git_signals,omitempty"`
	Health             []codeintel.CodeHealthTarget `json:"health"`
	Reviewers          gitReviewerSuggestions       `json:"reviewers,omitempty"`
	RepeatedFailures   []codeintel.RepeatedFailure  `json:"repeated_failures,omitempty"`
	Reasons            []string                     `json:"reasons,omitempty"`
	RecommendedChecks  []map[string]string          `json:"recommended_checks,omitempty"`
}

func (server Server) codeIntelOverview(args json.RawMessage) (any, error) {
	var input codeIntelRepoMapInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse code intelligence overview arguments: %w", inlineErr0)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	root := server.codeIntelRoot()

	repoMap, rendered, err := server.readRepoMap(store, root, input)
	if err != nil {
		return nil, err
	}

	meta, err := server.codeIntelTaskMeta(argsContext(), store, index, []string{
		"repo_map",
		"code_files",
		"code_chunks",
		"embeddings",
	})
	if err != nil {
		return nil, fmt.Errorf("read code intelligence task metadata: %w", err)
	}

	nextCalls := []map[string]any{
		{
			"tool":      "code_intel_context_card",
			"arguments": map[string]any{"path": "<path>"},
		},
		{
			"tool":      "code_intel_answer",
			"arguments": map[string]any{"question": "<repo question>"},
		},
		{
			"tool": "code_intel_change_risk",
			"arguments": map[string]any{
				"paths": []string{"<path>"},
			},
		},
	}

	result := map[string]any{
		"kind":           "code_intel_overview",
		"_meta":          meta,
		"repo_map":       repoMap,
		"toon":           rendered,
		"next_mcp_calls": nextCalls,
		"orientation":    "Use ranked files and symbols as the bounded starting point.",
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = rendered
	}

	return result, nil
}

func (server Server) codeIntelSearch(args json.RawMessage) (any, error) {
	var input codeIntelSearchInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence search arguments: %w",
			inlineErr0,
		)
	}

	text := firstNonEmpty(input.Text, input.Query)
	if strings.TrimSpace(text) == "" && len(input.Vector) == 0 {
		return nil, apperror.StaticError("text/query or vector is required")
	}

	if strings.TrimSpace(input.Repo) != "" {
		return server.codeIntelWorkspaceSearch(input, text)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	results, err := store.HybridSearch(
		argsContext(),
		index,
		codeintel.HybridSearchQuery{
			Filters:    input.Filters,
			Text:       text,
			RecordKind: input.RecordKind,
			Collection: firstNonEmpty(input.Collection, "remediations"),
			ModelID:    input.ModelID,
			PolicyID:   input.PolicyID,
			SkillID:    input.SkillID,
			Path:       input.Path,
			Vector:     input.Vector,
			Limit:      input.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("run code intelligence search: %w", err)
	}

	return map[string]any{
		"kind":    "code_intel_search",
		"backend": codeintel.VectorBackendDuckDBVSS,
		"results": results,
	}, nil
}

func (server Server) codeIntelAnswer(args json.RawMessage) (any, error) {
	var input codeIntelAnswerInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse code intelligence answer arguments: %w", inlineErr0)
	}

	question := strings.TrimSpace(firstNonEmpty(input.Question, input.Query))
	if question == "" {
		return nil, apperror.StaticError("question/query is required")
	}

	if strings.TrimSpace(input.Repo) != "" {
		return server.codeIntelWorkspaceAnswer(input, question)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)
	paths := codeIntelInputPaths(input.Path, input.Paths)

	results, err := codeIntelAnswerResults(ctx, store, index, question, paths, limit)
	if err != nil {
		return nil, err
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"hybrid_search",
		"code_chunks",
		"remediation_evidence",
	})
	if err != nil {
		return nil, fmt.Errorf("read code intelligence task metadata: %w", err)
	}

	citations := citationsFromHybridResults(results, limit)
	quality := retrievalQuality(len(citations), meta)

	return map[string]any{
		"kind":              "code_intel_answer",
		"_meta":             meta,
		"question":          question,
		"answer":            answerSummaryForRetrieval(quality),
		"retrieval_quality": quality,
		"confidence":        answerConfidence(quality),
		"citations":         citations,
		"results":           results,
		"next_actions": []string{
			"Inspect cited records before editing.",
			"Call code_intel_context_card for the highest-ranked cited path.",
		},
	}, nil
}

func codeIntelAnswerResults(
	ctx context.Context,
	store *codeintel.Store,
	index evidence.VectorIndex,
	question string,
	paths []string,
	limit int,
) ([]codeintel.HybridSearchResult, error) {
	if len(paths) == 0 {
		results, err := store.HybridSearch(ctx, index, codeintel.HybridSearchQuery{
			Text:       codeIntelFTSQuery(question),
			Collection: "code_chunks",
			Limit:      limit,
		})
		if err != nil {
			return nil, fmt.Errorf("answer code intelligence question: %w", err)
		}

		return results, nil
	}

	results := []codeintel.HybridSearchResult{}

	for _, path := range paths {
		pathResults, err := store.HybridSearch(
			ctx,
			index,
			codeintel.HybridSearchQuery{
				Text:       codeIntelFTSQuery(question),
				Collection: "code_chunks",
				Path:       path,
				Limit:      limit,
			},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"answer code intelligence question for %s: %w",
				path,
				err,
			)
		}

		results = append(results, pathResults...)
	}

	slices.SortFunc(results, func(left, right codeintel.HybridSearchResult) int {
		if left.Score > right.Score {
			return -1
		}

		if left.Score < right.Score {
			return 1
		}

		return strings.Compare(left.RecordID, right.RecordID)
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

type semanticSearchResult struct {
	Match codeintel.HybridSearchResult `json:"match"`
	Chunk codeintel.CodeChunk          `json:"chunk"`
}

func (server Server) semanticSearch(args json.RawMessage) (any, error) {
	var input codeIntelSearchInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse semantic search arguments: %w",
			inlineErr0,
		)
	}

	text := firstNonEmpty(input.Query, input.Text)
	if strings.TrimSpace(text) == "" && len(input.Vector) == 0 {
		return nil, apperror.StaticError("query/text or vector is required")
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()

	matches, err := store.HybridSearch(
		ctx,
		index,
		codeintel.HybridSearchQuery{
			Filters:    input.Filters,
			Text:       text,
			RecordKind: "code_chunk",
			Collection: firstNonEmpty(input.Collection, "code_chunks"),
			ModelID:    input.ModelID,
			Path:       input.Path,
			Vector:     input.Vector,
			Limit:      input.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("run semantic search: %w", err)
	}

	results := make([]semanticSearchResult, 0, len(matches))
	for _, match := range matches {
		context, contextErr := store.CodeContext(ctx, codeintel.CodeContextQuery{
			ChunkID: match.RecordID,
			Root:    server.codeIntelRoot(),
			Limit:   semanticSearchContextLimit,
		})
		if contextErr != nil {
			return nil, fmt.Errorf(
				"load semantic search code chunk %q: %w",
				match.RecordID,
				contextErr,
			)
		}

		results = append(results, semanticSearchResult{
			Match: match,
			Chunk: context.Chunk,
		})
	}

	return map[string]any{
		"kind":    "semantic_search",
		"backend": codeintel.VectorBackendDuckDBVSS,
		"query":   text,
		"results": results,
	}, nil
}

func (server Server) codeIntelIndexStatus(args json.RawMessage) (any, error) {
	var input codeIntelIndexStatusInput

	inlineErr1 := json.Unmarshal(args, &input)
	if inlineErr1 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence index-status arguments: %w",
			inlineErr1,
		)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()

	vectorStats, err := index.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("read vector index stats: %w", err)
	}

	status, err := store.IndexStatus(ctx, vectorStats, codeintel.EmbeddingRecordQuery{
		Backend:    codeintel.VectorBackendDuckDBVSS,
		Collection: firstNonEmpty(input.Collection, "remediations"),
		ModelID:    input.ModelID,
	})
	if err != nil {
		return nil, fmt.Errorf("read code intelligence index status: %w", err)
	}

	return status, nil
}

func (server Server) codeIntelHookUsage(args json.RawMessage) (any, error) {
	var input codeIntelHookUsageInput

	inlineErr2 := json.Unmarshal(args, &input)
	if inlineErr2 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence hook-usage arguments: %w",
			inlineErr2,
		)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	results, err := store.HookUsage(argsContext(), codeintel.HookUsageQuery{
		Provider:      input.Provider,
		Status:        input.Status,
		PolicyID:      input.PolicyID,
		SkillID:       input.SkillID,
		OperationKind: input.OperationKind,
		TargetKind:    input.TargetKind,
		RiskCategory:  input.RiskCategory,
		Limit:         input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query hook usage: %w", err)
	}

	return map[string]any{
		"kind":    "code_intel_hook_usage",
		"results": results,
	}, nil
}

func (server Server) codeIntelIndexCode(args json.RawMessage) (any, error) {
	var input codeIntelIndexCodeInput

	inlineErr3 := json.Unmarshal(args, &input)
	if inlineErr3 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence index-code arguments: %w",
			inlineErr3,
		)
	}

	root := server.codeIntelRoot()
	if strings.TrimSpace(root) == "" {
		return nil, errCodeIntelRootUnavailable
	}

	ctx := argsContext()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer autoPruneCodeIntelDB(root)
	defer store.Close()

	summary, err := codeintel.NewASTIndexer(store).IndexPaths(ctx, root, input.Paths)
	if err != nil {
		return nil, fmt.Errorf("index code intelligence paths: %w", err)
	}

	return map[string]any{
		"kind":    "code_intel_index_code",
		"summary": summary,
	}, nil
}

func (server Server) codeIntelEmbeddingCandidates(args json.RawMessage) (any, error) {
	var input codeIntelEmbeddingCandidatesInput

	inlineErr4 := json.Unmarshal(args, &input)
	if inlineErr4 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence embedding-candidates arguments: %w",
			inlineErr4,
		)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	candidates, err := store.EmbeddingCandidates(
		argsContext(),
		codeintel.EmbeddingCandidateQuery{
			RecordKind: input.RecordKind,
			PolicyID:   input.PolicyID,
			SkillID:    input.SkillID,
			Path:       input.Path,
			Limit:      input.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query embedding candidates: %w", err)
	}

	return map[string]any{
		"kind":       "code_intel_embedding_candidates",
		"candidates": candidates,
	}, nil
}

func (server Server) codeIntelCodeChunks(args json.RawMessage) (any, error) {
	var input codeIntelCodeChunksInput

	inlineErr5 := json.Unmarshal(args, &input)
	if inlineErr5 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence code-chunks arguments: %w",
			inlineErr5,
		)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	chunks, err := store.CodeChunks(argsContext(), codeintel.CodeChunkQuery{
		Path:       input.Path,
		Language:   input.Language,
		SymbolKind: input.SymbolKind,
		SymbolName: input.SymbolName,
		SymbolPath: input.SymbolPath,
		Limit:      input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query code chunks: %w", err)
	}

	return map[string]any{
		"kind":   "code_intel_code_chunks",
		"chunks": chunks,
	}, nil
}

func (server Server) codeIntelCodeContext(args json.RawMessage) (any, error) {
	var input codeIntelCodeContextInput

	inlineErr6 := json.Unmarshal(args, &input)
	if inlineErr6 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence code-context arguments: %w",
			inlineErr6,
		)
	}

	if strings.TrimSpace(input.ChunkID) == "" &&
		((strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.SymbolPath) == "") &&
			(strings.TrimSpace(input.Path) == "" || input.Line <= 0)) {
		return nil, apperror.StaticError(
			"chunk_id, both path and symbol_path, or path and line are required",
		)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	context, err := store.CodeContext(argsContext(), codeintel.CodeContextQuery{
		ChunkID:    input.ChunkID,
		Path:       input.Path,
		Root:       server.codeIntelRoot(),
		SymbolPath: input.SymbolPath,
		Line:       input.Line,
		Limit:      input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query code context: %w", err)
	}

	return map[string]any{
		"kind":    "code_intel_code_context",
		"context": context,
	}, nil
}

func (server Server) codeIntelContextCard(args json.RawMessage) (any, error) {
	var input codeIntelContextCardInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence context-card arguments: %w",
			inlineErr0,
		)
	}

	paths := codeIntelInputPaths(input.Path, input.Paths)
	if len(paths) == 0 && strings.TrimSpace(input.SymbolName) == "" &&
		strings.TrimSpace(input.SymbolPath) == "" {
		return nil, apperror.StaticError(
			"path, paths, symbol_name, or symbol_path is required",
		)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)

	targets, truncated, err := server.contextCardTargets(ctx, store, input, paths, limit)
	if err != nil {
		return nil, err
	}

	err = annotateContextTargetsWithCommunities(
		ctx,
		store,
		server.codeIntelRoot(),
		targets,
	)
	if err != nil {
		return nil, err
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"code_files",
		"code_chunks",
		"code_context",
		"ast_finding_links",
	})
	if err != nil {
		return nil, err
	}

	meta.Truncated = meta.Truncated || truncated

	result := map[string]any{
		"kind":           "code_intel_context_card",
		"_meta":          meta,
		"targets":        targets,
		"next_mcp_calls": contextCardNextMCPCalls(targets),
	}

	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = renderContextCardTOON(targets, meta)
	}

	return result, nil
}

func contextCardNextMCPCalls(targets []codeIntelContextTarget) []map[string]any {
	return []map[string]any{
		{
			"tool": "code_intel_code_context",
			"arguments": map[string]any{
				"path":        "<path>",
				"symbol_path": "<symbol_path>",
			},
		},
		{
			"tool": "code_intel_change_risk",
			"arguments": map[string]any{
				"paths": targetPaths(targets),
			},
		},
	}
}

func (server Server) codeIntelRepoMap(args json.RawMessage) (any, error) {
	var input codeIntelRepoMapInput

	inlineErr7 := json.Unmarshal(args, &input)
	if inlineErr7 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence repo-map arguments: %w",
			inlineErr7,
		)
	}

	if strings.TrimSpace(input.Repo) != "" {
		return server.codeIntelWorkspaceRepoMap(input)
	}

	repoMap, rendered, err := server.loadRepoMap(input)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"kind":     "code_intel_repo_map",
		"repo_map": repoMap,
		"toon":     rendered,
	}

	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = rendered
	}

	return result, nil
}

func (server Server) loadRepoMap(
	input codeIntelRepoMapInput,
) (codeintel.RepoMap, string, error) {
	if repoMapRefreshPath(input) == "" {
		return server.loadStoredRepoMap(input)
	}

	return server.loadFreshRepoMap(input)
}

func (server Server) codeIntelChangeRisk(args json.RawMessage) (any, error) {
	var input codeIntelChangeRiskInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence change-risk arguments: %w",
			inlineErr0,
		)
	}

	paths := codeIntelInputPaths(input.Path, input.Paths)
	if len(paths) == 0 {
		return nil, apperror.StaticError("path or paths is required")
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)

	targets := make([]codeIntelRiskTarget, 0, len(paths))
	for _, path := range paths {
		target, targetErr := changeRiskTarget(ctx, store, server.codeIntelRoot(), path, limit)
		if targetErr != nil {
			return nil, targetErr
		}

		targets = append(targets, target)
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"code_files",
		"code_chunks",
		"code_edges",
		"code_health_snapshots",
		"code_health_targets",
		"code_health_evidence",
		"code_health_coverage",
		"finding_occurrences",
		"remediation_outcomes",
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kind":    "code_intel_change_risk",
		"_meta":   meta,
		"targets": targets,
		"next_actions": []string{
			"Run focused tests for each target path before broad checks.",
			"Inspect code_intel_context_card for high-risk targets.",
		},
	}, nil
}

func (server Server) codeIntelHealth(args json.RawMessage) (any, error) {
	var input codeIntelHealthInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse code intelligence health arguments: %w", inlineErr0)
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()

	health, err := store.CodeHealth(ctx, codeintel.CodeHealthQuery{
		Root:     server.codeIntelRoot(),
		Path:     input.Path,
		Limit:    input.Limit,
		Refresh:  input.Refresh || input.LCOV != "",
		Trend:    input.Trend,
		GitHead:  input.GitHead,
		LCOVPath: input.LCOV,
	})
	if err != nil {
		return nil, fmt.Errorf("query code health: %w", err)
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"code_files",
		"code_chunks",
		"git_file_signals",
		"git_cochanges",
		"finding_occurrences",
		"code_health_snapshots",
		"code_health_coverage",
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kind":   "code_intel_health",
		"_meta":  meta,
		"health": health,
		"next_actions": []string{
			"Start with the highest priority target whose evidence matches the change.",
			"Use code_intel_context_card before editing a ranked refactoring target.",
		},
	}, nil
}

func (server Server) codeIntelWhy(args json.RawMessage) (any, error) {
	var input codeIntelWhyInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse code intelligence why arguments: %w", inlineErr0)
	}

	text := strings.TrimSpace(firstNonEmpty(input.Text, input.Query))
	path := strings.TrimSpace(input.Path)
	symbolPath := strings.TrimSpace(input.SymbolPath)

	status := strings.TrimSpace(input.Status)
	if text == "" && path == "" && symbolPath == "" && status == "" {
		return nil, apperror.StaticError("query, path, symbol_path, or status is required")
	}

	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence index: %w", err)
	}
	defer closeAll()

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)
	query := codeintel.DecisionQuery{
		Text:       text,
		Path:       path,
		SymbolPath: symbolPath,
		Status:     status,
		Limit:      limit,
	}

	decisions, err := store.Decisions(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query architectural decisions: %w", err)
	}

	health, err := store.DecisionHealth(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query architectural decision health: %w", err)
	}

	meta, err := server.codeIntelTaskMeta(ctx, store, index, []string{
		"decisions",
		"decision_links",
		"code_files",
		"code_chunks",
	})
	if err != nil {
		return nil, fmt.Errorf("read code intelligence task metadata: %w", err)
	}

	result := map[string]any{
		"kind":           "code_intel_why",
		"_meta":          meta,
		"decisions":      decisions,
		"health":         health,
		"next_mcp_calls": codeIntelWhyNextCalls(path, symbolPath),
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = renderCodeIntelWhyTOON(decisions, health)
	}

	return result, nil
}

func (server Server) codeIntelSessionSnapshot(args json.RawMessage) (any, error) {
	var input codeIntelSessionSnapshotInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence session-snapshot arguments: %w",
			inlineErr0,
		)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	root := server.codeIntelRoot()

	snapshot, err := store.SessionSnapshot(argsContext(), codeintel.SessionSnapshotQuery{
		Root:      root,
		Worktree:  root,
		Provider:  input.Provider,
		SessionID: input.SessionID,
		Limit:     input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query session snapshot: %w", err)
	}

	result := map[string]any{
		"kind":     codeintel.SessionSnapshotKind,
		"snapshot": snapshot,
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = codeintel.FormatSessionSnapshotTOON(snapshot)
	}

	return result, nil
}

func (server Server) repoMapResource() (any, error) {
	_, rendered, err := server.loadStoredRepoMap(codeIntelRepoMapInput{})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"contents": []map[string]string{{
			"uri":      repoMapResourceURI,
			"mimeType": "text/vnd.coding-ethos.toon",
			"text":     rendered,
		}},
	}, nil
}

func (server Server) loadFreshRepoMap(
	input codeIntelRepoMapInput,
) (codeintel.RepoMap, string, error) {
	root := server.codeIntelRoot()

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return codeintel.RepoMap{}, "", fmt.Errorf(
			"open code intelligence store: %w",
			err,
		)
	}

	defer autoPruneCodeIntelDB(root)
	defer closeStore()

	ctx := argsContext()

	_, err = codeintel.NewASTIndexer(store).IndexPaths(
		ctx,
		root,
		repoMapIndexPaths(input),
	)
	if err != nil {
		return codeintel.RepoMap{}, "", fmt.Errorf("refresh repo map index: %w", err)
	}

	return server.readRepoMap(store, root, input)
}

func (server Server) loadStoredRepoMap(
	input codeIntelRepoMapInput,
) (codeintel.RepoMap, string, error) {
	root := server.codeIntelRoot()

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return codeintel.RepoMap{}, "", fmt.Errorf(
			"open code intelligence store: %w",
			err,
		)
	}
	defer closeStore()

	return server.readRepoMap(store, root, input)
}

func (server Server) readRepoMap(
	store *codeintel.Store,
	root string,
	input codeIntelRepoMapInput,
) (codeintel.RepoMap, string, error) {
	ctx := argsContext()

	repoMap, err := store.GlobalRepoMap(ctx, codeintel.RepoMapQuery{
		Path:           input.Path,
		Root:           root,
		Language:       input.Language,
		Limit:          input.Limit,
		SymbolsPerFile: input.SymbolsPerFile,
	})
	if err != nil {
		return codeintel.RepoMap{}, "", fmt.Errorf("query repo map: %w", err)
	}

	return repoMap, codeintel.RenderRepoMapTOON(repoMap), nil
}

func repoMapIndexPaths(input codeIntelRepoMapInput) []string {
	path := repoMapRefreshPath(input)
	if path == "" {
		return nil
	}

	return []string{path}
}

func repoMapRefreshPath(input codeIntelRepoMapInput) string {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return ""
	}

	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return ""
	}

	cleanPath := filepath.ToSlash(cleaned)
	if cleanPath == "." ||
		cleanPath == ".." ||
		strings.HasPrefix(cleanPath, "../") {
		return ""
	}

	return cleanPath
}

func (server Server) codeIntelTaskMeta(
	ctx context.Context,
	store *codeintel.Store,
	index evidence.VectorIndex,
	dataSources []string,
) (codeIntelTaskMeta, error) {
	return server.codeIntelTaskMetaForRoot(
		ctx,
		server.codeIntelRoot(),
		store,
		index,
		dataSources,
	)
}

func (server Server) codeIntelTaskMetaForRoot(
	ctx context.Context,
	root string,
	store *codeintel.Store,
	index evidence.VectorIndex,
	dataSources []string,
) (codeIntelTaskMeta, error) {
	vectorStats, err := index.Stats(ctx)
	if err != nil {
		return codeIntelTaskMeta{}, fmt.Errorf("read vector index stats: %w", err)
	}

	status, err := store.IndexStatus(ctx, vectorStats, codeintel.EmbeddingRecordQuery{
		Backend:    codeintel.VectorBackendDuckDBVSS,
		Collection: "code_chunks",
	})
	if err != nil {
		return codeIntelTaskMeta{}, fmt.Errorf("read code intelligence index status: %w", err)
	}

	fileStats, err := store.CodeFileIndexStats(ctx)
	if err != nil {
		return codeIntelTaskMeta{}, fmt.Errorf(
			"read code intelligence file stats: %w",
			err,
		)
	}

	indexedAt := fileStats.LatestIndexedAtUTC

	meta := codeIntelTaskMeta{
		RepoHeadCommit: currentGitCommit(ctx, root),
		IndexedAtUTC:   indexedAt,
		IndexAge:       indexAgeDescription(indexedAt),
		Fresh:          status.Fresh,
		DataSources:    dataSources,
		Compression:    "bounded_json",
		ReadyRecords:   status.ReadyRecords,
		MissingVectors: status.MissingVectors,
		IndexedFiles:   fileStats.ActiveFiles,
		IndexedChunks:  status.Stats.CodeChunks,
		SchemaVersion:  status.Stats.SchemaVersion,
	}
	if !meta.Fresh {
		meta.StaleWarning = "code-intel vectors are missing for some ready records"
	}

	return meta, nil
}

func indexAgeDescription(indexedAt string) string {
	if strings.TrimSpace(indexedAt) == "" {
		return "unknown"
	}

	indexedTime, err := time.Parse(time.RFC3339Nano, indexedAt)
	if err != nil {
		return "unknown"
	}

	age := max(time.Since(indexedTime), 0)

	return age.Round(time.Second).String()
}

func currentGitCommit(ctx context.Context, root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}

	command := realgit.Command(ctx, false, "-C", root, "rev-parse", "--verify", "HEAD")
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func boundedCodeIntelLimit(value int) int {
	if value <= 0 {
		return codeIntelDefaultTaskLimit
	}

	if value > codeIntelMaxTaskLimit {
		return codeIntelMaxTaskLimit
	}

	return value
}

func codeIntelInputPaths(path string, paths []string) []string {
	seen := map[string]bool{}
	out := []string{}

	for _, candidate := range append([]string{path}, paths...) {
		clean := strings.TrimSpace(candidate)
		if clean == "" || seen[clean] {
			continue
		}

		seen[clean] = true
		out = append(out, clean)
	}

	return out
}

func codeIntelWhyNextCalls(path, symbolPath string) []map[string]any {
	if strings.TrimSpace(path) == "" {
		return []map[string]any{{
			"tool":      "code_intel_overview",
			"arguments": map[string]any{"limit": codeIntelDefaultTaskLimit},
		}}
	}

	contextArgs := map[string]any{"path": path}
	if strings.TrimSpace(symbolPath) != "" {
		contextArgs["symbol_path"] = symbolPath
	}

	return []map[string]any{
		{
			"tool":      "code_intel_context_card",
			"arguments": contextArgs,
		},
		{
			"tool":      "code_intel_change_risk",
			"arguments": map[string]any{"paths": []string{path}},
		},
	}
}

func renderCodeIntelWhyTOON(
	decisions []codeintel.DecisionRecord,
	health codeintel.DecisionHealth,
) string {
	var builder strings.Builder
	builder.WriteString("kind: code_intel_why\n")
	builder.WriteString("decisions: ")
	builder.WriteString(strconv.Itoa(len(decisions)))
	builder.WriteString("\nstale: ")
	builder.WriteString(strconv.Itoa(health.Summary.StaleCount))
	builder.WriteString("\nconflicts: ")
	builder.WriteString(strconv.Itoa(health.Summary.ConflictCount))
	builder.WriteString("\n")

	for _, decision := range decisions {
		builder.WriteString("- id: ")
		builder.WriteString(decision.ID)
		builder.WriteString("\n  status: ")
		builder.WriteString(decision.Status)
		builder.WriteString("\n  title: ")
		builder.WriteString(decision.Title)
		builder.WriteString("\n")
	}

	return builder.String()
}

func citationsFromHybridResults(
	results []codeintel.HybridSearchResult,
	limit int,
) []codeIntelCitation {
	citations := make([]codeIntelCitation, 0, min(len(results), limit))
	for _, result := range results {
		if len(citations) >= limit {
			break
		}

		citations = append(citations, codeIntelCitation{
			Kind:       result.Kind,
			RecordID:   result.RecordID,
			Path:       result.Path,
			PolicyID:   result.PolicyID,
			SkillID:    result.SkillID,
			SearchText: result.Message,
		})
	}

	return citations
}

func retrievalQuality(citationCount int, meta codeIntelTaskMeta) string {
	switch {
	case citationCount == 0:
		return "none"
	case meta.Fresh && citationCount >= 3:
		return codeIntelQualityHigh
	case meta.Fresh:
		return codeIntelQualityMedium
	default:
		return "partial"
	}
}

func answerConfidence(quality string) string {
	switch quality {
	case codeIntelQualityHigh:
		return codeIntelQualityMedium
	case codeIntelQualityMedium:
		return codeIntelQualityLow
	default:
		return codeIntelQualityLow
	}
}

func answerSummaryForRetrieval(quality string) string {
	if quality == "none" {
		return strings.Join([]string{
			"No indexed evidence matched the question;",
			"inspect next actions instead of relying on an answer.",
		}, " ")
	}

	return strings.Join([]string{
		"Relevant indexed evidence is available below;",
		"confidence remains separate from retrieval quality.",
	}, " ")
}

func codeIntelFTSQuery(value string) string {
	terms := []string{}

	for field := range strings.FieldsSeq(value) {
		term := strings.Trim(field, " \t\r\n.,:;!?()[]{}'\"`")
		if term != "" {
			terms = append(terms, term)
		}
	}

	if len(terms) == 0 {
		return value
	}

	return strings.Join(terms, " ")
}

func (server Server) contextCardTargets(
	ctx context.Context,
	store *codeintel.Store,
	input codeIntelContextCardInput,
	paths []string,
	limit int,
) ([]codeIntelContextTarget, bool, error) {
	chunks, truncated, err := contextCardChunks(ctx, store, input, paths, limit)
	if err != nil {
		return nil, false, err
	}

	targetsByPath, targetOrder, targetSeen := contextCardPathTargets(
		ctx,
		store,
		paths,
		chunks,
	)

	for _, chunk := range chunks {
		targetOrder = appendContextCardChunk(
			ctx,
			store,
			input,
			targetsByPath,
			targetSeen,
			targetOrder,
			chunk,
		)
	}

	if len(chunks) != 0 {
		context, contextErr := store.CodeContext(ctx, codeintel.CodeContextQuery{
			Path:       firstNonEmpty(input.Path, chunks[0].Path),
			Root:       server.codeIntelRoot(),
			SymbolPath: firstNonEmpty(input.SymbolPath, chunks[0].SymbolPath),
			Line:       input.Line,
			Limit:      limit,
		})
		if contextErr == nil {
			target := targetsByPath[context.Chunk.Path]
			target.Context = &context
			targetsByPath[context.Chunk.Path] = target
		}
	}

	targets := make([]codeIntelContextTarget, 0, len(targetsByPath))

	for _, path := range targetOrder {
		if target, ok := targetsByPath[path]; ok {
			targets = append(targets, target)
		}
	}

	return targets, truncated, nil
}

func contextCardPathTargets(
	ctx context.Context,
	store *codeintel.Store,
	paths []string,
	chunks []codeintel.CodeChunk,
) (map[string]codeIntelContextTarget, []string, map[string]bool) {
	targetsByPath := map[string]codeIntelContextTarget{}
	targetOrder := make([]string, 0, len(paths)+len(chunks))
	targetSeen := map[string]bool{}

	for _, path := range paths {
		targetsByPath[path] = contextTargetForPath(ctx, store, path)
		targetOrder = append(targetOrder, path)
		targetSeen[path] = true
	}

	return targetsByPath, targetOrder, targetSeen
}

func appendContextCardChunk(
	ctx context.Context,
	store *codeintel.Store,
	input codeIntelContextCardInput,
	targetsByPath map[string]codeIntelContextTarget,
	targetSeen map[string]bool,
	targetOrder []string,
	chunk codeintel.CodeChunk,
) []string {
	if !targetSeen[chunk.Path] {
		targetOrder = append(targetOrder, chunk.Path)
		targetSeen[chunk.Path] = true
	}

	target := targetsByPath[chunk.Path]
	if target.Path == "" {
		target = contextTargetForPath(ctx, store, chunk.Path)
	}

	if !input.IncludeRaw {
		chunk.RawText = ""
	}

	target.Chunks = append(target.Chunks, chunk)
	targetsByPath[chunk.Path] = target

	return targetOrder
}

func contextCardChunks(
	ctx context.Context,
	store *codeintel.Store,
	input codeIntelContextCardInput,
	paths []string,
	limit int,
) ([]codeintel.CodeChunk, bool, error) {
	if len(paths) == 0 {
		chunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
			SymbolName: input.SymbolName,
			SymbolPath: input.SymbolPath,
			Limit:      limit,
		})
		if err != nil {
			return nil, false, fmt.Errorf("query context-card chunks: %w", err)
		}

		return chunks, len(chunks) == limit, nil
	}

	chunks := []codeintel.CodeChunk{}
	truncated := false

	for _, path := range paths {
		pathChunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
			Path:       path,
			SymbolName: input.SymbolName,
			SymbolPath: input.SymbolPath,
			Limit:      limit,
		})
		if err != nil {
			return nil, false, fmt.Errorf("query context-card chunks for %s: %w", path, err)
		}

		truncated = truncated || len(pathChunks) == limit
		chunks = append(chunks, pathChunks...)
	}

	return chunks, truncated, nil
}

func contextTargetForPath(
	ctx context.Context,
	store *codeintel.Store,
	path string,
) codeIntelContextTarget {
	file, found, err := store.GetCodeFile(ctx, path)
	if err != nil {
		return codeIntelContextTarget{Path: path}
	}

	return codeIntelContextTarget{
		Path:       path,
		File:       file,
		Found:      found,
		IndexFresh: found && file.DeletedAtUTC == "" && file.StaleReason == "",
	}
}

func targetPaths(targets []codeIntelContextTarget) []string {
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		paths = append(paths, target.Path)
	}

	return paths
}

func annotateContextTargetsWithCommunities(
	ctx context.Context,
	store *codeintel.Store,
	root string,
	targets []codeIntelContextTarget,
) error {
	if len(targets) == 0 {
		return nil
	}

	communities, err := store.CodeCommunities(ctx, codeintel.CodeCommunityQuery{
		Root:  root,
		Limit: contextCardCommunityLimit,
	})
	if err != nil {
		return fmt.Errorf("query context-card communities: %w", err)
	}

	communityByPath := map[string]string{}

	for _, community := range communities {
		for _, path := range community.MemberPaths {
			communityByPath[path] = community.ID
		}
	}

	for index := range targets {
		targets[index].CommunityID = communityByPath[targets[index].Path]
	}

	return nil
}

func renderContextCardTOON(
	targets []codeIntelContextTarget,
	meta codeIntelTaskMeta,
) string {
	lines := make([]string, 0, contextCardTOONHeaderLines+len(targets))
	lines = append(lines,
		"tool: code_intel_context_card",
		fmt.Sprintf("fresh: %t", meta.Fresh),
		fmt.Sprintf("targets[%d]{path,found,chunks,index_fresh,community}:", len(targets)),
	)

	for _, target := range targets {
		lines = append(lines, fmt.Sprintf(
			"  %s,%t,%d,%t,%s",
			target.Path,
			target.Found,
			len(target.Chunks),
			target.IndexFresh,
			target.CommunityID,
		))
	}

	return strings.Join(lines, "\n")
}

func changeRiskTarget(
	ctx context.Context,
	store *codeintel.Store,
	root string,
	path string,
	limit int,
) (codeIntelRiskTarget, error) {
	file, found, err := store.GetCodeFile(ctx, path)
	if err != nil {
		return codeIntelRiskTarget{}, fmt.Errorf(
			"read code file metadata for %s: %w",
			path,
			err,
		)
	}

	chunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
		Path:  path,
		Limit: limit,
	})
	if err != nil {
		return codeIntelRiskTarget{}, fmt.Errorf("query code chunks for %s: %w", path, err)
	}

	failures, err := store.RepeatedFailures(ctx, codeintel.RepeatedFailureQuery{
		Path:  path,
		Limit: limit,
	})
	if err != nil {
		return codeIntelRiskTarget{}, fmt.Errorf(
			"query repeated failures for %s: %w",
			path,
			err,
		)
	}

	gitFreshness, gitSignals, reviewers, err := changeRiskGitSignals(
		ctx,
		store,
		root,
		path,
	)
	if err != nil {
		return codeIntelRiskTarget{}, err
	}

	healthTargets, err := storedHealthTargets(ctx, store, root, path)
	if err != nil {
		return codeIntelRiskTarget{}, fmt.Errorf("query health score for %s: %w", path, err)
	}

	reasons := changeRiskReasons(
		file,
		found,
		len(chunks),
		limit,
		len(failures),
		gitFreshness,
		gitSignals,
	)

	return codeIntelRiskTarget{
		Path:               path,
		File:               &file,
		Chunks:             chunks,
		GitSignalFreshness: &gitFreshness,
		GitSignals:         gitSignals,
		Health:             healthTargets,
		Reviewers:          gitReviewerSuggestions(reviewers),
		RepeatedFailures:   failures,
		RiskLevel:          riskLevelForReasons(reasons),
		Reasons:            reasons,
		RecommendedChecks:  recommendedChecksForPath(path),
	}, nil
}

func storedHealthTargets(
	ctx context.Context,
	store *codeintel.Store,
	root string,
	path string,
) ([]codeintel.CodeHealthTarget, error) {
	health, foundHealth, err := store.StoredCodeHealth(ctx, codeintel.CodeHealthQuery{
		Root:  root,
		Path:  path,
		Limit: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("read stored code health: %w", err)
	}

	if !foundHealth {
		return []codeintel.CodeHealthTarget{}, nil
	}

	return health.Targets, nil
}

func changeRiskReasons(
	file codeintel.CodeFile,
	found bool,
	chunkCount int,
	limit int,
	failureCount int,
	gitFreshness codeintel.GitSignalSummary,
	gitSignals []codeintel.GitFileSignal,
) []string {
	reasons := []string{}
	if !found {
		reasons = append(reasons, "target is not indexed")
	}

	if chunkCount >= limit {
		reasons = append(reasons, "target has many indexed chunks")
	}

	if failureCount != 0 {
		reasons = append(reasons, "target has repeated failure evidence")
	}

	if gitFreshness.Stale {
		reasons = append(reasons, "git-signal index metadata is stale")
	}

	if len(gitSignals) != 0 && gitSignals[0].HotspotScore >= codeIntelHotspotThreshold {
		reasons = append(reasons, "target is a git-history hotspot")
	}

	if len(gitSignals) != 0 && gitSignals[0].AuthorCount >= codeIntelAuthorRiskCount {
		reasons = append(reasons, "target has multi-author ownership risk")
	}

	if found && (file.StaleReason != "" || file.DeletedAtUTC != "") {
		reasons = append(reasons, "target index metadata is stale")
	}

	return reasons
}

func changeRiskGitSignals(
	ctx context.Context,
	store *codeintel.Store,
	root string,
	path string,
) (
	codeintel.GitSignalSummary,
	[]codeintel.GitFileSignal,
	[]codeintel.GitReviewerSuggestion,
	error,
) {
	freshness := codeintel.GitSignalSummary{}

	summary, err := store.GitSignalSummary(ctx, root)
	if err != nil {
		freshness.Stale = true
	} else {
		freshness = summary
	}

	gitSignals, err := store.GitSignals(ctx, codeintel.GitSignalQuery{
		Path:  path,
		Limit: 1,
	})
	if err != nil {
		return codeintel.GitSignalSummary{}, nil, nil, fmt.Errorf(
			"query git signals for %s: %w",
			path,
			err,
		)
	}

	reviewers, err := store.GitReviewerSuggestions(
		ctx,
		codeintel.GitReviewerSuggestionQuery{
			Paths: []string{path},
			Limit: codeIntelReviewerLimit,
		},
	)
	if err != nil {
		return codeintel.GitSignalSummary{}, nil, nil, fmt.Errorf(
			"query git reviewers for %s: %w",
			path,
			err,
		)
	}

	return freshness, gitSignals, reviewers, nil
}

func riskLevelForReasons(reasons []string) string {
	switch {
	case len(reasons) > codeIntelMediumRiskReason:
		return codeIntelQualityHigh
	case len(reasons) == 1:
		return codeIntelQualityMedium
	default:
		return codeIntelQualityLow
	}
}

func recommendedChecksForPath(path string) []map[string]string {
	return []map[string]string{
		{"command": "make check", "reason": "canonical repository gate"},
		{
			"mcp":    "code_intel_context_card",
			"reason": "inspect local symbol and evidence context for " + path,
		},
	}
}

func (server Server) codeSimilarityCheck(args json.RawMessage) (any, error) {
	var input codeSimilarityCheckInput

	inlineErr8 := json.Unmarshal(args, &input)
	if inlineErr8 != nil {
		return nil, fmt.Errorf(
			"parse code similarity check arguments: %w",
			inlineErr8,
		)
	}

	if strings.TrimSpace(input.Code) == "" {
		return nil, apperror.StaticError("code is required")
	}

	root := server.codeIntelRoot()

	settings, err := similarityconfig.LoadFromRoot(root)
	if err != nil {
		return nil, fmt.Errorf("load similarity settings: %w", err)
	}

	settings, err = settings.WithStructuralThreshold(input.Threshold)
	if err != nil {
		return nil, fmt.Errorf("validate similarity threshold: %w", err)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	matches, err := store.SimilarCode(argsContext(), codeintel.SimilarCodeQuery{
		Settings: settings,
		Code:     input.Code,
		Path:     input.Path,
		Language: input.Language,
		Limit:    input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("check code similarity: %w", err)
	}

	return map[string]any{
		"kind":      "code_similarity_check",
		"enabled":   settings.Enabled,
		"threshold": settings.StructuralThreshold,
		"matches":   matches,
	}, nil
}

func (server Server) openCodeIntel() (
	*codeintel.Store,
	evidence.VectorIndex,
	func(),
	error,
) {
	return server.openCodeIntelForRoot(server.codeIntelRoot())
}

func (server Server) openCodeIntelStore() (*codeintel.Store, func(), error) {
	return server.openCodeIntelStoreForRoot(server.codeIntelRoot())
}

func (server Server) codeIntelRoot() string {
	return firstNonEmpty(server.runtime.ConsumerRoot, server.runtime.InvocationCwd)
}

func autoPruneCodeIntelDB(root string) {
	err := outputsurface.AutoPruneCodeIntelDB(argsContext(), root)
	if err == nil {
		return
	}

	debuglog.Debug(
		"mcp.code_intel.auto_prune.warn",
		zap.String("root", root),
		zap.Error(err),
	)
}
