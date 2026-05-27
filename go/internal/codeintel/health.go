// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const (
	defaultHealthLimit         = 20
	defaultHealthTrendLimit    = 20
	largeFunctionLineThreshold = 60
	largeFileLineThreshold     = 400
	deepNestingThreshold       = 4
	complexConditionalBranches = 5
	complexFunctionBranches    = 10
	brainMethodLineThreshold   = 90
	brainMethodBranchThreshold = 8
	healthHotspotThreshold     = 25
	healthAuthorRiskCount      = 3
	healthFloatPrecision       = 10
	healthBaseScore            = 100
	healthPercentageScale      = 100
	healthRepeatedFailureLimit = 1000
	healthLowCoverageThreshold = 50
	healthMinClonePathCount    = 2
	healthIndentSpaces         = 4
	healthEffortSmoothing      = 2
	healthMinEffortScore       = 1
	healthLargeFileScore       = 8
	healthLargeFunctionScore   = 10
	healthNestingScore         = 7
	healthConditionalScore     = 7
	healthComplexFuncScore     = 12
	healthBrainMethodScore     = 16
	healthCloneScore           = 6
	healthHotspotScore         = 12
	healthOwnershipScore       = 8
	healthCouplingScore        = 6
	healthUntestedHotspotScore = 14
	healthFailureTraceScore    = 4
)

var errLCOVSourcePathRequired = apperror.StaticError("LCOV source path is required")

var healthBranchTokenPattern = regexp.MustCompile(
	`\b(if|else|elif|for|while|case)\b|&&|\|\||\?`,
)

