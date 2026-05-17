// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

type FindingInput struct {
	SymbolKind   string   `json:"symbol_kind"`
	ChunkHash    string   `json:"chunk_hash"`
	Message      string   `json:"message"`
	File         string   `json:"file"`
	Language     string   `json:"language"`
	SymbolName   string   `json:"symbol_name"`
	Code         string   `json:"code"`
	SkillID      string   `json:"skill_id"`
	Tool         string   `json:"tool"`
	PolicyID     string   `json:"policy_id"`
	Severity     string   `json:"severity"`
	PrincipleIDs []string `json:"principle_ids"`
	Line         int64    `json:"line"`
	LineCount    int64    `json:"line_count"`
	ChangedLines int64    `json:"changed_lines"`
}

type SourceInput struct {
	Path               string `json:"path"`
	Language           string `json:"language"`
	SymbolName         string `json:"symbol_name"`
	SymbolKind         string `json:"symbol_kind"`
	ChunkHash          string `json:"chunk_hash"`
	LineCount          int64  `json:"line_count"`
	ChangedLines       int64  `json:"changed_lines"`
	PriorFailures      int64  `json:"prior_failures"`
	RecentRemediations int64  `json:"recent_remediations"`
	HasNearbyTest      bool   `json:"has_nearby_test"`
	HasDocChunk        bool   `json:"has_doc_chunk"`
}

type FindingActivation struct {
	Tool         string
	Code         string
	Message      string
	File         string
	Language     string
	SymbolName   string
	SymbolKind   string
	ChunkHash    string
	Severity     string
	PolicyID     string
	SkillID      string
	PrincipleIDs []string
	Column       int
	Line         int
	LineCount    int
	ChangedLines int
}

type SourceActivation struct {
	Path               string
	Language           string
	SymbolName         string
	SymbolKind         string
	ChunkHash          string
	LineCount          int
	ChangedLines       int
	PriorFailures      int
	RecentRemediations int
	HasNearbyTest      bool
	HasDocChunk        bool
}

func findingInput(finding *FindingActivation) FindingInput {
	if finding == nil {
		return FindingInput{PrincipleIDs: []string{}}
	}

	return FindingInput{
		Tool:         finding.Tool,
		Code:         finding.Code,
		Message:      finding.Message,
		File:         cleanInputFile(finding.File),
		Language:     finding.Language,
		SymbolName:   finding.SymbolName,
		SymbolKind:   finding.SymbolKind,
		ChunkHash:    finding.ChunkHash,
		Line:         int64(finding.Line),
		LineCount:    int64(finding.LineCount),
		ChangedLines: int64(finding.ChangedLines),
		Severity:     finding.Severity,
		PolicyID:     finding.PolicyID,
		SkillID:      finding.SkillID,
		PrincipleIDs: append([]string(nil), finding.PrincipleIDs...),
	}
}

func sourceInput(
	source SourceActivation,
	finding *FindingActivation,
	primaryPath PathInput,
) SourceInput {
	result := SourceInput{
		Path:               cleanInputFile(source.Path),
		Language:           source.Language,
		SymbolName:         source.SymbolName,
		SymbolKind:         source.SymbolKind,
		ChunkHash:          source.ChunkHash,
		LineCount:          int64(source.LineCount),
		ChangedLines:       int64(source.ChangedLines),
		PriorFailures:      int64(source.PriorFailures),
		RecentRemediations: int64(source.RecentRemediations),
		HasNearbyTest:      source.HasNearbyTest,
		HasDocChunk:        source.HasDocChunk,
	}
	if result.Path == "" {
		result.Path = primaryPath.File
	}

	if finding == nil {
		return result
	}

	if result.Path == "" {
		result.Path = cleanInputFile(finding.File)
	}

	if result.Language == "" {
		result.Language = finding.Language
	}

	if result.SymbolName == "" {
		result.SymbolName = finding.SymbolName
	}

	if result.SymbolKind == "" {
		result.SymbolKind = finding.SymbolKind
	}

	if result.ChunkHash == "" {
		result.ChunkHash = finding.ChunkHash
	}

	if result.LineCount == 0 {
		result.LineCount = int64(finding.LineCount)
	}

	if result.ChangedLines == 0 {
		result.ChangedLines = int64(finding.ChangedLines)
	}

	return result
}
