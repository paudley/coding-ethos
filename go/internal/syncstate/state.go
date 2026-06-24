// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package syncstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	SchemaVersion = 1
	StatePath     = ".coding-ethos/state/install-sync.json"

	DefaultOwnership = "coding-ethos-managed"

	artifactStatusDrifted     = "drifted"
	artifactStatusMissing     = "missing"
	artifactStatusPlanned     = "planned"
	artifactStatusUnchanged   = "unchanged"
	artifactStatusWouldUpdate = "would_update"
	sourceStatusCurrent       = "current"
	sourceStatusMissing       = "missing"
	sourceStatusStale         = "stale"
	reportStatusPass          = "pass"
	stateDirMode              = 0o700
	stateFileMode             = 0o600
)

var (
	errArtifactOutsideRepo = errors.New("artifact path is outside repo root")
	errInvalidSyncState    = errors.New("invalid sync state")
)

type State struct {
	TargetRepoRoot    string           `json:"target_repo_root"`
	RequestedAction   string           `json:"requested_action"`
	RuntimeVersion    string           `json:"runtime_version"`
	RuntimeCommit     string           `json:"runtime_commit"`
	LastValidationUTC string           `json:"last_validation_utc"`
	SourceHashes      []SourceHash     `json:"source_config_hashes"`
	ProviderTargets   []ProviderTarget `json:"provider_targets"`
	Artifacts         []Artifact       `json:"artifacts"`
	SchemaVersion     int              `json:"schema_version"`
}

type SourceHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ProviderTarget struct {
	Provider string `json:"provider"`
	Root     string `json:"root"`
}

type Artifact struct {
	Path                string `json:"path"`
	Provider            string `json:"provider"`
	Surface             string `json:"surface"`
	Ownership           string `json:"ownership"`
	ExpectedSHA256      string `json:"expected_sha256"`
	VerificationCommand string `json:"verification_command"`
	LastWrittenUTC      string `json:"last_written_utc,omitempty"`
}

type ArtifactInput struct {
	RelativePath        string
	Content             string
	Provider            string
	Surface             string
	Ownership           string
	VerificationCommand string
}

type UpsertOptions struct {
	Now             time.Time
	RepoRoot        string
	EthosRoot       string
	RequestedAction string
	SourcePaths     []string
	ProviderTargets []ProviderTarget
	Artifacts       []Artifact
}

type Report struct {
	Tool              string           `json:"tool"`
	Status            string           `json:"status"`
	StatePath         string           `json:"state_path"`
	TargetRepoRoot    string           `json:"target_repo_root"`
	RequestedAction   string           `json:"requested_action,omitempty"`
	RuntimeVersion    string           `json:"runtime_version,omitempty"`
	RuntimeCommit     string           `json:"runtime_commit,omitempty"`
	LastValidationUTC string           `json:"last_validation_utc,omitempty"`
	ProviderTargets   []ProviderTarget `json:"provider_targets,omitempty"`
	Artifacts         []ArtifactReport `json:"artifacts"`
	Sources           []SourceReport   `json:"sources,omitempty"`
	PlannedWriteCount int              `json:"planned_write_count"`
}

type ArtifactReport struct {
	Path                string `json:"path"`
	Provider            string `json:"provider"`
	Surface             string `json:"surface"`
	Ownership           string `json:"ownership"`
	Status              string `json:"status"`
	Plan                string `json:"plan,omitempty"`
	ExpectedSHA256      string `json:"expected_sha256,omitempty"`
	ActualSHA256        string `json:"actual_sha256,omitempty"`
	VerificationCommand string `json:"verification_command,omitempty"`
}