type CodeHealthQuery struct {
	Root     string `json:"root,omitempty"`
	Path     string `json:"path,omitempty"`
	GitHead  string `json:"git_head,omitempty"`
	LCOVPath string `json:"lcov_path,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Trend    int    `json:"trend,omitempty"`
	Refresh  bool   `json:"refresh,omitempty"`
}

type CodeHealthSnapshot struct {
	ID               string             `json:"id"`
	RepoRoot         string             `json:"repo_root"`
	GitHead          string             `json:"git_head,omitempty"`
	IndexedAtUTC     string             `json:"indexed_at_utc"`
	Targets          []CodeHealthTarget `json:"targets"`
	Trend            []CodeHealthTrend  `json:"trend,omitempty"`
	TotalHealthScore float64            `json:"total_health_score"`
	TargetCount      int                `json:"target_count"`
}

type CodeHealthTrend struct {
	SnapshotID       string  `json:"snapshot_id"`
	GitHead          string  `json:"git_head,omitempty"`
	IndexedAtUTC     string  `json:"indexed_at_utc"`
	TotalHealthScore float64 `json:"total_health_score"`
	TargetCount      int     `json:"target_count"`
}

type CodeHealthTarget struct {
	Path          string               `json:"path"`
	Language      string               `json:"language,omitempty"`
	Evidence      []CodeHealthEvidence `json:"evidence"`
	HealthScore   float64              `json:"health_score"`
	ImpactScore   float64              `json:"impact_score"`
	EffortScore   float64              `json:"effort_score"`
	PriorityScore float64              `json:"priority_score"`
	Rank          int                  `json:"rank"`
}

type CodeHealthEvidence struct {
	Biomarker  string  `json:"biomarker"`
	Severity   string  `json:"severity"`
	Message    string  `json:"message"`
	EvidenceID string  `json:"evidence_id,omitempty"`
	ScoreDelta float64 `json:"score_delta"`
	Line       int     `json:"line,omitempty"`
}

type CodeHealthCoverage struct {
	Path                string  `json:"path"`
	SourcePath          string  `json:"source_path"`
	ImportedAtUTC       string  `json:"imported_at_utc"`
	FoundLines          int     `json:"found_lines"`
	CoveredLines        int     `json:"covered_lines"`
	LineCoveragePercent float64 `json:"line_coverage_percent"`
}

type LCOVImportSummary struct {
	SourcePath string               `json:"source_path"`
	ImportedAt string               `json:"imported_at_utc"`
	Coverage   []CodeHealthCoverage `json:"coverage"`
	Files      int                  `json:"files"`
}

type CodeHealthSettings struct {
	Biomarkers    map[string]CodeHealthBiomarkerSetting `json:"biomarkers,omitempty"`
	PathOverrides []CodeHealthPathSetting               `json:"path_overrides,omitempty"`
}

type CodeHealthBiomarkerSetting struct {
	Enabled bool    `json:"enabled"`
	Weight  float64 `json:"weight"`
}

type CodeHealthPathSetting struct {
	Weights            map[string]float64 `json:"weights,omitempty"`
	Glob               string             `json:"glob"`
	DisabledBiomarkers []string           `json:"disabled_biomarkers,omitempty"`
	globPattern        *regexp.Regexp
}

type healthFacts struct {
	files      map[string]CodeFile
	gitSignals map[string]GitFileSignal
	cochanges  map[string][]GitCoChange
	failures   map[string][]RepeatedFailure
	coverage   map[string]CodeHealthCoverage
	chunks     []CodeChunk
}

func (store *Store) CodeHealth(
	ctx context.Context,
	query CodeHealthQuery,
) (CodeHealthSnapshot, error) {
	query.GitHead = codeHealthGitHead(ctx, query.Root, query.GitHead)

	if query.Refresh {
		return store.RefreshCodeHealth(ctx, query)
	}

	snapshot, found, err := store.latestCodeHealth(ctx, query)
	if err != nil {
		return CodeHealthSnapshot{}, err
	}

	if found {
		return snapshot, nil
	}

	return store.RefreshCodeHealth(ctx, query)
}

func (store *Store) StoredCodeHealth(
	ctx context.Context,
	query CodeHealthQuery,
) (CodeHealthSnapshot, bool, error) {
	query.GitHead = codeHealthGitHead(ctx, query.Root, query.GitHead)

	return store.latestCodeHealth(ctx, query)
}

func codeHealthGitHead(ctx context.Context, root, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}

	head, err := currentGitSignalHead(ctx, root)
	if err != nil {
		return ""
	}

	return head
}

func (store *Store) RefreshCodeHealth(
	ctx context.Context,
	query CodeHealthQuery,
) (CodeHealthSnapshot, error) {
	query.GitHead = codeHealthGitHead(ctx, query.Root, query.GitHead)

	settings, err := LoadCodeHealthSettings(query.Root)
	if err != nil {
		return CodeHealthSnapshot{}, err
	}

	if query.LCOVPath != "" {
		_, err = store.ImportLCOV(ctx, query.Root, query.LCOVPath, time.Now().UTC())
		if err != nil {
			return CodeHealthSnapshot{}, err
		}
	}

	facts, err := store.loadHealthFacts(ctx)
	if err != nil {
		return CodeHealthSnapshot{}, err
	}

	snapshotQuery := query
	snapshotQuery.Path = ""
	snapshotQuery.Limit = 0

	snapshot := buildCodeHealthSnapshot(snapshotQuery, facts, settings, time.Now().UTC())

	err = store.persistCodeHealth(ctx, snapshot)
	if err != nil {
		return CodeHealthSnapshot{}, err
	}

	return store.CodeHealth(ctx, CodeHealthQuery{
		Root:    query.Root,
		Path:    query.Path,
		Limit:   query.Limit,
		Trend:   query.Trend,
		GitHead: query.GitHead,
	})
}

func LoadCodeHealthSettings(root string) (CodeHealthSettings, error) {
	settings := CodeHealthSettings{Biomarkers: defaultHealthBiomarkers()}
	if strings.TrimSpace(root) == "" {
		return settings, nil
	}

	for _, name := range configdata.RepoConfigCandidates() {
		path := filepath.Join(root, name)

		config, err := configdata.LoadYAMLMap(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return CodeHealthSettings{}, fmt.Errorf(
				"load code health config %s: %w",
				path,
				err,
			)
		}

		applyCodeHealthConfig(&settings, config)
	}

	return settings, nil
}

func (store *Store) ImportLCOV(
	ctx context.Context,
	root string,
	path string,
	now time.Time,
) (LCOVImportSummary, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return LCOVImportSummary{}, errLCOVSourcePathRequired
	}

	file, err := os.Open(filepath.Clean(cleanPath))
	if err != nil {
		return LCOVImportSummary{}, fmt.Errorf("open LCOV %s: %w", cleanPath, err)
	}
	defer file.Close()

	coverage, err := parseLCOV(file, root, cleanPath, now.UTC().Format(time.RFC3339))
	if err != nil {
		return LCOVImportSummary{}, err
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return LCOVImportSummary{}, fmt.Errorf("begin LCOV import: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	for _, record := range coverage {
		_, err = transaction.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO code_health_coverage(
				path, source_path, imported_at_utc, found_lines, covered_lines,
				line_coverage_percent
			) VALUES (?, ?, ?, ?, ?, ?)`,
			record.Path,
			record.SourcePath,
			record.ImportedAtUTC,
			record.FoundLines,
			record.CoveredLines,
			record.LineCoveragePercent,
		)
		if err != nil {
			return LCOVImportSummary{}, fmt.Errorf("insert LCOV coverage: %w", err)
		}
	}

	err = transaction.Commit()
	if err != nil {
		return LCOVImportSummary{}, fmt.Errorf("commit LCOV import: %w", err)
	}

	return LCOVImportSummary{
		SourcePath: cleanPath,
		ImportedAt: now.UTC().Format(time.RFC3339),
		Files:      len(coverage),
		Coverage:   coverage,
	}, nil
}

func (store *Store) loadHealthFacts(ctx context.Context) (healthFacts, error) {
	files, err := store.CodeFilesByPath(ctx)
	if err != nil {
		return healthFacts{}, err
	}

	chunks, err := store.healthChunks(ctx)
	if err != nil {
		return healthFacts{}, err
	}

	signals, err := store.GitSignals(ctx, GitSignalQuery{Limit: len(files) + 1})
	if err != nil {
		return healthFacts{}, err
	}

	failures, err := store.RepeatedFailures(
		ctx,
		RepeatedFailureQuery{Limit: healthRepeatedFailureLimit},
	)
	if err != nil {
		return healthFacts{}, err
	}

	cochanges, err := store.healthCochanges(ctx, signals)
	if err != nil {
		return healthFacts{}, err
	}

	coverage, err := store.healthCoverage(ctx)
	if err != nil {
		return healthFacts{}, err
	}

	return healthFacts{
		files:      files,
		chunks:     chunks,
		gitSignals: gitSignalMap(signals),
		cochanges:  cochanges,
		failures:   repeatedFailureMap(failures),
		coverage:   coverage,
	}, nil
}

