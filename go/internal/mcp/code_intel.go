// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func (server Server) codeIntelSearch(args json.RawMessage) (any, error) {
	var input codeIntelSearchInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence search arguments: %w", err)
	}
	if strings.TrimSpace(input.Text) == "" && len(input.Vector) == 0 {
		return nil, fmt.Errorf("text or vector is required")
	}
	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, err
	}
	defer closeAll()

	results, err := store.HybridSearch(argsContext(), index, codeintel.HybridSearchQuery{
		Filters:    input.Filters,
		Text:       input.Text,
		Collection: firstNonEmpty(input.Collection, "remediations"),
		ModelID:    input.ModelID,
		PolicyID:   input.PolicyID,
		SkillID:    input.SkillID,
		Path:       input.Path,
		Vector:     input.Vector,
		Limit:      input.Limit,
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kind":    "code_intel_search",
		"backend": "sqlite-vec",
		"results": results,
	}, nil
}

func (server Server) codeIntelIndexStatus(args json.RawMessage) (any, error) {
	var input codeIntelIndexStatusInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence index-status arguments: %w", err)
	}
	store, index, closeAll, err := server.openCodeIntel()
	if err != nil {
		return nil, err
	}
	defer closeAll()

	ctx := argsContext()
	vectorStats, err := index.Stats(ctx)
	if err != nil {
		return nil, err
	}
	status, err := store.IndexStatus(ctx, vectorStats, codeintel.EmbeddingRecordQuery{
		Backend:    "sqlite-vec",
		Collection: firstNonEmpty(input.Collection, "remediations"),
		ModelID:    input.ModelID,
	})
	if err != nil {
		return nil, err
	}

	return status, nil
}

func (server Server) codeIntelHookUsage(args json.RawMessage) (any, error) {
	var input codeIntelHookUsageInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence hook-usage arguments: %w", err)
	}
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return map[string]any{
		"kind":    "code_intel_hook_usage",
		"results": results,
	}, nil
}

func (server Server) codeIntelIndexCode(args json.RawMessage) (any, error) {
	var input codeIntelIndexCodeInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence index-code arguments: %w", err)
	}
	root := server.codeIntelRoot()
	if strings.TrimSpace(root) == "" {
		return nil, errManagedLintRuntimeUnavailable
	}
	ctx := argsContext()
	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	summary, err := codeintel.NewASTIndexer(store).IndexPaths(ctx, root, input.Paths)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kind":    "code_intel_index_code",
		"summary": summary,
	}, nil
}

func (server Server) codeIntelEmbeddingCandidates(args json.RawMessage) (any, error) {
	var input codeIntelEmbeddingCandidatesInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence embedding-candidates arguments: %w", err)
	}
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, err
	}
	defer closeStore()

	candidates, err := store.EmbeddingCandidates(argsContext(), codeintel.EmbeddingCandidateQuery{
		RecordKind: input.RecordKind,
		PolicyID:   input.PolicyID,
		SkillID:    input.SkillID,
		Path:       input.Path,
		Limit:      input.Limit,
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"kind":       "code_intel_embedding_candidates",
		"candidates": candidates,
	}, nil
}

func (server Server) codeIntelCodeChunks(args json.RawMessage) (any, error) {
	var input codeIntelCodeChunksInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence code-chunks arguments: %w", err)
	}
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return map[string]any{
		"kind":   "code_intel_code_chunks",
		"chunks": chunks,
	}, nil
}

func (server Server) codeIntelCodeContext(args json.RawMessage) (any, error) {
	var input codeIntelCodeContextInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse code intelligence code-context arguments: %w", err)
	}
	if strings.TrimSpace(input.ChunkID) == "" &&
		((strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.SymbolPath) == "") &&
			(strings.TrimSpace(input.Path) == "" || input.Line <= 0)) {
		return nil, fmt.Errorf("chunk_id, both path and symbol_path, or path and line are required")
	}
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return map[string]any{
		"kind":    "code_intel_code_context",
		"context": context,
	}, nil
}

func (server Server) openCodeIntel() (*codeintel.Store, evidence.VectorIndex, func(), error) {
	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, nil, nil, err
	}
	ctx := argsContext()
	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: "sqlite-vec",
		URI:     codeintel.DefaultVectorPath(server.codeIntelRoot()),
	})
	if err != nil {
		closeStore()
		return nil, nil, nil, err
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
		return nil, nil, err
	}
	closeStore := func() {
		_ = store.Close()
	}

	return store, closeStore, nil
}

func (server Server) codeIntelRoot() string {
	return firstNonEmpty(server.runtime.ConsumerRoot, server.runtime.InvocationCwd)
}
