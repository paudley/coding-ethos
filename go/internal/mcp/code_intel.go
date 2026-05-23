// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

const semanticSearchContextLimit = 5

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
		"backend": "sqlite-vec",
		"results": results,
	}, nil
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
		"backend": "sqlite-vec",
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

func (server Server) codeIntelRepoMap(args json.RawMessage) (any, error) {
	var input codeIntelRepoMapInput

	inlineErr7 := json.Unmarshal(args, &input)
	if inlineErr7 != nil {
		return nil, fmt.Errorf(
			"parse code intelligence repo-map arguments: %w",
			inlineErr7,
		)
	}

	repoMap, rendered, err := server.loadFreshRepoMap(input)
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
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return nil
	}

	return []string{path}
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