func (store *Store) healthChunks(ctx context.Context) ([]CodeChunk, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT chunk_id, code_chunks.path, code_chunks.language, node_kind,
			symbol_kind, symbol_name, symbol_path,
			COALESCE(parent_symbol_path, ''), parent_chunk_id,
			start_byte, end_byte, start_line, end_line, code_chunks.content_hash,
			COALESCE(normalized_hash, ''), search_text, raw_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY code_chunks.path, start_line, start_byte`,
	)
	if err != nil {
		return nil, fmt.Errorf("query health code chunks: %w", err)
	}
	defer rows.Close()

	chunks := []CodeChunk{}

	for rows.Next() {
		var chunk CodeChunk

		err = rows.Scan(
			&chunk.ID,
			&chunk.Path,
			&chunk.Language,
			&chunk.NodeKind,
			&chunk.SymbolKind,
			&chunk.SymbolName,
			&chunk.SymbolPath,
			&chunk.ParentSymbolPath,
			&chunk.ParentChunkID,
			&chunk.StartByte,
			&chunk.EndByte,
			&chunk.StartLine,
			&chunk.EndLine,
			&chunk.ContentHash,
			&chunk.NormalizedHash,
			&chunk.SearchText,
			&chunk.RawText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health code chunk: %w", err)
		}

		chunks = append(chunks, chunk)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate health code chunks: %w", err)
	}

	return chunks, nil
}

func (store *Store) healthCochanges(
	ctx context.Context,
	signals []GitFileSignal,
) (map[string][]GitCoChange, error) {
	cochanges := map[string][]GitCoChange{}
	if len(signals) == 0 {
		return cochanges, nil
	}

	signalPaths := map[string]bool{}
	placeholders := make([]string, 0, len(signals))
	args := []any{defaultGitSignalCoChangeLimit}

	for _, signal := range signals {
		signalPaths[signal.Path] = true

		placeholders = append(placeholders, "?")
		args = append(args, signal.Path)
	}

	// #nosec G202 -- IN-list placeholders are generated from indexed paths;
	// path values remain bound parameters.
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, related_path, cochange_count, COALESCE(last_seen_utc, ''),
			hidden_coupling
		FROM (
			SELECT path, related_path, cochange_count, last_seen_utc, hidden_coupling,
				ROW_NUMBER() OVER (
					PARTITION BY path
					ORDER BY cochange_count DESC, hidden_coupling DESC, related_path
				) AS rank
			FROM git_cochanges
		)
		WHERE rank <= ? AND path IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY path, rank`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query health co-changes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cochange GitCoChange

		var hidden int

		err = rows.Scan(
			&cochange.Path,
			&cochange.RelatedPath,
			&cochange.Count,
			&cochange.LastSeenUTC,
			&hidden,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health co-change: %w", err)
		}

		if signalPaths[cochange.Path] {
			cochange.HiddenCoupling = hidden != 0
			cochanges[cochange.Path] = append(cochanges[cochange.Path], cochange)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate health co-changes: %w", err)
	}

	return cochanges, nil
}

