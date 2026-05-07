// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func (server Server) codeIntelSearch(args json.RawMessage) (any, error) {
	var input codeIntelSearchInput

	inlineErr0 := json.Unmarshal(args, &input)
	if inlineErr0 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence search arguments: %w",
			inlineErr0,
		)
	}

	if strings.TrimSpace(input.Text) == "" && len(input.Vector) == 0 {
		return nil, apperror.StaticError("text or vector is required")
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
			Text:       input.Text,
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
		"backend": "sqlite-vec",
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
		Backend:    "sqlite-vec",
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
		return nil, errManagedLintRuntimeUnavailable
	}

	ctx := argsContext()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
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

func (server Server) openCodeIntel() (
	*codeintel.Store,
	evidence.VectorIndex,
	func(),
	error,
) {
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open code intelligence store: %w", err)
	}

	ctx := argsContext()

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: "sqlite-vec",
		URI:     codeintel.DefaultVectorPath(server.codeIntelRoot()),
	})
	if err != nil {
		closeStore()

		return nil, nil, nil, fmt.Errorf("open vector index: %w", err)
	}

	closeAll := func() {
		if closer, ok := index.(interface{ Close() error }); ok {
			_ = closer.Close()
		}

		closeStore()
	}

	return store, index, closeAll, nil
}

func (server Server) openCodeIntelStore() (*codeintel.Store, func(), error) {
	root := server.codeIntelRoot()
	if strings.TrimSpace(root) == "" {
		return nil, nil, errManagedLintRuntimeUnavailable
	}

	store, err := codeintel.Open(argsContext(), codeintel.DefaultDBPath(root))
	if err != nil {
		return nil, nil, fmt.Errorf("open code intelligence store: %w", err)
	}

	closeStore := func() {
		_ = store.Close()
	}

	return store, closeStore, nil
}

func (server Server) codeIntelRoot() string {
	return firstNonEmpty(server.runtime.ConsumerRoot, server.runtime.InvocationCwd)
}
