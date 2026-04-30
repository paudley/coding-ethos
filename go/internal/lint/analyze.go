// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxTraceAnalyzeRuns      = 250
	maxTraceTopCounts        = 10
	maxGuidanceCandidateRows = 10
)

type Analysis struct {
	Path               string              `json:"path"`
	Files              []string            `json:"files,omitempty"`
	TopChecks          []Count             `json:"top_checks"`
	TopCodes           []Count             `json:"top_codes"`
	UnmappedCodes      []Count             `json:"unmapped_codes,omitempty"`
	RepeatedPatterns   []Count             `json:"repeated_patterns"`
	TopEthosIDs        []Count             `json:"top_ethos_ids"`
	GuidanceCandidates []GuidanceCandidate `json:"guidance_candidates"`
	RunsAnalyzed       int                 `json:"runs_analyzed"`
	RunsAvailable      int                 `json:"runs_available"`
	RunsSkipped        int                 `json:"runs_skipped"`
	Findings           int                 `json:"findings"`
}

type AnalysisOptions struct {
	Files                 []string
	MaxCounts             int
	MaxGuidanceCandidates int
}

type Count struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type GuidanceCandidate struct {
	Key      string `json:"key"`
	CheckID  string `json:"check_id"`
	Code     string `json:"code,omitempty"`
	Advice   string `json:"advice,omitempty"`
	Message  string `json:"message"`
	EthosID  string `json:"ethos_id,omitempty"`
	Count    int    `json:"count"`
	Blocking bool   `json:"blocking"`
}

func DefaultTraceDir(cwd string) (string, error) {
	root := cwd
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve lint trace root: %w", err)
		}
	}

	return filepath.Join(root, ".coding-ethos", "lint-runs"), nil
}

func AnalyzeTraces(path string) (Analysis, error) {
	return AnalyzeTracesWithOptions(path, AnalysisOptions{})
}

func AnalyzeTracesWithOptions(path string, options AnalysisOptions) (Analysis, error) {
	files := normalizeAnalysisFiles(options.Files)
	analysis := Analysis{Path: path, Files: files}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return analysis, nil
		}
		return analysis, fmt.Errorf("read lint trace dir %q: %w", path, err)
	}

	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].Name() > entries[right].Name()
	})

	checkCounts := map[string]int{}
	codeCounts := map[string]int{}
	unmappedCodeCounts := map[string]int{}
	patternCounts := map[string]int{}
	ethosCounts := map[string]int{}
	candidates := map[string]GuidanceCandidate{}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		analysis.RunsAvailable++
		if analysis.RunsAnalyzed >= maxTraceAnalyzeRuns {
			analysis.RunsSkipped++

			continue
		}

		record, err := loadTraceRecord(filepath.Join(path, entry.Name()))
		if err != nil {
			continue
		}
		analysis.RunsAnalyzed++

		for _, finding := range record.Result.Findings {
			if !findingFailed(finding) || !findingRelevantToFiles(finding, files) {
				continue
			}

			analysis.Findings++
			incrementFindingCounts(
				finding,
				checkCounts,
				codeCounts,
				unmappedCodeCounts,
				patternCounts,
				ethosCounts,
				candidates,
			)
		}
	}

	analysis.TopChecks = topCountsLimit(checkCounts, options.MaxCounts)
	analysis.TopCodes = topCountsLimit(codeCounts, options.MaxCounts)
	analysis.UnmappedCodes = topCountsLimit(unmappedCodeCounts, options.MaxCounts)
	analysis.RepeatedPatterns = topCountsLimit(patternCounts, options.MaxCounts)
	analysis.TopEthosIDs = topCountsLimit(ethosCounts, options.MaxCounts)
	analysis.GuidanceCandidates = topGuidanceCandidatesLimit(
		candidates,
		options.MaxGuidanceCandidates,
	)

	return analysis, nil
}

func loadTraceRecord(path string) (TraceRecord, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return TraceRecord{}, fmt.Errorf("open lint trace %q: %w", path, err)
	}
	defer file.Close()

	var record TraceRecord
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		return TraceRecord{}, fmt.Errorf("decode lint trace %q: %w", path, err)
	}

	return record, nil
}

func findingFailed(finding Finding) bool {
	return finding.Blocking || finding.Status == "fail"
}

func normalizeAnalysisFiles(files []string) []string {
	normalized := []string{}
	seen := map[string]bool{}
	for _, file := range files {
		cleaned := normalizeAnalysisFile(file)
		if cleaned == "" || seen[cleaned] {
			continue
		}

		normalized = append(normalized, cleaned)
		seen[cleaned] = true
	}

	return normalized
}