func (store *Store) healthCoverage(
	ctx context.Context,
) (map[string]CodeHealthCoverage, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, source_path, imported_at_utc, found_lines, covered_lines,
			line_coverage_percent
		FROM code_health_coverage`,
	)
	if err != nil {
		return nil, fmt.Errorf("query code health coverage: %w", err)
	}
	defer rows.Close()

	coverage := map[string]CodeHealthCoverage{}

	for rows.Next() {
		var record CodeHealthCoverage

		err = rows.Scan(
			&record.Path,
			&record.SourcePath,
			&record.ImportedAtUTC,
			&record.FoundLines,
			&record.CoveredLines,
			&record.LineCoveragePercent,
		)
		if err != nil {
			return coverage, fmt.Errorf("scan code health coverage: %w", err)
		}

		coverage[record.Path] = record
	}

	err = rows.Err()
	if err != nil {
		return coverage, fmt.Errorf("iterate code health coverage: %w", err)
	}

	return coverage, nil
}

func buildCodeHealthSnapshot(
	query CodeHealthQuery,
	facts healthFacts,
	settings CodeHealthSettings,
	now time.Time,
) CodeHealthSnapshot {
	targets := make([]CodeHealthTarget, 0, len(facts.files))
	chunksByPath := chunksByPath(facts.chunks)
	cloneCounts := cloneCountsByPath(facts.chunks)
	totalFiles := 0
	totalScore := 0.0

	for _, path := range sortedCodeFilePaths(facts.files) {
		file := facts.files[path]
		if file.DeletedAtUTC != "" || !healthPathMatches(path, query.Path) {
			continue
		}

		totalFiles++
		target := scoreHealthTarget(
			file,
			chunksByPath[path],
			cloneCounts[path],
			facts.gitSignals[path],
			facts.cochanges[path],
			facts.failures[path],
			facts.coverage[path],
			settings,
		)

		totalScore += target.HealthScore
		if len(target.Evidence) != 0 {
			targets = append(targets, target)
		}
	}

	slices.SortFunc(targets, compareHealthTargets)

	limit := query.Limit
	if limit <= 0 {
		limit = defaultHealthLimit
	}

	for index := range targets {
		targets[index].Rank = index + 1
	}

	targetCount := len(targets)
	if len(targets) > limit {
		targets = targets[:limit]
	}

	return CodeHealthSnapshot{
		ID: stableID(
			"code-health",
			query.Root,
			query.GitHead,
			now.Format(time.RFC3339Nano),
		),
		RepoRoot:         query.Root,
		GitHead:          query.GitHead,
		IndexedAtUTC:     now.Format(time.RFC3339Nano),
		TotalHealthScore: repoHealthScore(totalScore, totalFiles),
		TargetCount:      targetCount,
		Targets:          targets,
	}
}

func sortedCodeFilePaths(files map[string]CodeFile) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}

	slices.Sort(paths)

	return paths
}

func scoreHealthTarget(
	file CodeFile,
	chunks []CodeChunk,
	cloneCount int,
	gitSignal GitFileSignal,
	cochanges []GitCoChange,
	failures []RepeatedFailure,
	coverage CodeHealthCoverage,
	settings CodeHealthSettings,
) CodeHealthTarget {
	evidence := weightedHealthEvidenceItems(file.Path, settings,
		fileHealthEvidence(file),
		chunkHealthEvidence(chunks),
		cloneHealthEvidence(file, cloneCount),
		gitHealthEvidence(file, gitSignal),
		cochangeHealthEvidence(file, cochanges),
		coverageHealthEvidence(file, gitSignal, coverage),
		failureHealthEvidence(file, failures),
	)
	impact := impactScore(evidence)
	effort := effortScore(file, chunks)

	return CodeHealthTarget{
		Path:          file.Path,
		Language:      file.Language,
		HealthScore:   roundedFloat(math.Max(0, healthBaseScore-impact)),
		ImpactScore:   roundedFloat(impact),
		EffortScore:   roundedFloat(effort),
		PriorityScore: roundedFloat(impact / effort),
		Evidence:      evidence,
	}
}

func weightedHealthEvidenceItems(
	path string,
	settings CodeHealthSettings,
	groups ...[]CodeHealthEvidence,
) []CodeHealthEvidence {
	evidence := []CodeHealthEvidence{}

	for _, group := range groups {
		for _, item := range group {
			weighted, ok := weightedHealthEvidence(path, item, settings)
			if ok {
				evidence = append(evidence, weighted)
			}
		}
	}

	return evidence
}

func fileHealthEvidence(file CodeFile) []CodeHealthEvidence {
	if file.LineCount < largeFileLineThreshold {
		return nil
	}

	return []CodeHealthEvidence{healthEvidence("large_file", "medium",
		healthLargeFileScore, fmt.Sprintf(
			"%s has %d indexed lines",
			file.Path,
			file.LineCount,
		), file.Path, 1)}
}

func chunkHealthEvidence(chunks []CodeChunk) []CodeHealthEvidence {
	evidence := make([]CodeHealthEvidence, 0, len(chunks))

	for _, chunk := range chunks {
		evidence = append(evidence, singleChunkHealthEvidence(chunk)...)
	}

	return evidence
}

func singleChunkHealthEvidence(chunk CodeChunk) []CodeHealthEvidence {
	lineCount := chunk.EndLine - chunk.StartLine + 1
	branchCount := branchTokenCount(chunk.RawText)
	cyclomaticEstimate := branchCount + 1
	depth := indentationDepth(chunk.RawText)
	evidence := []CodeHealthEvidence{}

	if isCallableChunk(chunk) && lineCount >= largeFunctionLineThreshold {
		evidence = append(evidence, healthEvidence("large_function", "medium",
			healthLargeFunctionScore, fmt.Sprintf(
				"%s spans %d lines",
				healthChunkName(chunk),
				lineCount,
			), chunk.ID, chunk.StartLine))
	}

	if depth >= deepNestingThreshold {
		evidence = append(evidence, healthEvidence("deep_nesting", "medium",
			healthNestingScore, fmt.Sprintf(
				"%s reaches nesting depth %d",
				healthChunkName(chunk),
				depth,
			), chunk.ID, chunk.StartLine))
	}

	if branchCount >= complexConditionalBranches {
		evidence = append(evidence, healthEvidence("complex_conditional", "medium",
			healthConditionalScore, fmt.Sprintf(
				"%s has %d conditional branches",
				healthChunkName(chunk),
				branchCount,
			), chunk.ID, chunk.StartLine))
	}

	if isCallableChunk(chunk) && cyclomaticEstimate >= complexFunctionBranches {
		evidence = append(evidence, healthEvidence("complex_function", "high",
			healthComplexFuncScore, fmt.Sprintf(
				"%s has estimated cyclomatic complexity %d",
				healthChunkName(chunk),
				cyclomaticEstimate,
			), chunk.ID, chunk.StartLine))
	}

	if isBrainMethodCandidate(chunk, lineCount, branchCount) {
		evidence = append(evidence, healthEvidence("brain_method_candidate", "high",
			healthBrainMethodScore, fmt.Sprintf(
				"%s combines %d lines with %d branches",
				healthChunkName(chunk),
				lineCount,
				branchCount,
			), chunk.ID, chunk.StartLine))
	}

	return evidence
}

func isBrainMethodCandidate(
	chunk CodeChunk,
	lineCount int,
	branchCount int,
) bool {
	return isCallableChunk(chunk) &&
		lineCount >= brainMethodLineThreshold &&
		branchCount >= brainMethodBranchThreshold
}

func cloneHealthEvidence(file CodeFile, cloneCount int) []CodeHealthEvidence {
	if cloneCount == 0 {
		return nil
	}

	return []CodeHealthEvidence{healthEvidence(
		"structural_clone",
		"medium",
		float64(healthCloneScore*cloneCount),
		fmt.Sprintf(
			"%s has %d exact normalized structural clone matches",
			file.Path,
			cloneCount,
		),
		file.Path,
		1,
	)}
}

func gitHealthEvidence(file CodeFile, signal GitFileSignal) []CodeHealthEvidence {
	evidence := []CodeHealthEvidence{}

	if signal.HotspotScore >= healthHotspotThreshold {
		evidence = append(evidence, healthEvidence("git_hotspot", "high",
			healthHotspotScore, fmt.Sprintf(
				"%s hotspot score is %.1f",
				file.Path,
				signal.HotspotScore,
			), file.Path, 1))
	}

	if signal.AuthorCount >= healthAuthorRiskCount {
		evidence = append(evidence, healthEvidence("ownership_risk", "medium",
			healthOwnershipScore, fmt.Sprintf(
				"%s has %d contributing authors",
				file.Path,
				signal.AuthorCount,
			), file.Path, 1))
	}

	return evidence
}

func cochangeHealthEvidence(
	file CodeFile,
	cochanges []GitCoChange,
) []CodeHealthEvidence {
	evidence := make([]CodeHealthEvidence, 0, len(cochanges))

	for _, cochange := range cochanges {
		if cochange.HiddenCoupling {
			evidence = append(evidence, healthEvidence("hidden_coupling", "medium",
				healthCouplingScore, fmt.Sprintf(
					"%s repeatedly co-changes with %s",
					file.Path,
					cochange.RelatedPath,
				), cochange.RelatedPath, 1))
		}
	}

	return evidence
}

func coverageHealthEvidence(
	file CodeFile,
	gitSignal GitFileSignal,
	coverage CodeHealthCoverage,
) []CodeHealthEvidence {
	if gitSignal.HotspotScore < healthHotspotThreshold ||
		isTestPath(file.Path) ||
		coverageHasEnoughEvidence(coverage) {
		return nil
	}

	return []CodeHealthEvidence{healthEvidence(
		"hotspot_without_test_evidence",
		"high",
		healthUntestedHotspotScore,
		file.Path+" is a hotspot without nearby LCOV test coverage evidence",
		file.Path,
		1,
	)}
}

func coverageHasEnoughEvidence(coverage CodeHealthCoverage) bool {
	return coverage.FoundLines != 0 &&
		coverage.LineCoveragePercent >= healthLowCoverageThreshold
}

func failureHealthEvidence(
	file CodeFile,
	failures []RepeatedFailure,
) []CodeHealthEvidence {
	evidence := make([]CodeHealthEvidence, 0, len(failures))

	for _, failure := range failures {
		evidence = append(evidence, healthEvidence(
			"repeated_lint_hook_failure",
			"high",
			float64(healthFailureTraceScore*failure.TraceCount),
			fmt.Sprintf(
				"%s has %d repeated failure traces for %s",
				file.Path,
				failure.TraceCount,
				firstNonEmpty(failure.PolicyID, failure.SkillID, "unknown policy"),
			),
			failure.LastTraceID,
			1,
		))
	}

	return evidence
}

func (store *Store) persistCodeHealth(
	ctx context.Context,
	snapshot CodeHealthSnapshot,
) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin code health snapshot write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	rawSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal code health snapshot: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO code_health_snapshots(
			snapshot_id, repo_root, git_head, indexed_at_utc, total_health_score,
			target_count, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ID,
		snapshot.RepoRoot,
		snapshot.GitHead,
		snapshot.IndexedAtUTC,
		snapshot.TotalHealthScore,
		snapshot.TargetCount,
		string(rawSnapshot),
	)
	if err != nil {
		return fmt.Errorf("insert code health snapshot: %w", err)
	}

	for _, target := range snapshot.Targets {
		err = insertHealthTarget(ctx, transaction, snapshot.ID, target)
		if err != nil {
			return err
		}
	}

	err = pruneHealthSnapshots(ctx, transaction, snapshot.RepoRoot)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit code health snapshot: %w", err)
	}

	return nil
}

func insertHealthTarget(
	ctx context.Context,
	transaction *sql.Tx,
	snapshotID string,
	target CodeHealthTarget,
) error {
	rawTarget, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("marshal code health target: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO code_health_targets(
			snapshot_id, path, language, health_score, impact_score, effort_score,
			priority_score, rank, evidence_count, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshotID,
		target.Path,
		target.Language,
		target.HealthScore,
		target.ImpactScore,
		target.EffortScore,
		target.PriorityScore,
		target.Rank,
		len(target.Evidence),
		string(rawTarget),
	)
	if err != nil {
		return fmt.Errorf("insert code health target: %w", err)
	}

	for index, item := range target.Evidence {
		rawEvidence, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return fmt.Errorf("marshal code health evidence: %w", marshalErr)
		}

		_, err = transaction.ExecContext(
			ctx,
			`INSERT INTO code_health_evidence(
				snapshot_id, path, ordinal, biomarker, severity, score_delta,
				message, evidence_id, line, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotID,
			target.Path,
			index,
			item.Biomarker,
			item.Severity,
			item.ScoreDelta,
			item.Message,
			item.EvidenceID,
			item.Line,
			string(rawEvidence),
		)
		if err != nil {
			return fmt.Errorf("insert code health evidence: %w", err)
		}
	}

	return nil
}

