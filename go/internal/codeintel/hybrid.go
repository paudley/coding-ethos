// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	hybridCandidateMultiplier = 2
	indexStatusRecordLimit    = 100000
)

func (store *Store) HybridSearch(
	ctx context.Context,
	index evidence.VectorIndex,
	query HybridSearchQuery,
) ([]HybridSearchResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	resultMap := map[string]HybridSearchResult{}

	if strings.TrimSpace(query.Text) != "" {
		ftsResults, err := store.Search(
			ctx,
			SearchQuery{
				Text:  query.Text,
				Limit: limit * hybridCandidateMultiplier,
			},
		)
		if err != nil {
			return nil, err
		}

		for position, result := range ftsResults {
			item := hybridFromFTS(result, position)
			if !hybridMatches(item, query) {
				continue
			}

			resultMap[hybridKey(item)] = item
		}
	}

	if len(query.Vector) > 0 {
		err := store.addHybridVectorMatches(ctx, index, query, limit, resultMap)
		if err != nil {
			return nil, err
		}
	}

	results := make([]HybridSearchResult, 0, len(resultMap))
	for _, result := range resultMap {
		result = applyOutcomeScore(result)
		results = append(results, result)
	}

	slices.SortFunc(results, func(left, right HybridSearchResult) int {
		if left.Score != right.Score {
			if left.Score > right.Score {
				return -1
			}

			return 1
		}

		return strings.Compare(left.RecordID, right.RecordID)
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (store *Store) addHybridVectorMatches(
	ctx context.Context,
	index evidence.VectorIndex,
	query HybridSearchQuery,
	limit int,
	resultMap map[string]HybridSearchResult,
) error {
	vectorResults, err := index.Search(ctx, evidence.VectorQuery{
		Collection: firstNonEmpty(query.Collection, "remediations"),
		ModelID:    query.ModelID,
		Vector:     query.Vector,
		Filters:    hybridVectorFilters(query),
		Limit:      limit * hybridCandidateMultiplier,
	})
	if err != nil {
		return fmt.Errorf("search vector index: %w", err)
	}

	for _, match := range vectorResults {
		item := hybridFromVector(match)
		if !hybridMatches(item, query) {
			continue
		}

		mergeHybridVectorMatch(resultMap, item, match)
	}

	return nil
}

func mergeHybridVectorMatch(
	resultMap map[string]HybridSearchResult,
	item HybridSearchResult,
	match evidence.VectorMatch,
) {
	key := hybridKey(item)

	existing, found := resultMap[key]
	if !found {
		resultMap[key] = item

		return
	}

	existing.Source = joinSource(existing.Source, "vector")
	existing.VectorID = match.ID

	existing.VectorScore = match.Score
	if existing.Outcome == "" {
		existing.Outcome = item.Outcome
	}

	if len(existing.Metadata) == 0 {
		existing.Metadata = item.Metadata
	}

	existing.Score += match.Score * hybridCandidateMultiplier
	resultMap[key] = existing
}

func (store *Store) IndexStatus(
	ctx context.Context,
	vectorStats evidence.VectorStats,
	query EmbeddingRecordQuery,
) (IndexStatus, error) {
	stats, err := store.Stats(ctx)
	if err != nil {
		return IndexStatus{}, err
	}

	records, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		Backend:    query.Backend,
		Collection: query.Collection,
		ModelID:    query.ModelID,
		Limit:      indexStatusRecordLimit,
	})
	if err != nil {
		return IndexStatus{}, err
	}

	status := IndexStatus{
		Stats:            stats,
		VectorStats:      vectorStats,
		EmbeddingRecords: len(records),
		Backend:          query.Backend,
		ModelID:          query.ModelID,
		Collection:       query.Collection,
	}
	status.ReadyRecords = stats.SARIFResults + stats.RemediationOutcomes +
		stats.Remediations + stats.CodeChunks

	status.MissingVectors = max(status.ReadyRecords-status.EmbeddingRecords, 0)

	status.Fresh = status.MissingVectors == 0

	return status, nil
}

func hybridFromFTS(result SearchResult, position int) HybridSearchResult {
	return HybridSearchResult{
		Kind:     result.Kind,
		RecordID: result.RecordID,
		TraceID:  result.TraceID,
		PolicyID: result.PolicyID,
		SkillID:  result.SkillID,
		Path:     result.Path,
		Message:  result.Message,
		Source:   "fts",
		Score:    1 / float64(position+1),
		FTSScore: 1 / float64(position+1),
	}
}

func hybridFromVector(match evidence.VectorMatch) HybridSearchResult {
	metadata := match.Metadata
	kind := firstNonEmpty(metadata["record_kind"], metadata["kind"])
	recordID := firstNonEmpty(metadata["record_id"], match.ID)

	return HybridSearchResult{
		Metadata:    metadata,
		Kind:        kind,
		RecordID:    recordID,
		TraceID:     metadata["trace_id"],
		PolicyID:    metadata["policy_id"],
		SkillID:     metadata["skill_id"],
		Path:        metadata["path"],
		Message:     metadata["message"],
		Source:      "vector",
		Outcome:     metadata["outcome"],
		VectorID:    match.ID,
		Score:       match.Score * hybridCandidateMultiplier,
		VectorScore: match.Score,
	}
}

func hybridVectorFilters(query HybridSearchQuery) map[string]string {
	filters := map[string]string{}

	for key, value := range query.Filters {
		if strings.TrimSpace(value) != "" {
			filters[key] = strings.TrimSpace(value)
		}
	}

	if strings.TrimSpace(query.PolicyID) != "" {
		filters["policy_id"] = strings.TrimSpace(query.PolicyID)
	}

	if strings.TrimSpace(query.SkillID) != "" {
		filters["skill_id"] = strings.TrimSpace(query.SkillID)
	}

	if strings.TrimSpace(query.Path) != "" {
		filters["path"] = strings.TrimSpace(query.Path)
	}

	return filters
}

func hybridMatches(result HybridSearchResult, query HybridSearchQuery) bool {
	if strings.TrimSpace(query.PolicyID) != "" && result.PolicyID != query.PolicyID {
		return false
	}

	if strings.TrimSpace(query.SkillID) != "" && result.SkillID != query.SkillID {
		return false
	}

	if strings.TrimSpace(query.Path) != "" && result.Path != query.Path {
		return false
	}

	return true
}

func hybridKey(result HybridSearchResult) string {
	return result.Kind + "\x00" + result.RecordID
}

func joinSource(left, right string) string {
	if left == "" {
		return right
	}

	if right == "" || strings.Contains(left, right) {
		return left
	}

	return left + "+" + right
}

func applyOutcomeScore(result HybridSearchResult) HybridSearchResult {
	switch result.Outcome {
	case "fixed":
		result.Score += 2
	case "repeated":
		result.Score -= 1
	case "superseded":
		result.Score -= 0.5
	}

	return result
}