func normalizeAnalysisFile(file string) string {
	trimmed := strings.TrimSpace(file)
	if trimmed == "" {
		return ""
	}

	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(trimmed)), "./")
}

func findingRelevantToFiles(finding Finding, files []string) bool {
	if len(files) == 0 {
		return true
	}

	candidates := []string{finding.File}
	if finding.File == "" {
		candidates = append(candidates, finding.Files...)
	}
	for _, candidate := range candidates {
		if fileRelevantToFiles(candidate, files) {
			return true
		}
	}

	return false
}

func fileRelevantToFiles(candidate string, files []string) bool {
	normalized := normalizeAnalysisFile(candidate)
	if normalized == "" {
		return false
	}

	pattern := normalizedFilePattern(normalized)
	for _, file := range files {
		filePattern := normalizedFilePattern(file)
		if normalized == file ||
			strings.HasSuffix(normalized, "/"+file) ||
			strings.HasSuffix(file, "/"+normalized) ||
			pattern == filePattern ||
			pathMatchesFileArea(normalized, filePattern) ||
			pathMatchesFileArea(file, pattern) {
			return true
		}
	}

	return false
}

func pathMatchesFileArea(path string, pattern string) bool {
	prefix := strings.TrimSuffix(pattern, "/...")
	if prefix == "" || prefix == pattern {
		return false
	}

	return strings.HasPrefix(path, prefix+"/") || strings.Contains(path, "/"+prefix+"/")
}

func incrementFindingCounts(
	finding Finding,
	checkCounts map[string]int,
	codeCounts map[string]int,
	unmappedCodeCounts map[string]int,
	patternCounts map[string]int,
	ethosCounts map[string]int,
	candidates map[string]GuidanceCandidate,
) {
	checkID := firstNonEmpty(finding.CheckID, finding.PolicyID, "unknown")
	checkCounts[checkID]++

	if finding.SourceTool != "" && finding.Code != "" {
		toolCode := finding.SourceTool + ":" + finding.Code
		codeCounts[toolCode]++
		if findingUnmapped(finding) {
			unmappedCodeCounts[toolCode]++
		}
	}

	pattern := repeatedPatternKey(finding)
	if pattern != "" {
		patternCounts[pattern]++
	}

	for _, ethosID := range finding.EthosIDs {
		if ethosID != "" {
			ethosCounts[ethosID]++
		}
	}

	candidateKey := guidanceCandidateKey(finding)
	candidate := candidates[candidateKey]
	if candidate.Key == "" {
		candidate = GuidanceCandidate{
			Key:      candidateKey,
			CheckID:  checkID,
			Code:     finding.Code,
			Advice:   finding.Advice,
			Message:  finding.Message,
			EthosID:  firstString(finding.EthosIDs),
			Blocking: finding.Blocking,
		}
	}
	candidate.Count++
	candidates[candidateKey] = candidate
}

func findingUnmapped(finding Finding) bool {
	return finding.PolicyID == "" && len(finding.EthosIDs) == 0
}

func repeatedPatternKey(finding Finding) string {
	checkID := firstNonEmpty(finding.CheckID, finding.PolicyID)
	if checkID == "" {
		return ""
	}

	filePart := normalizedFilePattern(finding.File)
	if filePart == "" && len(finding.Files) > 0 {
		filePart = normalizedFilePattern(finding.Files[0])
	}
	if filePart == "" {
		filePart = "<repo>"
	}

	return checkID + "|" + filePart
}

func guidanceCandidateKey(finding Finding) string {
	return strings.Join([]string{
		firstNonEmpty(finding.CheckID, finding.PolicyID, "unknown"),
		firstNonEmpty(finding.SourceTool, "policy"),
		finding.Code,
		finding.Message,
	}, "|")
}

func normalizedFilePattern(path string) string {
	if path == "" {
		return ""
	}

	normalized := filepath.ToSlash(path)
	parts := strings.Split(normalized, "/")
	if len(parts) <= 2 {
		return normalized
	}

	return strings.Join(parts[:2], "/") + "/..."
}

func topCounts(counts map[string]int) []Count {
	return topCountsLimit(counts, maxTraceTopCounts)
}

func topCountsLimit(counts map[string]int, limit int) []Count {
	if limit <= 0 {
		limit = maxTraceTopCounts
	}

	items := make([]Count, 0, len(counts))
	for key, count := range counts {
		items = append(items, Count{Key: key, Count: count})
	}

	sort.Slice(items, func(left int, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Key < items[right].Key
	})

	if len(items) > limit {
		return items[:limit]
	}

	return items
}

func topGuidanceCandidates(
	candidates map[string]GuidanceCandidate,
) []GuidanceCandidate {
	return topGuidanceCandidatesLimit(candidates, maxGuidanceCandidateRows)
}

