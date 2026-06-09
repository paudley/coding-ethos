// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

var errWorkspaceRepoAliasMissing = apperror.StaticError(
	"workspace repo alias is not registered",
)

func (server Server) codeIntelWorkspaceSearch(
	input codeIntelSearchInput,
	text string,
) (any, error) {
	repos, err := server.workspaceReposForScope(input.Repo)
	if err != nil {
		return nil, err
	}

	ctx := argsContext()
	limit := boundedCodeIntelLimit(input.Limit)
	results := []codeintel.HybridSearchResult{}

	for _, repo := range repos {
		store, index, closeAll, err := server.openCodeIntelForRoot(repo.Path)
		if err != nil {
			return nil, fmt.Errorf(
				"open code intelligence index for repo %s: %w",
				repo.Alias,
				err,
			)
		}

		repoResults, err := store.HybridSearch(
			ctx,
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
				Limit:      limit,
			},
		)

		closeAll()

		if err != nil {
			return nil, fmt.Errorf(
				"run code intelligence search for repo %s: %w",
				repo.Alias,
				err,
			)
		}

		results = append(results, annotateWorkspaceSearchResults(repo, repoResults)...)
	}

	return map[string]any{
		"kind":    "code_intel_search",
		"backend": codeintel.VectorBackendDuckDBVSS,
		"repo":    strings.TrimSpace(input.Repo),
		"results": capWorkspaceSearchResults(results, limit),
	}, nil
}

func capWorkspaceSearchResults(
	results []codeintel.HybridSearchResult,
	limit int,
) []codeintel.HybridSearchResult {
	if len(results) > limit {
		return results[:limit]
	}

	return results
}

func annotateWorkspaceSearchResults(
	repo codeintel.WorkspaceRepo,
	results []codeintel.HybridSearchResult,
) []codeintel.HybridSearchResult {
	annotated := make([]codeintel.HybridSearchResult, 0, len(results))
	for _, result := range results {
		if result.Metadata == nil {
			result.Metadata = map[string]string{}
		}

		result.Metadata["repo"] = repo.Alias
		result.Metadata["repo_root"] = repo.Path
		annotated = append(annotated, result)
	}

	return annotated
}

func (server Server) codeIntelWorkspaceAnswer(
	input codeIntelAnswerInput,
	question string,
) (any, error) {
	repos, err := server.workspaceReposForScope(input.Repo)
	if err != nil {
		return nil, err
	}

	limit := boundedCodeIntelLimit(input.Limit)
	paths := codeIntelInputPaths(input.Path, input.Paths)
	results := []codeintel.HybridSearchResult{}
	metaByRepo := map[string]codeIntelTaskMeta{}

	for _, repo := range repos {
		repoResults, meta, err := server.codeIntelWorkspaceAnswerRepo(
			repo,
			question,
			paths,
			limit,
		)
		if err != nil {
			return nil, err
		}

		metaByRepo[repo.Alias] = meta

		results = append(results, repoResults...)
	}

	if len(results) > limit {
		results = results[:limit]
	}

	citations := citationsFromHybridResults(results, limit)

	quality := codeIntelQualityLow
	if len(citations) > 0 {
		quality = codeIntelQualityMedium
	}

	return map[string]any{
		"kind":              "code_intel_answer",
		"repo":              strings.TrimSpace(input.Repo),
		"_meta_by_repo":     metaByRepo,
		"question":          question,
		"answer":            answerSummaryForRetrieval(quality),
		"retrieval_quality": quality,
		"confidence":        answerConfidence(quality),
		"citations":         citations,
		"results":           results,
		"next_actions": []string{
			"Inspect cited records before editing.",
			"Call code_intel_context_card with the cited repo and path before changing files.",
		},
	}, nil
}

func (server Server) codeIntelWorkspaceAnswerRepo(
	repo codeintel.WorkspaceRepo,
	question string,
	paths []string,
	limit int,
) ([]codeintel.HybridSearchResult, codeIntelTaskMeta, error) {
	store, index, closeAll, err := server.openCodeIntelForRoot(repo.Path)
	if err != nil {
		return nil, codeIntelTaskMeta{},
			fmt.Errorf("open code intelligence index for repo %s: %w", repo.Alias, err)
	}
	defer closeAll()

	ctx := argsContext()

	results, err := codeIntelAnswerResults(ctx, store, index, question, paths, limit)
	if err != nil {
		return nil, codeIntelTaskMeta{},
			fmt.Errorf("answer code intelligence question for repo %s: %w", repo.Alias, err)
	}

	meta, err := server.codeIntelTaskMetaForRoot(ctx, repo.Path, store, index, []string{
		"hybrid_search",
		"code_chunks",
		"remediation_evidence",
	})
	if err != nil {
		return nil, codeIntelTaskMeta{},
			fmt.Errorf("read code intelligence task metadata for repo %s: %w", repo.Alias, err)
	}

	return annotateWorkspaceSearchResults(repo, results), meta, nil
}