func (store *Store) latestCodeHealth(
	ctx context.Context,
	query CodeHealthQuery,
) (CodeHealthSnapshot, bool, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT snapshot_id, repo_root, COALESCE(git_head, ''), indexed_at_utc,
			total_health_score, target_count
		FROM code_health_snapshots
		WHERE (? = '' OR repo_root = ?)
			AND (? = '' OR COALESCE(git_head, '') = ?)
		ORDER BY indexed_at_utc DESC
		LIMIT 1`,
		query.Root,
		query.Root,
		query.GitHead,
		query.GitHead,
	)

	var snapshot CodeHealthSnapshot

	err := row.Scan(
		&snapshot.ID,
		&snapshot.RepoRoot,
		&snapshot.GitHead,
		&snapshot.IndexedAtUTC,
		&snapshot.TotalHealthScore,
		&snapshot.TargetCount,
	)
	if errorsIsNoRows(err) {
		return CodeHealthSnapshot{}, false, nil
	}

	if err != nil {
		return CodeHealthSnapshot{}, false, fmt.Errorf("read latest code health: %w", err)
	}

	targets, err := store.healthTargets(ctx, snapshot.ID, query.Path, query.Limit)
	if err != nil {
		return CodeHealthSnapshot{}, false, err
	}

	trend, err := store.healthTrend(ctx, query.Root, query.Trend)
	if err != nil {
		return CodeHealthSnapshot{}, false, err
	}

	snapshot.Targets = targets
	snapshot.Trend = trend

	return snapshot, true, nil
}

func (store *Store) healthTargets(
	ctx context.Context,
	snapshotID string,
	path string,
	limit int,
) ([]CodeHealthTarget, error) {
	if limit <= 0 {
		limit = defaultHealthLimit
	}

	filter := strings.TrimSuffix(strings.TrimSpace(filepath.ToSlash(path)), "/")
	childPattern := filter + "/%"

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, COALESCE(language, ''), health_score, impact_score,
			effort_score, priority_score, rank
		FROM code_health_targets
		WHERE snapshot_id = ? AND (? = '' OR path = ? OR path LIKE ?)
		ORDER BY rank, priority_score DESC
		LIMIT ?`,
		snapshotID,
		filter,
		filter,
		childPattern,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query code health targets: %w", err)
	}
	defer rows.Close()

	targets := []CodeHealthTarget{}

	for rows.Next() {
		var target CodeHealthTarget

		err = rows.Scan(
			&target.Path,
			&target.Language,
			&target.HealthScore,
			&target.ImpactScore,
			&target.EffortScore,
			&target.PriorityScore,
			&target.Rank,
		)
		if err != nil {
			return nil, fmt.Errorf("scan code health target: %w", err)
		}

		target.Evidence, err = store.healthEvidence(ctx, snapshotID, target.Path)
		if err != nil {
			return nil, err
		}

		targets = append(targets, target)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code health targets: %w", err)
	}

	return targets, nil
}

