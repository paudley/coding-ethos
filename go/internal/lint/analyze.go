// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	maxTraceAnalyzeRuns      = 250
	maxTraceTopCounts        = 10
	maxGuidanceCandidateRows = 10
	compactPathMinimumParts  = 2
)

type Analysis struct {
	Path               string              `json:"path"`
	Files              []string            `json:"files,omitempty"`
	TopChecks          []Count             `json:"top_checks"`
	TopCodes           []Count             `json:"top_codes"`
	UnmappedCodes      []Count             `json:"unmapped_codes,omitempty"`
	RepeatedPatterns   []Count             `json:"repeated_patterns"`
	TopEthosIDs        []Count             `json:"top_ethos_ids"`
	TopSkillIDs        []Count             `json:"top_skill_ids"`
	TopSkillHints      []Count             `json:"top_skill_hints"`
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
	SkillID  string `json:"skill_id,omitempty"`
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

func TracePathForID(cwd, traceID string) (string, error) {
	dir, err := DefaultTraceDir(cwd)
	if err != nil {
		return "", err
	}

	name := filepath.Base(strings.TrimSpace(traceID))
	if name == "" || name == "." || name == ".." || name != strings.TrimSpace(traceID) {
		return "", apperror.Wrapf(
			apperror.StaticError("invalid lint trace id %q"),
			"invalid lint trace id %q",
			traceID,
		)
	}

	return filepath.Join(dir, name), nil
}

func AnalyzeTraces(path string) (Analysis, error) {
	return AnalyzeTracesWithOptions(path, AnalysisOptions{})
}

func ReplayTrace(path string) (Result, error) {
	record, err := loadTraceRecord(path)
	if err != nil {
		return Result{}, err
	}

	return record.Result, nil
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

	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() > entries[right].Name()
	})

	counts := newAnalysisCounts()

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

		relevantSkillIDs := map[string]bool{}

		for _, finding := range record.Result.Findings {
			if !findingFailed(finding) || !findingRelevantToFiles(finding, files) {
				continue
			}

			analysis.Findings++

			counts.incrementFinding(finding, relevantSkillIDs)
		}

		counts.incrementSkillHints(record.Result.SkillHints, relevantSkillIDs, files)
	}

	counts.applyToAnalysis(&analysis, options)

	return analysis, nil
}

type analysisCounts struct {
	checks     map[string]int
	codes      map[string]int
	unmapped   map[string]int
	patterns   map[string]int
	ethos      map[string]int
	skills     map[string]int
	skillHints map[string]int
	candidates map[string]GuidanceCandidate
}

func newAnalysisCounts() analysisCounts {
	return analysisCounts{
		checks:     map[string]int{},
		codes:      map[string]int{},
		unmapped:   map[string]int{},
		patterns:   map[string]int{},
		ethos:      map[string]int{},
		skills:     map[string]int{},
		skillHints: map[string]int{},
		candidates: map[string]GuidanceCandidate{},
	}
}

func (counts analysisCounts) incrementFinding(
	finding Finding,
	relevantSkillIDs map[string]bool,
) {
	if finding.SkillID != "" {
		relevantSkillIDs[finding.SkillID] = true
	}

	incrementFindingCounts(
		finding,
		counts.checks,
		counts.codes,
		counts.unmapped,
		counts.patterns,
		counts.ethos,
		counts.skills,
		counts.candidates,
	)
}

func (counts analysisCounts) incrementSkillHints(
	hints []SkillHint,
	relevantSkillIDs map[string]bool,
	files []string,
) {
	incrementSkillHintCounts(hints, counts.skillHints, relevantSkillIDs, files)
}

func (counts analysisCounts) applyToAnalysis(
	analysis *Analysis,
	options AnalysisOptions,
) {
	analysis.TopChecks = topCountsLimit(counts.checks, options.MaxCounts)
	analysis.TopCodes = topCountsLimit(counts.codes, options.MaxCounts)
	analysis.UnmappedCodes = topCountsLimit(counts.unmapped, options.MaxCounts)
	analysis.RepeatedPatterns = topCountsLimit(counts.patterns, options.MaxCounts)
	analysis.TopEthosIDs = topCountsLimit(counts.ethos, options.MaxCounts)
	analysis.TopSkillIDs = topCountsLimit(counts.skills, options.MaxCounts)
	analysis.TopSkillHints = topCountsLimit(counts.skillHints, options.MaxCounts)
	analysis.GuidanceCandidates = topGuidanceCandidatesLimit(
		counts.candidates,
		options.MaxGuidanceCandidates,
	)
}