func (server Server) codeIntelWorkspaceRepoMap(
	input codeIntelRepoMapInput,
) (any, error) {
	repos, err := server.workspaceReposForScope(input.Repo)
	if err != nil {
		return nil, err
	}

	maps := []map[string]any{}

	for _, repo := range repos {
		store, closeStore, err := server.openCodeIntelStoreForRoot(repo.Path)
		if err != nil {
			return nil, fmt.Errorf(
				"open code intelligence store for repo %s: %w",
				repo.Alias,
				err,
			)
		}

		repoMap, rendered, err := server.readRepoMap(store, repo.Path, input)

		closeStore()

		if err != nil {
			return nil, fmt.Errorf("query repo map for repo %s: %w", repo.Alias, err)
		}

		maps = append(maps, map[string]any{
			"repo":     repo.Alias,
			"repo_map": repoMap,
			"toon":     rendered,
		})
	}

	result := map[string]any{
		"kind":      "code_intel_repo_map",
		"repo":      strings.TrimSpace(input.Repo),
		"repo_maps": maps,
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = renderWorkspaceRepoMapsTOON(maps)
	}

	return result, nil
}

func renderWorkspaceRepoMapsTOON(maps []map[string]any) string {
	var builder strings.Builder

	for _, repoMap := range maps {
		repo, repoFound := repoMap["repo"].(string)
		if !repoFound {
			continue
		}

		rendered, renderedFound := repoMap["toon"].(string)
		if !renderedFound {
			continue
		}

		builder.WriteString("repo: " + repo + "\n")
		builder.WriteString(rendered)
		builder.WriteString("\n")
	}

	return builder.String()
}

func (server Server) codeIntelWorkspaceStatus(args json.RawMessage) (any, error) {
	var input codeIntelWorkspaceStatusInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse code intelligence workspace status arguments: %w", err)
	}

	status, err := server.loadCodeIntelWorkspaceStatus(input.Refresh)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"kind":   "code_intel_workspace_status",
		"status": status,
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = renderWorkspaceStatusTOON(status)
	}

	return result, nil
}

func (server Server) loadCodeIntelWorkspaceStatus(
	refresh bool,
) (codeintel.WorkspaceStatus, error) {
	root := server.codeIntelRoot()
	if refresh {
		status, err := codeintel.RefreshWorkspaceStatus(argsContext(), root)
		if err != nil {
			return codeintel.WorkspaceStatus{}, fmt.Errorf("refresh workspace status: %w", err)
		}

		return status, nil
	}

	registry, err := codeintel.LoadWorkspaceRegistry(root)
	if err != nil {
		return codeintel.WorkspaceStatus{}, fmt.Errorf("load workspace registry: %w", err)
	}

	status, err := codeintel.WorkspaceStatusForRegistry(argsContext(), registry)
	if err != nil {
		return codeintel.WorkspaceStatus{}, fmt.Errorf("read workspace status: %w", err)
	}

	return status, nil
}

func (server Server) openCodeIntelForRoot(
	root string,
) (
	*codeintel.Store,
	evidence.VectorIndex,
	func(),
	error,
) {
	store, closeStore, err := server.openCodeIntelStoreForRoot(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open code intelligence store: %w", err)
	}

	ctx := argsContext()

	index, err := codeintel.NewVectorIndex(ctx, codeintel.VectorBackendConfig{
		Backend: codeintel.VectorBackendDuckDBVSS,
		URI:     codeintel.DefaultVectorPath(root),
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

func (server Server) openCodeIntelStoreForRoot(
	root string,
) (*codeintel.Store, func(), error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil, errCodeIntelRootUnavailable
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

func (server Server) workspaceReposForScope(
	scope string,
) ([]codeintel.WorkspaceRepo, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, apperror.StaticError("workspace repo scope is required")
	}

	registry, err := codeintel.LoadWorkspaceRegistry(server.codeIntelRoot())
	if err != nil {
		return nil, fmt.Errorf("load workspace registry: %w", err)
	}

	if scope == "all" {
		return registry.Repos, nil
	}

	repo, ok := codeintel.WorkspaceRepoByAlias(registry, scope)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errWorkspaceRepoAliasMissing, scope)
	}

	return []codeintel.WorkspaceRepo{repo}, nil
}

func renderWorkspaceStatusTOON(status codeintel.WorkspaceStatus) string {
	var builder strings.Builder
	builder.WriteString("coding_ethos_workspace_status:\n")
	builder.WriteString("  root: " + strconv.Quote(status.Root) + "\n")
	builder.WriteString("  repos: " + strconv.Itoa(status.Stats.Repos) + "\n")
	builder.WriteString("  stale: " + strconv.Itoa(status.Stats.Stale) + "\n")
	builder.WriteString("  cochanges: " + strconv.Itoa(status.Stats.CoChanges) + "\n")
	builder.WriteString("  contracts: " + strconv.Itoa(status.Stats.Contracts) + "\n")
	builder.WriteString("  registered:\n")

	for _, repo := range status.Repos {
		builder.WriteString("    - alias: " + strconv.Quote(repo.Alias) + "\n")
		builder.WriteString("      path: " + strconv.Quote(repo.Path) + "\n")
		builder.WriteString("      stale: " + strconv.FormatBool(repo.Stale) + "\n")

		if repo.StaleWarning != "" {
			builder.WriteString("      warning: " + strconv.Quote(repo.StaleWarning) + "\n")
		}
	}

	return builder.String()
}