func (store *Store) healthEvidence(
	ctx context.Context,
	snapshotID string,
	path string,
) ([]CodeHealthEvidence, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT biomarker, severity, score_delta, message,
			COALESCE(evidence_id, ''), line
		FROM code_health_evidence
		WHERE snapshot_id = ? AND path = ?
		ORDER BY ordinal`,
		snapshotID,
		path,
	)
	if err != nil {
		return nil, fmt.Errorf("query code health evidence: %w", err)
	}
	defer rows.Close()

	evidence := []CodeHealthEvidence{}

	for rows.Next() {
		var item CodeHealthEvidence

		err = rows.Scan(
			&item.Biomarker,
			&item.Severity,
			&item.ScoreDelta,
			&item.Message,
			&item.EvidenceID,
			&item.Line,
		)
		if err != nil {
			return nil, fmt.Errorf("scan code health evidence: %w", err)
		}

		evidence = append(evidence, item)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code health evidence: %w", err)
	}

	return evidence, nil
}

func (store *Store) healthTrend(
	ctx context.Context,
	root string,
	limit int,
) ([]CodeHealthTrend, error) {
	if limit <= 0 {
		limit = defaultHealthTrendLimit
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT snapshot_id, COALESCE(git_head, ''), indexed_at_utc,
			total_health_score, target_count
		FROM code_health_snapshots
		WHERE (? = '' OR repo_root = ?)
		ORDER BY indexed_at_utc DESC
		LIMIT ?`,
		root,
		root,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query code health trend: %w", err)
	}
	defer rows.Close()

	trend := []CodeHealthTrend{}

	for rows.Next() {
		var item CodeHealthTrend

		err = rows.Scan(
			&item.SnapshotID,
			&item.GitHead,
			&item.IndexedAtUTC,
			&item.TotalHealthScore,
			&item.TargetCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan code health trend: %w", err)
		}

		trend = append(trend, item)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code health trend: %w", err)
	}

	return trend, nil
}

func pruneHealthSnapshots(
	ctx context.Context,
	transaction *sql.Tx,
	repoRoot string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`DELETE FROM code_health_snapshots
		WHERE repo_root = ?
			AND snapshot_id NOT IN (
			SELECT snapshot_id FROM code_health_snapshots
			WHERE repo_root = ?
			ORDER BY indexed_at_utc DESC
			LIMIT ?
		)`,
		repoRoot,
		repoRoot,
		defaultHealthTrendLimit,
	)
	if err != nil {
		return fmt.Errorf("prune code health snapshots: %w", err)
	}

	for _, statement := range []string{
		`DELETE FROM code_health_evidence
		WHERE snapshot_id NOT IN (SELECT snapshot_id FROM code_health_snapshots)`,
		`DELETE FROM code_health_targets
		WHERE snapshot_id NOT IN (SELECT snapshot_id FROM code_health_snapshots)`,
	} {
		_, err = transaction.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("prune code health child rows: %w", err)
		}
	}

	return nil
}