func loadTraceRecord(path string) (TraceRecord, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return TraceRecord{}, fmt.Errorf("open lint trace %q: %w", path, err)
	}
	defer file.Close()

	var record TraceRecord

	inlineErr0 := json.NewDecoder(file).Decode(&record)
	if inlineErr0 != nil {
		return TraceRecord{}, fmt.Errorf("decode lint trace %q: %w", path, inlineErr0)
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

func pathMatchesFileArea(path, pattern string) bool {
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
	skillCounts map[string]int,
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

	if finding.SkillID != "" {
		skillCounts[finding.SkillID]++
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
			SkillID:  finding.SkillID,
			Blocking: finding.Blocking,
		}
	}

	candidate.Count++
	candidates[candidateKey] = candidate
}

func incrementSkillHintCounts(
	hints []SkillHint,
	counts map[string]int,
	relevantSkillIDs map[string]bool,
	files []string,
) {
	for _, hint := range hints {
		skillID := strings.TrimSpace(hint.SkillID)
		if skillID == "" {
			continue
		}

		if len(files) > 0 && !relevantSkillIDs[skillID] {
			continue
		}

		counts[skillID]++
	}
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
	if len(parts) <= compactPathMinimumParts {
		return normalized
	}

	return strings.Join(parts[:2], "/") + "/..."
}

func topCountsLimit(counts map[string]int, limit int) []Count {
	if limit <= 0 {
		limit = maxTraceTopCounts
	}

	items := make([]Count, 0, len(counts))
	for key, count := range counts {
		items = append(items, Count{Key: key, Count: count})
	}

	sort.Slice(items, func(left, right int) bool {
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

	sort.Slice(items, func(left, right int) bool {
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

		err := encoder.Encode(analysis)
		if err != nil {
			return fmt.Errorf("encode lint analysis JSON: %w", err)
		}

		return nil
	case "toon":
		_, err := fmt.Fprintln(writer, FormatAnalysisTOON(analysis))
		if err != nil {
			return fmt.Errorf("write lint analysis TOON: %w", err)
		}

		return nil
	default:
		_, err := fmt.Fprintln(writer, FormatAnalysisHuman(analysis))
		if err != nil {
			return fmt.Errorf("write lint analysis text: %w", err)
		}

		return nil
	}
}

func FormatAnalysisHuman(analysis Analysis) string {
	lines := []string{
		"Lint trace analysis: " + analysis.Path,
		fmt.Sprintf(
			"Runs analyzed: %d of %d",
			analysis.RunsAnalyzed,
			analysis.RunsAvailable,
		),
		fmt.Sprintf("Findings: %d", analysis.Findings),
		"Top checks: " + countsHuman(analysis.TopChecks),
		"Top tool codes: " + countsHuman(analysis.TopCodes),
		"Unmapped tool codes: " + countsHuman(analysis.UnmappedCodes),
		"Repeated file/policy patterns: " + countsHuman(analysis.RepeatedPatterns),
		"Top ETHOS IDs: " + countsHuman(analysis.TopEthosIDs),
		"Top skill IDs: " + countsHuman(analysis.TopSkillIDs),
		"Top emitted skill hints: " + countsHuman(analysis.TopSkillHints),
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
			if candidate.SkillID != "" {
				lines = append(lines, "  skill: "+candidate.SkillID)
			}

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
	lines = appendAnalysisCountsTOON(
		lines,
		"repeated_patterns",
		analysis.RepeatedPatterns,
	)
	lines = appendAnalysisCountsTOON(lines, "top_ethos_ids", analysis.TopEthosIDs)
	lines = appendAnalysisCountsTOON(lines, "top_skill_ids", analysis.TopSkillIDs)
	lines = appendAnalysisCountsTOON(lines, "top_skill_hints", analysis.TopSkillHints)

	lines = append(
		lines,
		fmt.Sprintf(
			"guidance_candidates[%d]"+
				"{check_id,code,ethos_id,skill_id,count,blocking,message,advice}:",
			len(analysis.GuidanceCandidates),
		),
	)
	for _, candidate := range analysis.GuidanceCandidates {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%d,%t,%s,%s",
			toonCell(candidate.CheckID),
			toonCell(candidate.Code),
			toonCell(candidate.EthosID),
			toonCell(candidate.SkillID),
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