type SourceReport struct {
	Path           string `json:"path"`
	Status         string `json:"status"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
}

func Artifacts(repoRoot string, inputs []ArtifactInput) ([]Artifact, error) {
	artifacts := make([]Artifact, 0, len(inputs))

	for _, input := range inputs {
		relativePath, err := repoRelativePath(repoRoot, input.RelativePath)
		if err != nil {
			return nil, err
		}

		ownership := strings.TrimSpace(input.Ownership)
		if ownership == "" {
			ownership = DefaultOwnership
		}

		artifacts = append(artifacts, Artifact{
			Path:                relativePath,
			Provider:            strings.TrimSpace(input.Provider),
			Surface:             strings.TrimSpace(input.Surface),
			Ownership:           ownership,
			ExpectedSHA256:      hashString(input.Content),
			VerificationCommand: strings.TrimSpace(input.VerificationCommand),
		})
	}

	sortArtifacts(artifacts)

	return artifacts, nil
}

func Upsert(options UpsertOptions) (State, error) {
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	path := FilePath(options.RepoRoot)

	state, err := ReadFile(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist), errors.Is(err, errInvalidSyncState):
		state = State{SchemaVersion: SchemaVersion}
	default:
		return State{}, err
	}

	state.SchemaVersion = SchemaVersion
	state.TargetRepoRoot = filepath.Clean(options.RepoRoot)
	state.RequestedAction = strings.TrimSpace(options.RequestedAction)
	state.LastValidationUTC = now.UTC().Format(time.RFC3339)
	state.RuntimeVersion = runtimeVersion(options.EthosRoot)
	state.RuntimeCommit = runtimeCommit(options.EthosRoot)
	state.SourceHashes = mergeSources(state.SourceHashes, hashSources(options.SourcePaths))
	state.ProviderTargets = mergeProviderTargets(
		state.ProviderTargets,
		normalizedProviderTargets(options.RepoRoot, options.ProviderTargets),
	)
	state.Artifacts = mergeArtifacts(
		state.Artifacts,
		timestampArtifacts(options.Artifacts, now),
	)

	err = WriteFile(path, state)
	if err != nil {
		return State{}, err
	}

	return state, nil
}

func Read(repoRoot string) (State, error) {
	return ReadFile(FilePath(repoRoot))
}

func ReadFile(path string) (State, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return State{}, fmt.Errorf("read sync state %s: %w", path, err)
	}

	var state State

	err = json.Unmarshal(payload, &state)
	if err != nil {
		return State{}, fmt.Errorf(
			"parse sync state %s: %w: %w",
			path,
			errInvalidSyncState,
			err,
		)
	}

	return state, nil
}

func WriteFile(path string, state State) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sync state: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(path), stateDirMode)
	if err != nil {
		return fmt.Errorf("create sync state dir: %w", err)
	}

	err = os.WriteFile(filepath.Clean(path), append(payload, '\n'), stateFileMode)
	if err != nil {
		return fmt.Errorf("write sync state %s: %w", path, err)
	}

	return nil
}

func FilePath(repoRoot string) string {
	return filepath.Join(filepath.Clean(repoRoot), filepath.FromSlash(StatePath))
}

func Plan(repoRoot, requestedAction string, artifacts []Artifact) Report {
	report := baseReport("install-sync-plan", repoRoot)
	report.RequestedAction = requestedAction
	report.Artifacts = artifactReports(repoRoot, artifacts, true)
	report.PlannedWriteCount = plannedWriteCount(report.Artifacts)

	if report.PlannedWriteCount == 0 {
		report.Status = reportStatusPass
	} else {
		report.Status = artifactStatusPlanned
	}

	return report
}

func Doctor(repoRoot string) (Report, error) {
	state, err := Read(repoRoot)
	if err != nil {
		return Report{}, err
	}

	report := reportFromState("install-sync-doctor", repoRoot, state)
	report.Artifacts = artifactReports(repoRoot, state.Artifacts, false)
	report.Sources = sourceReports(state.SourceHashes)
	report.PlannedWriteCount = plannedWriteCount(report.Artifacts)
	report.Status = doctorStatus(report)

	return report, nil
}

func RepairPlan(repoRoot string) (Report, error) {
	state, err := Read(repoRoot)
	if err != nil {
		return Report{}, err
	}

	owned := make([]Artifact, 0, len(state.Artifacts))
	for _, artifact := range state.Artifacts {
		if artifact.Ownership == DefaultOwnership {
			owned = append(owned, artifact)
		}
	}

	report := reportFromState("install-sync-repair-plan", repoRoot, state)
	report.Artifacts = artifactReports(repoRoot, owned, true)
	report.Sources = sourceReports(state.SourceHashes)
	report.PlannedWriteCount = plannedWriteCount(report.Artifacts)

	if report.PlannedWriteCount == 0 {
		report.Status = reportStatusPass
	} else {
		report.Status = artifactStatusPlanned
	}

	return report, nil
}

func (report Report) MarshalFeedbackJSON() any {
	return report
}

func (report Report) MarshalFeedbackTOON() string {
	lines := []string{
		"tool: " + feedback.Cell(report.Tool),
		"status: " + feedback.Cell(report.Status),
		"state_path: " + feedback.Cell(report.StatePath),
		"target_repo_root: " + feedback.Cell(report.TargetRepoRoot),
	}

	if report.RequestedAction != "" {
		lines = append(lines, "requested_action: "+feedback.Cell(report.RequestedAction))
	}

	if report.RuntimeVersion != "" {
		lines = append(lines, "runtime_version: "+feedback.Cell(report.RuntimeVersion))
	}

	if report.RuntimeCommit != "" {
		lines = append(lines, "runtime_commit: "+feedback.Cell(report.RuntimeCommit))
	}

	if report.LastValidationUTC != "" {
		lines = append(lines, "last_validation_utc: "+feedback.Cell(report.LastValidationUTC))
	}

	lines = append(lines, providerTargetTOONLines(report.ProviderTargets)...)
	lines = append(lines, sourceTOONLines(report.Sources)...)
	lines = append(lines, artifactTOONLines(report.Artifacts)...)
	lines = append(
		lines,
		fmt.Sprintf("planned_write_count: %d", report.PlannedWriteCount),
	)

	return strings.Join(lines, "\n")
}

func (report Report) MarshalFeedbackHuman() string {
	return report.MarshalFeedbackTOON()
}

func (report Report) MarshalFeedbackSARIF() feedback.SARIFLog {
	return feedback.Text{Text: report.MarshalFeedbackTOON()}.MarshalFeedbackSARIF()
}

func (report Report) FeedbackLogFields() map[string]any {
	return map[string]any{
		"tool":                report.Tool,
		"status":              report.Status,
		"state_path":          report.StatePath,
		"planned_write_count": report.PlannedWriteCount,
	}
}

func baseReport(tool, repoRoot string) Report {
	return Report{
		Tool:           tool,
		StatePath:      FilePath(repoRoot),
		TargetRepoRoot: filepath.Clean(repoRoot),
	}
}

func reportFromState(tool, repoRoot string, state State) Report {
	report := baseReport(tool, repoRoot)
	report.RequestedAction = state.RequestedAction
	report.RuntimeVersion = state.RuntimeVersion
	report.RuntimeCommit = state.RuntimeCommit
	report.LastValidationUTC = state.LastValidationUTC
	report.ProviderTargets = state.ProviderTargets

	return report
}

func artifactReports(
	repoRoot string,
	artifacts []Artifact,
	dryRun bool,
) []ArtifactReport {
	reports := make([]ArtifactReport, 0, len(artifacts))
	for _, artifact := range artifacts {
		report := ArtifactReport{
			Path:                artifact.Path,
			Provider:            artifact.Provider,
			Surface:             artifact.Surface,
			Ownership:           artifact.Ownership,
			ExpectedSHA256:      artifact.ExpectedSHA256,
			VerificationCommand: artifact.VerificationCommand,
		}

		actual, err := fileSHA256(filepath.Join(repoRoot, filepath.FromSlash(artifact.Path)))
		switch {
		case err != nil:
			report.Status = artifactStatusMissing
			report.Plan = "write"
		case actual == artifact.ExpectedSHA256:
			report.ActualSHA256 = actual
			report.Status = artifactStatusUnchanged
			report.Plan = "none"
		case dryRun:
			report.ActualSHA256 = actual
			report.Status = artifactStatusWouldUpdate
			report.Plan = "write"
		default:
			report.ActualSHA256 = actual
			report.Status = artifactStatusDrifted
			report.Plan = "repair"
		}

		reports = append(reports, report)
	}

	sort.SliceStable(reports, func(left, right int) bool {
		return reports[left].Path < reports[right].Path
	})

	return reports
}

func sourceReports(sources []SourceHash) []SourceReport {
	reports := make([]SourceReport, 0, len(sources))
	for _, source := range sources {
		report := SourceReport{
			Path:           source.Path,
			ExpectedSHA256: source.SHA256,
		}

		actual, err := fileSHA256(source.Path)
		if err != nil {
			report.Status = sourceStatusMissing
		} else {
			report.ActualSHA256 = actual
			if actual == source.SHA256 {
				report.Status = sourceStatusCurrent
			} else {
				report.Status = sourceStatusStale
			}
		}

		reports = append(reports, report)
	}

	sort.SliceStable(reports, func(left, right int) bool {
		return reports[left].Path < reports[right].Path
	})

	return reports
}

func doctorStatus(report Report) string {
	for _, source := range report.Sources {
		if source.Status != sourceStatusCurrent {
			return "fail"
		}
	}

	for _, artifact := range report.Artifacts {
		if artifact.Status != artifactStatusUnchanged {
			return "fail"
		}
	}

	return reportStatusPass
}

func plannedWriteCount(artifacts []ArtifactReport) int {
	count := 0

	for _, artifact := range artifacts {
		if artifact.Plan == "write" || artifact.Plan == "repair" {
			count++
		}
	}

	return count
}

func providerTargetTOONLines(targets []ProviderTarget) []string {
	lines := make([]string, 0, 1+len(targets))
	lines = append(
		lines,
		fmt.Sprintf("provider_targets[%d]{provider,root}:", len(targets)),
	)

	for _, target := range targets {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s",
			feedback.Cell(target.Provider),
			feedback.Cell(target.Root),
		))
	}

	return lines
}

func sourceTOONLines(sources []SourceReport) []string {
	lines := make([]string, 0, 1+len(sources))
	lines = append(lines, fmt.Sprintf(
		"sources[%d]{path,status,expected_sha256,actual_sha256}:",
		len(sources),
	))

	for _, source := range sources {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s",
			feedback.Cell(source.Path),
			feedback.Cell(source.Status),
			feedback.Cell(source.ExpectedSHA256),
			feedback.Cell(source.ActualSHA256),
		))
	}

	return lines
}

func artifactTOONLines(artifacts []ArtifactReport) []string {
	const columns = "path,provider,surface,status,ownership,plan," +
		"expected_sha256,actual_sha256,verification_command"

	lines := make([]string, 0, 1+len(artifacts))
	lines = append(lines, fmt.Sprintf("artifacts[%d]{%s}:", len(artifacts), columns))

	for _, artifact := range artifacts {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%s,%s,%s,%s,%s",
			feedback.Cell(artifact.Path),
			feedback.Cell(artifact.Provider),
			feedback.Cell(artifact.Surface),
			feedback.Cell(artifact.Status),
			feedback.Cell(artifact.Ownership),
			feedback.Cell(artifact.Plan),
			feedback.Cell(artifact.ExpectedSHA256),
			feedback.Cell(artifact.ActualSHA256),
			feedback.Cell(artifact.VerificationCommand),
		))
	}

	return lines
}

func mergeSources(existing, incoming []SourceHash) []SourceHash {
	merged := map[string]SourceHash{}

	for _, source := range existing {
		merged[source.Path] = source
	}

	for _, source := range incoming {
		merged[source.Path] = source
	}

	sources := make([]SourceHash, 0, len(merged))
	for _, source := range merged {
		sources = append(sources, source)
	}

	sort.SliceStable(sources, func(left, right int) bool {
		return sources[left].Path < sources[right].Path
	})

	return sources
}

func mergeProviderTargets(existing, incoming []ProviderTarget) []ProviderTarget {
	merged := map[string]ProviderTarget{}

	for _, target := range existing {
		merged[target.Provider+"\x00"+target.Root] = target
	}

	for _, target := range incoming {
		merged[target.Provider+"\x00"+target.Root] = target
	}

	targets := make([]ProviderTarget, 0, len(merged))
	for _, target := range merged {
		targets = append(targets, target)
	}

	sort.SliceStable(targets, func(left, right int) bool {
		if targets[left].Provider != targets[right].Provider {
			return targets[left].Provider < targets[right].Provider
		}

		return targets[left].Root < targets[right].Root
	})

	return targets
}

func mergeArtifacts(existing, incoming []Artifact) []Artifact {
	merged := map[string]Artifact{}

	for _, artifact := range existing {
		merged[artifact.Provider+"\x00"+artifact.Path] = artifact
	}

	for _, artifact := range incoming {
		merged[artifact.Provider+"\x00"+artifact.Path] = artifact
	}

	artifacts := make([]Artifact, 0, len(merged))
	for _, artifact := range merged {
		artifacts = append(artifacts, artifact)
	}

	sortArtifacts(artifacts)

	return artifacts
}

func timestampArtifacts(artifacts []Artifact, now time.Time) []Artifact {
	timestamped := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifact.LastWrittenUTC = now.UTC().Format(time.RFC3339)
		timestamped = append(timestamped, artifact)
	}

	return timestamped
}

func normalizedProviderTargets(
	repoRoot string,
	targets []ProviderTarget,
) []ProviderTarget {
	normalized := make([]ProviderTarget, 0, len(targets))
	for _, target := range targets {
		root := strings.TrimSpace(target.Root)
		if root == "" {
			root = repoRoot
		}

		normalized = append(normalized, ProviderTarget{
			Provider: strings.TrimSpace(target.Provider),
			Root:     filepath.Clean(root),
		})
	}

	return normalized
}

func sortArtifacts(artifacts []Artifact) {
	sort.SliceStable(artifacts, func(left, right int) bool {
		if artifacts[left].Provider != artifacts[right].Provider {
			return artifacts[left].Provider < artifacts[right].Provider
		}

		return artifacts[left].Path < artifacts[right].Path
	})
}

func hashSources(paths []string) []SourceHash {
	sources := make([]SourceHash, 0, len(paths))
	for _, path := range paths {
		cleaned := filepath.Clean(path)
		if strings.TrimSpace(cleaned) == "" || cleaned == "." {
			continue
		}

		sum, err := fileSHA256(cleaned)
		if err != nil {
			continue
		}

		sources = append(sources, SourceHash{Path: cleaned, SHA256: sum})
	}

	sort.SliceStable(sources, func(left, right int) bool {
		return sources[left].Path < sources[right].Path
	})

	return sources
}

func fileSHA256(path string) (string, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read file for SHA-256 %s: %w", path, err)
	}

	return hashBytes(payload), nil
}

func hashString(value string) string {
	return hashBytes([]byte(value))
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func repoRelativePath(repoRoot, path string) (string, error) {
	cleaned := filepath.Clean(path)
	root := filepath.Clean(repoRoot)

	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}

	relative, err := filepath.Rel(root, cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve relative artifact path %s: %w", path, err)
	}

	if pathEscapesRoot(relative) {
		return "", fmt.Errorf(
			"artifact path %s is outside repo root %s: %w",
			path,
			repoRoot,
			errArtifactOutsideRepo,
		)
	}

	return filepath.ToSlash(relative), nil
}

func pathEscapesRoot(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func runtimeVersion(ethosRoot string) string {
	payload, err := os.ReadFile(filepath.Join(ethosRoot, "pyproject.toml"))
	if err != nil {
		return ""
	}

	inProject := false

	for line := range strings.SplitSeq(string(payload), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[project]" {
			inProject = true

			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			inProject = false

			continue
		}

		if !inProject {
			continue
		}

		if !strings.HasPrefix(trimmed, "version = ") {
			continue
		}

		return strings.Trim(strings.TrimPrefix(trimmed, "version = "), `"`)
	}

	return ""
}

func runtimeCommit(ethosRoot string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	command := realgit.Command(ctx, false, "rev-parse", "HEAD")
	command.Dir = filepath.Clean(ethosRoot)

	output, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}