func parseLCOV(
	input *os.File,
	root string,
	sourcePath string,
	importedAt string,
) ([]CodeHealthCoverage, error) {
	records := []CodeHealthCoverage{}
	scanner := bufio.NewScanner(input)
	current := lcovAccumulator{root: root, sourcePath: sourcePath, importedAt: importedAt}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = lcovAccumulator{
				path:       filepath.ToSlash(strings.TrimPrefix(line, "SF:")),
				root:       root,
				sourcePath: sourcePath,
				importedAt: importedAt,
			}
		case strings.HasPrefix(line, "DA:"):
			current.addLine(strings.TrimPrefix(line, "DA:"))
		case line == "end_of_record":
			if current.path != "" {
				records = append(records, current.record())
			}

			current = lcovAccumulator{root: root, sourcePath: sourcePath, importedAt: importedAt}
		}
	}

	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("scan LCOV %s: %w", sourcePath, err)
	}

	if current.path != "" {
		records = append(records, current.record())
	}

	return records, nil
}

type lcovAccumulator struct {
	path       string
	root       string
	sourcePath string
	importedAt string
	found      int
	covered    int
}

func (accumulator *lcovAccumulator) addLine(value string) {
	lineNumber, rest, found := strings.Cut(value, ",")
	if !found || strings.TrimSpace(lineNumber) == "" {
		return
	}

	countText, _, _ := strings.Cut(rest, ",")

	count, err := strconv.Atoi(strings.TrimSpace(countText))
	if err != nil {
		return
	}

	accumulator.found++
	if count > 0 {
		accumulator.covered++
	}
}

func (accumulator *lcovAccumulator) record() CodeHealthCoverage {
	percent := 0.0
	if accumulator.found != 0 {
		percent = float64(accumulator.covered) *
			healthPercentageScale / float64(accumulator.found)
	}

	return CodeHealthCoverage{
		Path:                cleanLCOVPath(accumulator.path, accumulator.root),
		SourcePath:          accumulator.sourcePath,
		ImportedAtUTC:       accumulator.importedAt,
		FoundLines:          accumulator.found,
		CoveredLines:        accumulator.covered,
		LineCoveragePercent: roundedFloat(percent),
	}
}

func cleanLCOVPath(path, root string) string {
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) && strings.TrimSpace(root) != "" {
		relative, err := filepath.Rel(root, cleaned)
		if err == nil && relative != "." && !strings.HasPrefix(relative, "..") {
			cleaned = relative
		}
	}

	cleaned = filepath.ToSlash(cleaned)
	if after, ok := strings.CutPrefix(cleaned, "../"); ok {
		return after
	}

	return strings.TrimPrefix(cleaned, "./")
}

func defaultHealthBiomarkers() map[string]CodeHealthBiomarkerSetting {
	return map[string]CodeHealthBiomarkerSetting{
		"large_file":                    {Enabled: true, Weight: 1},
		"large_function":                {Enabled: true, Weight: 1},
		"deep_nesting":                  {Enabled: true, Weight: 1},
		"complex_conditional":           {Enabled: true, Weight: 1},
		"complex_function":              {Enabled: true, Weight: 1},
		"brain_method_candidate":        {Enabled: true, Weight: 1},
		"structural_clone":              {Enabled: true, Weight: 1},
		"git_hotspot":                   {Enabled: true, Weight: 1},
		"hotspot_without_test_evidence": {Enabled: true, Weight: 1},
		"hidden_coupling":               {Enabled: true, Weight: 1},
		"ownership_risk":                {Enabled: true, Weight: 1},
		"repeated_lint_hook_failure":    {Enabled: true, Weight: 1},
	}
}

func applyCodeHealthConfig(settings *CodeHealthSettings, config configdata.Map) {
	health := configdata.MapValue(configdata.GetPath(config, "code_intel.health", nil))
	if health == nil {
		return
	}

	for name, rawSetting := range configdata.MapValue(health["biomarkers"]) {
		setting := configdata.MapValue(rawSetting)

		current := settings.Biomarkers[name]

		enabled, ok := setting["enabled"].(bool)
		if ok {
			current.Enabled = enabled
		}

		if weightValue, found := setting["weight"]; found {
			current.Weight = floatValue(weightValue)
		}

		settings.Biomarkers[name] = current
	}

	for _, rawOverride := range configdata.ListValue(health["path_overrides"]) {
		override := configdata.MapValue(rawOverride)

		glob := configdata.StringAt(override, "glob")
		if glob == "" {
			continue
		}

		setting := CodeHealthPathSetting{
			Glob:               glob,
			DisabledBiomarkers: configdata.StringList(override["disabled_biomarkers"]),
			Weights:            floatMap(configdata.MapValue(override["weights"])),
		}
		setting.globPattern = compileHealthGlob(glob)

		settings.PathOverrides = append(settings.PathOverrides, setting)
	}
}

func weightedHealthEvidence(
	path string,
	item CodeHealthEvidence,
	settings CodeHealthSettings,
) (CodeHealthEvidence, bool) {
	setting, found := settings.Biomarkers[item.Biomarker]
	if found && !setting.Enabled {
		return CodeHealthEvidence{}, false
	}

	weight := 1.0
	if found {
		weight = setting.Weight
	}

	for _, override := range settings.PathOverrides {
		if !override.matches(path) {
			continue
		}

		if slices.Contains(override.DisabledBiomarkers, item.Biomarker) {
			return CodeHealthEvidence{}, false
		}

		if overrideWeight, ok := override.Weights[item.Biomarker]; ok {
			weight = overrideWeight
		}
	}

	item.ScoreDelta = roundedFloat(item.ScoreDelta * weight)
	if item.ScoreDelta <= 0 {
		return CodeHealthEvidence{}, false
	}

	return item, true
}

