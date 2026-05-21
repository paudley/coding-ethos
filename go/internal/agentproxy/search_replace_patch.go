// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import "strings"

const (
	SearchReplaceStatusOK        = "ok"
	SearchReplaceStatusEmpty     = "empty_search"
	SearchReplaceStatusMissing   = "missing"
	SearchReplaceStatusAmbiguous = "ambiguous"
)

type SearchReplaceBlock struct {
	Search  string
	Replace string
}

type SearchReplaceBlockResult struct {
	Status     string
	Index      int
	MatchCount int
}

type SearchReplacePatchRequest struct {
	Path           string
	CurrentContent string
	Blocks         []SearchReplaceBlock
}

type SearchReplacePatchResult struct {
	Path               string
	CurrentContentHash string
	AppliedContentHash string
	AppliedContent     string
	Blocks             []SearchReplaceBlockResult
}

func ApplySearchReplacePatch(
	request SearchReplacePatchRequest,
) SearchReplacePatchResult {
	content := request.CurrentContent
	result := SearchReplacePatchResult{
		Path:               request.Path,
		CurrentContentHash: HashText(request.CurrentContent),
		Blocks:             make([]SearchReplaceBlockResult, 0, len(request.Blocks)),
	}

	for index, block := range request.Blocks {
		blockResult := validateSearchReplaceBlock(content, block, index)
		result.Blocks = append(result.Blocks, blockResult)

		if blockResult.Status != SearchReplaceStatusOK {
			result.AppliedContent = content
			result.AppliedContentHash = HashText(content)

			return result
		}

		content = strings.Replace(content, block.Search, block.Replace, 1)
	}

	result.AppliedContent = content
	result.AppliedContentHash = HashText(content)

	return result
}

func validateSearchReplaceBlock(
	content string,
	block SearchReplaceBlock,
	index int,
) SearchReplaceBlockResult {
	if block.Search == "" {
		return SearchReplaceBlockResult{
			Status: SearchReplaceStatusEmpty,
			Index:  index,
		}
	}

	matchCount := strings.Count(content, block.Search)
	switch {
	case matchCount == 0:
		return SearchReplaceBlockResult{
			Status:     SearchReplaceStatusMissing,
			Index:      index,
			MatchCount: matchCount,
		}
	case matchCount > 1:
		return SearchReplaceBlockResult{
			Status:     SearchReplaceStatusAmbiguous,
			Index:      index,
			MatchCount: matchCount,
		}
	default:
		return SearchReplaceBlockResult{
			Status:     SearchReplaceStatusOK,
			Index:      index,
			MatchCount: matchCount,
		}
	}
}