func topGuidanceCandidatesLimit(
	candidates map[string]GuidanceCandidate,
	limit int,
) []GuidanceCandidate {
	if limit <= 0 {
		limit = maxGuidanceCandidateRows
	}

	items := make([]GuidanceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate)
	}

	sort.Slice(items, func(left int, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Key < items[right].Key
	})

	if len(items) > limit {
		return items[:limit]
	}

	return items
}

func firstString(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func EncodeAnalysis(writer io.Writer, analysis Analysis, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")

		return encoder.Encode(analysis)
	case "toon":
		_, err := fmt.Fprintln(writer, FormatAnalysisTOON(analysis))

		return err
	default:
		_, err := fmt.Fprintln(writer, FormatAnalysisHuman(analysis))

		return err
	}
}

func FormatAnalysisHuman(analysis Analysis) string {
	lines := []string{
		"Lint trace analysis: " + analysis.Path,
		fmt.Sprintf("Runs analyzed: %d of %d", analysis.RunsAnalyzed, analysis.RunsAvailable),
		fmt.Sprintf("Findings: %d", analysis.Findings),
		"Top checks: " + countsHuman(analysis.TopChecks),
		"Top tool codes: " + countsHuman(analysis.TopCodes),
		"Unmapped tool codes: " + countsHuman(analysis.UnmappedCodes),
		"Repeated file/policy patterns: " + countsHuman(analysis.RepeatedPatterns),
		"Top ETHOS IDs: " + countsHuman(analysis.TopEthosIDs),
	}
	if len(analysis.GuidanceCandidates) > 0 {
		lines = append(lines, "Guidance candidates:")
		for _, candidate := range analysis.GuidanceCandidates {
			lines = append(lines, fmt.Sprintf(
				"- %s count=%d code=%s ethos=%s",
				candidate.CheckID,
				candidate.Count,
				firstNonEmpty(candidate.Code, "none"),
				firstNonEmpty(candidate.EthosID, "none"),
			))
			lines = append(lines, "  message: "+candidate.Message)
			if candidate.Advice != "" {
				lines = append(lines, "  advice: "+candidate.Advice)
			}
		}
	}

	return strings.Join(lines, "\n")
}

func countsHuman(counts []Count) string {
	if len(counts) == 0 {
		return "none"
	}

	items := make([]string, 0, len(counts))
	for _, count := range counts {
		items = append(items, fmt.Sprintf("%s=%d", count.Key, count.Count))
	}

	return strings.Join(items, ", ")
}

func FormatAnalysisTOON(analysis Analysis) string {
	lines := []string{
		"format: toon",
		"tool: policy-lint",
		"operation: analyze-log",
		"path: " + toonCell(analysis.Path),
		fmt.Sprintf("runs_analyzed: %d", analysis.RunsAnalyzed),
		fmt.Sprintf("runs_available: %d", analysis.RunsAvailable),
		fmt.Sprintf("runs_skipped: %d", analysis.RunsSkipped),
		fmt.Sprintf("findings: %d", analysis.Findings),
	}
	if len(analysis.Files) > 0 {
		lines = append(lines, fmt.Sprintf("files[%d]{path}:", len(analysis.Files)))
		for _, file := range analysis.Files {
			lines = append(lines, "  "+toonCell(file))
		}
	}
	lines = appendAnalysisCountsTOON(lines, "top_checks", analysis.TopChecks)
	lines = appendAnalysisCountsTOON(lines, "top_codes", analysis.TopCodes)
	lines = appendAnalysisCountsTOON(lines, "unmapped_codes", analysis.UnmappedCodes)
	lines = appendAnalysisCountsTOON(lines, "repeated_patterns", analysis.RepeatedPatterns)
	lines = appendAnalysisCountsTOON(lines, "top_ethos_ids", analysis.TopEthosIDs)
	lines = append(
		lines,
		fmt.Sprintf(
			"guidance_candidates[%d]{check_id,code,ethos_id,count,blocking,message,advice}:",
			len(analysis.GuidanceCandidates),
		),
	)
	for _, candidate := range analysis.GuidanceCandidates {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%d,%t,%s,%s",
			toonCell(candidate.CheckID),
			toonCell(candidate.Code),
			toonCell(candidate.EthosID),
			candidate.Count,
			candidate.Blocking,
			toonCell(candidate.Message),
			toonCell(candidate.Advice),
		))
	}

	return strings.Join(lines, "\n")
}

func appendAnalysisCountsTOON(lines []string, name string, counts []Count) []string {
	lines = append(lines, fmt.Sprintf("%s[%d]{key,count}:", name, len(counts)))
	for _, count := range counts {
		lines = append(lines, fmt.Sprintf("  %s,%d", toonCell(count.Key), count.Count))
	}

	return lines
}