func healthGlobMatches(pattern, path string) bool {
	compiled := compileHealthGlob(pattern)
	if compiled == nil {
		return false
	}

	return compiled.MatchString(filepath.ToSlash(path))
}

func compileHealthGlob(pattern string) *regexp.Regexp {
	expression := regexp.QuoteMeta(filepath.ToSlash(pattern))
	expression = strings.ReplaceAll(expression, `\*\*`, `.*`)
	expression = strings.ReplaceAll(expression, `\*`, `[^/]*`)

	compiled, err := regexp.Compile(`^` + expression + `$`)
	if err != nil {
		return nil
	}

	return compiled
}

func (setting CodeHealthPathSetting) matches(path string) bool {
	if setting.globPattern != nil {
		return setting.globPattern.MatchString(filepath.ToSlash(path))
	}

	return healthGlobMatches(setting.Glob, path)
}

func floatMap(values configdata.Map) map[string]float64 {
	if values == nil {
		return nil
	}

	result := make(map[string]float64, len(values))
	for key, value := range values {
		result[key] = floatValue(value)
	}

	return result
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}

	return 0
}

func chunksByPath(chunks []CodeChunk) map[string][]CodeChunk {
	byPath := map[string][]CodeChunk{}
	for _, chunk := range chunks {
		byPath[chunk.Path] = append(byPath[chunk.Path], chunk)
	}

	return byPath
}

func cloneCountsByPath(chunks []CodeChunk) map[string]int {
	hashPaths := map[string]map[string]bool{}

	for _, chunk := range chunks {
		if chunk.NormalizedHash == "" {
			continue
		}

		paths := hashPaths[chunk.NormalizedHash]
		if paths == nil {
			paths = map[string]bool{}
			hashPaths[chunk.NormalizedHash] = paths
		}

		paths[chunk.Path] = true
	}

	counts := map[string]int{}

	for _, paths := range hashPaths {
		if len(paths) < healthMinClonePathCount {
			continue
		}

		for path := range paths {
			counts[path] += len(paths) - 1
		}
	}

	return counts
}

func gitSignalMap(signals []GitFileSignal) map[string]GitFileSignal {
	byPath := map[string]GitFileSignal{}
	for _, signal := range signals {
		byPath[signal.Path] = signal
	}

	return byPath
}

func repeatedFailureMap(failures []RepeatedFailure) map[string][]RepeatedFailure {
	byPath := map[string][]RepeatedFailure{}
	for _, failure := range failures {
		byPath[failure.Path] = append(byPath[failure.Path], failure)
	}

	return byPath
}

func healthEvidence(
	biomarker string,
	severity string,
	scoreDelta float64,
	message string,
	evidenceID string,
	line int,
) CodeHealthEvidence {
	return CodeHealthEvidence{
		Biomarker:  biomarker,
		Severity:   severity,
		ScoreDelta: scoreDelta,
		Message:    message,
		EvidenceID: evidenceID,
		Line:       line,
	}
}

func isCallableChunk(chunk CodeChunk) bool {
	kind := strings.ToLower(chunk.SymbolKind + " " + chunk.NodeKind)

	return strings.Contains(kind, "function") ||
		strings.Contains(kind, "method") ||
		strings.Contains(kind, "procedure")
}

func healthChunkName(chunk CodeChunk) string {
	return firstNonEmpty(chunk.SymbolPath, chunk.SymbolName, chunk.ID)
}

func branchTokenCount(text string) int {
	return len(healthBranchTokenPattern.FindAllString(text, -1))
}

func indentationDepth(text string) int {
	maxDepth := 0

	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}

		depth := indentationLevel(line[:len(line)-len(trimmed)])
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth
}

func indentationLevel(indent string) int {
	columns := 0

	for _, char := range indent {
		switch char {
		case '\t':
			columns += healthIndentSpaces
		case ' ':
			columns++
		}
	}

	return columns / healthIndentSpaces
}

func isTestPath(path string) bool {
	lower := strings.ToLower(path)

	return strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "_test.") ||
		strings.Contains(lower, ".spec.") ||
		strings.HasPrefix(filepath.Base(lower), "test_")
}

func healthPathMatches(path, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}

	return path == filter || strings.HasPrefix(path, strings.TrimSuffix(filter, "/")+"/")
}

func impactScore(evidence []CodeHealthEvidence) float64 {
	score := 0.0
	for _, item := range evidence {
		score += item.ScoreDelta
	}

	return score
}

func effortScore(file CodeFile, chunks []CodeChunk) float64 {
	return math.Max(
		healthMinEffortScore,
		math.Log2(float64(file.LineCount+len(chunks)+healthEffortSmoothing)),
	)
}

func repoHealthScore(totalScore float64, totalFiles int) float64 {
	if totalFiles == 0 {
		return healthBaseScore
	}

	return roundedFloat(totalScore / float64(totalFiles))
}

func compareHealthTargets(left, right CodeHealthTarget) int {
	if left.PriorityScore != right.PriorityScore {
		return cmpFloatDesc(left.PriorityScore, right.PriorityScore)
	}

	if left.ImpactScore != right.ImpactScore {
		return cmpFloatDesc(left.ImpactScore, right.ImpactScore)
	}

	return strings.Compare(left.Path, right.Path)
}

func cmpFloatDesc(left, right float64) int {
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func roundedFloat(value float64) float64 {
	return math.Round(value*healthFloatPrecision) / healthFloatPrecision
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
