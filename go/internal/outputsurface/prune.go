// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputsurface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

const (
	pruneTraceDirMode  = 0o700
	pruneTraceFileMode = 0o600
)

var (
	errUnknownOutputScope   = errors.New("unknown output surface scope")
	errUnsupportedPruneKind = errors.New("unsupported output surface prune kind")
)

// PruneOptions controls output prune candidate selection and deletion.
type PruneOptions struct {
	Now         time.Time
	Settings    Settings
	Root        string
	Scopes      []string
	OlderThan   time.Duration
	IncludeTemp bool
	Apply       bool
	Vacuum      bool
	All         bool
}

// PruneReport summarizes a prune run.
type PruneReport struct {
	GeneratedAtUTC string           `json:"generated_at_utc"`
	Root           string           `json:"root"`
	TracePath      string           `json:"trace_path,omitempty"`
	Candidates     []PruneCandidate `json:"candidates"`
	DBMaintenance  []DBMaintenance  `json:"db_maintenance,omitempty"`
	Errors         []string         `json:"errors,omitempty"`
	DeletedFiles   int              `json:"deleted_files"`
	DeletedDirs    int              `json:"deleted_dirs"`
	DeletedBytes   int64            `json:"deleted_bytes"`
	Skipped        int              `json:"skipped"`
	Apply          bool             `json:"apply"`
}

// DBMaintenance describes code-intel database row and storage maintenance.
type DBMaintenance struct {
	SurfaceID          string `json:"surface_id"`
	CutoffUTC          string `json:"cutoff_utc,omitempty"`
	DeletedTraces      int    `json:"deleted_traces,omitempty"`
	DeletedProxyEvents int    `json:"deleted_proxy_events,omitempty"`
	Vacuumed           bool   `json:"vacuumed,omitempty"`
}

// PruneCandidate describes one candidate selected by policy or CLI flags.
type PruneCandidate struct {
	SurfaceID string `json:"surface_id"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	Bytes     int64  `json:"bytes"`
	Skipped   bool   `json:"skipped"`
	Deleted   bool   `json:"deleted"`
}

// Prune selects and optionally removes stale output surface entries.
func Prune(ctx context.Context, options PruneOptions) (PruneReport, error) {
	absoluteRoot, err := normalizedPruneRoot(options.Root)
	if err != nil {
		return PruneReport{}, err
	}

	now := normalizedPruneTime(options.Now)
	settings := normalizedPruneSettings(options.Settings)

	report := PruneReport{
		GeneratedAtUTC: now.UTC().Format(time.RFC3339),
		Root:           filepath.Clean(absoluteRoot),
		Apply:          options.Apply,
	}
	scopes := scopeSet(options.Scopes)

	err = validateScopes(scopes)
	if err != nil {
		return PruneReport{}, err
	}

	collectPruneWork(ctx, &report, settings, options, scopes, now)

	if options.Apply && options.Vacuum && shouldVacuum(scopes) {
		err = vacuumCodeIntel(ctx, report.Root, &report)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
	}

	if options.Apply {
		applyPruneCandidates(&report)
	}

	report.Skipped = skippedCandidateCount(report.Candidates)

	if options.Apply && settings.Prune.WritePruneTrace && reportHasTraceableWork(report) {
		tracePath, err := writePruneTrace(report.Root, &report, now)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.TracePath = tracePath
		}
	}

	return report, nil
}

func normalizedPruneRoot(root string) (string, error) {
	cleanRoot := strings.TrimSpace(root)
	if cleanRoot == "" {
		cleanRoot = "."
	}

	absoluteRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve prune root: %w", err)
	}

	return filepath.Clean(absoluteRoot), nil
}

func normalizedPruneTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}

	return now
}

func normalizedPruneSettings(settings Settings) Settings {
	if settings.Prune.Surfaces == nil {
		return DefaultSettings()
	}

	return settings
}

func collectPruneWork(
	ctx context.Context,
	report *PruneReport,
	settings Settings,
	options PruneOptions,
	scopes map[string]bool,
	now time.Time,
) {
	for _, definition := range Definitions() {
		if !shouldPruneSurface(definition, options, scopes) {
			continue
		}

		policy := retentionPolicy(settings, definition)
		appendFilesystemCandidates(ctx, report, definition, policy, options, now)
		appendDBMaintenance(ctx, report, definition, policy, options, now)
	}
}

func appendFilesystemCandidates(
	ctx context.Context,
	report *PruneReport,
	definition Definition,
	policy SurfaceRetentionPolicy,
	options PruneOptions,
	now time.Time,
) {
	candidates, err := pruneCandidates(report.Root, definition, policy, options, now)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
	}

	if policy.RequireCodeIntelIngest {
		candidates, err = markUningestedCandidates(ctx, report.Root, candidates)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
	}

	report.Candidates = append(report.Candidates, candidates...)
}

func appendDBMaintenance(
	ctx context.Context,
	report *PruneReport,
	definition Definition,
	policy SurfaceRetentionPolicy,
	options PruneOptions,
	now time.Time,
) {
	if !definition.DBMaintenance {
		return
	}

	maintenance, hasMaintenance, err := pruneCodeIntelRows(
		ctx,
		report.Root,
		policy,
		options,
		now,
	)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())

		return
	}

	if hasMaintenance {
		report.DBMaintenance = append(report.DBMaintenance, maintenance)
	}
}

func skippedCandidateCount(candidates []PruneCandidate) int {
	count := 0

	for _, candidate := range candidates {
		if candidate.Skipped {
			count++
		}
	}

	return count
}

func reportHasTraceableWork(report PruneReport) bool {
	if len(report.Candidates) > 0 || len(report.Errors) > 0 {
		return true
	}

	for _, maintenance := range report.DBMaintenance {
		hasDeletedRows := maintenance.DeletedTraces > 0 || maintenance.DeletedProxyEvents > 0
		if hasDeletedRows || maintenance.Vacuumed {
			return true
		}
	}

	return false
}

func AutoPruneSurface(ctx context.Context, root, scope string, includeTemp bool) error {
	settings, err := LoadSettings(root)
	if err != nil {
		return fmt.Errorf("load output auto-prune settings: %w", err)
	}

	policy := settings.Prune.Surfaces[scope]
	if !settings.Prune.Enabled || !settings.Prune.AutoEnabled || !policy.Enabled ||
		!policy.Auto {
		return nil
	}

	_, err = Prune(ctx, PruneOptions{
		Root:        root,
		Settings:    settings,
		Scopes:      []string{scope},
		IncludeTemp: includeTemp,
		Apply:       true,
	})
	if err != nil {
		return fmt.Errorf("auto-prune output surface %s: %w", scope, err)
	}

	return nil
}

func shouldPruneSurface(
	definition Definition,
	options PruneOptions,
	scopes map[string]bool,
) bool {
	if len(scopes) > 0 && !scopes[definition.ID] {
		return false
	}

	if definition.Root == rootTemp && !options.IncludeTemp {
		return false
	}

	if definition.CommandPrune {
		return true
	}

	return definition.DBMaintenance && (len(scopes) > 0 || options.All || options.Vacuum)
}

func pruneCandidates(
	root string,
	definition Definition,
	policy SurfaceRetentionPolicy,
	options PruneOptions,
	now time.Time,
) ([]PruneCandidate, error) {
	if shouldSkipPruneCandidates(definition, policy, options) {
		return nil, nil
	}

	olderThan := candidateRetentionAge(policy, options)
	if !hasRetentionCriteria(policy, olderThan) {
		return nil, nil
	}

	switch definition.RecordKind {
	case recordKindDirectory:
		return directoryCandidates(root, definition, policy, olderThan, now)
	case recordKindGlob:
		return globCandidates(
			surfacePattern(root, definition),
			definition.ID,
			policy,
			olderThan,
			now,
		)
	case recordKindFile:
		return fileCandidate(
			surfacePattern(root, definition),
			definition.ID,
			policy,
			olderThan,
			now,
		)
	default:
		return nil, fmt.Errorf(
			"%w: surface %s has kind %s",
			errUnsupportedPruneKind,
			definition.ID,
			definition.RecordKind,
		)
	}
}

func shouldSkipPruneCandidates(
	definition Definition,
	policy SurfaceRetentionPolicy,
	options PruneOptions,
) bool {
	if definition.DBMaintenance && !definition.CommandPrune {
		return true
	}

	return !policy.Enabled && options.OlderThan == 0 && !options.All
}

func candidateRetentionAge(
	policy SurfaceRetentionPolicy,
	options PruneOptions,
) time.Duration {
	if options.OlderThan > 0 {
		return options.OlderThan
	}

	return policy.MaxAge
}

func hasRetentionCriteria(policy SurfaceRetentionPolicy, olderThan time.Duration) bool {
	return olderThan > 0 || policy.KeepLast > 0 || policy.MaxBytes > 0
}

type pruneEntry struct {
	ModTime time.Time
	Path    string
	Kind    string
	Bytes   int64
}

func directoryCandidates(
	root string,
	definition Definition,
	policy SurfaceRetentionPolicy,
	olderThan time.Duration,
	now time.Time,
) ([]PruneCandidate, error) {
	path := surfacePattern(root, definition)

	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read prune surface %s: %w", definition.ID, err)
	}

	records := []pruneEntry{}
	candidates := []PruneCandidate{}

	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())

		info, err := os.Lstat(child)
		if err != nil {
			candidates = append(
				candidates,
				skippedCandidate(definition.ID, child, "stat failed: "+err.Error()),
			)

			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			candidates = append(
				candidates,
				skippedCandidate(definition.ID, child, "symlink skipped"),
			)

			continue
		}

		if !pathWithinRoot(root, child) {
			candidates = append(
				candidates,
				skippedCandidate(definition.ID, child, "path escaped root"),
			)

			continue
		}

		size, sizeErr := candidateSize(child, info)
		if sizeErr != nil {
			candidates = append(
				candidates,
				skippedCandidate(definition.ID, child, sizeErr.Error()),
			)

			continue
		}

		records = append(records, pruneEntry{
			Path:    child,
			Kind:    candidateKind(info),
			Bytes:   size,
			ModTime: info.ModTime(),
		})
	}

	return append(
		candidates,
		retentionCandidates(definition.ID, records, policy, olderThan, now)...), nil
}

func globCandidates(
	pattern string,
	surfaceID string,
	policy SurfaceRetentionPolicy,
	olderThan time.Duration,
	now time.Time,
) ([]PruneCandidate, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob prune surface %s: %w", surfaceID, err)
	}

	slices.Sort(matches)

	records := []pruneEntry{}
	candidates := []PruneCandidate{}

	for _, match := range matches {
		info, err := os.Lstat(match)
		if err != nil {
			candidates = append(
				candidates,
				skippedCandidate(surfaceID, match, "stat failed: "+err.Error()),
			)

			continue
		}

		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		records = append(records, pruneEntry{
			Path:    match,
			Kind:    recordKindFile,
			Bytes:   info.Size(),
			ModTime: info.ModTime(),
		})
	}

	return append(
		candidates,
		retentionCandidates(surfaceID, records, policy, olderThan, now)...), nil
}

func fileCandidate(
	path string,
	surfaceID string,
	policy SurfaceRetentionPolicy,
	olderThan time.Duration,
	now time.Time,
) ([]PruneCandidate, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("stat prune surface %s: %w", surfaceID, err)
	}

	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil
	}

	return retentionCandidates(surfaceID, []pruneEntry{{
		Path:    path,
		Kind:    recordKindFile,
		Bytes:   info.Size(),
		ModTime: info.ModTime(),
	}}, policy, olderThan, now), nil
}

func retentionCandidates(
	surfaceID string,
	records []pruneEntry,
	policy SurfaceRetentionPolicy,
	olderThan time.Duration,
	now time.Time,
) []PruneCandidate {
	sort.SliceStable(records, func(left, right int) bool {
		return records[left].ModTime.After(records[right].ModTime)
	})

	candidates := []PruneCandidate{}
	selected := map[string]bool{}

	var cumulativeBytes int64
	for index, record := range records {
		cumulativeBytes += record.Bytes

		reason := candidateReason(index, cumulativeBytes, record, policy, olderThan, now)
		if reason == "" || selected[record.Path] {
			continue
		}

		selected[record.Path] = true
		candidates = append(candidates, PruneCandidate{
			SurfaceID: surfaceID,
			Path:      filepath.ToSlash(record.Path),
			Kind:      record.Kind,
			Reason:    reason,
			Bytes:     record.Bytes,
		})
	}

	return candidates
}

func candidateReason(
	index int,
	cumulativeBytes int64,
	record pruneEntry,
	policy SurfaceRetentionPolicy,
	olderThan time.Duration,
	now time.Time,
) string {
	if olderThan > 0 && now.Sub(record.ModTime) > olderThan {
		return "retention max_age " + durationText(olderThan)
	}

	if policy.KeepLast > 0 && index >= policy.KeepLast {
		return fmt.Sprintf("retention keep_last %d", policy.KeepLast)
	}

	if policy.MaxBytes > 0 && cumulativeBytes > policy.MaxBytes {
		return "retention max_bytes " + bytesText(policy.MaxBytes)
	}

	return ""
}

func applyPruneCandidates(report *PruneReport) {
	for index := range report.Candidates {
		candidate := &report.Candidates[index]
		if candidate.Skipped {
			continue
		}

		path := filepath.Clean(candidate.Path)

		var err error
		if candidate.Kind == recordKindDirectory {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}

		if err != nil {
			candidate.Skipped = true
			candidate.Reason = "delete failed: " + err.Error()
			report.Errors = append(
				report.Errors,
				fmt.Sprintf("delete %s: %v", candidate.Path, err),
			)

			continue
		}

		candidate.Deleted = true

		report.DeletedBytes += candidate.Bytes
		if candidate.Kind == recordKindDirectory {
			report.DeletedDirs++
		} else {
			report.DeletedFiles++
		}
	}
}

func surfacePattern(root string, definition Definition) string {
	if definition.Root == rootTemp {
		return filepath.Join(os.TempDir(), definition.PathPattern)
	}

	return filepath.Join(root, filepath.FromSlash(definition.PathPattern))
}

func scopeSet(scopes []string) map[string]bool {
	if len(scopes) == 0 {
		return nil
	}

	values := map[string]bool{}

	for _, scope := range scopes {
		for item := range strings.SplitSeq(scope, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				values[item] = true
			}
		}
	}

	return values
}

func validateScopes(scopes map[string]bool) error {
	known := definitionIDs()
	for scope := range scopes {
		if !known[scope] {
			return fmt.Errorf("%w: %q", errUnknownOutputScope, scope)
		}
	}

	return nil
}

func pathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return relative == "." ||
		(!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..")
}

func candidateKind(info fs.FileInfo) string {
	if info.IsDir() {
		return recordKindDirectory
	}

	return recordKindFile
}

func candidateSize(path string, info fs.FileInfo) (int64, error) {
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64

	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk candidate size: %w", err)
		}

		if entry.IsDir() {
			return nil
		}

		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("stat candidate size entry: %w", infoErr)
		}

		total += entryInfo.Size()

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("calculate candidate size for %s: %w", path, err)
	}

	return total, nil
}

func skippedCandidate(surfaceID, path, reason string) PruneCandidate {
	return PruneCandidate{
		SurfaceID: surfaceID,
		Path:      filepath.ToSlash(path),
		Reason:    reason,
		Skipped:   true,
	}
}

func markUningestedCandidates(
	ctx context.Context,
	root string,
	candidates []PruneCandidate,
) ([]PruneCandidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}

	path := filepath.Join(root, ".coding-ethos", "code-intel.db")

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return markAllCandidatesSkipped(
				candidates,
				"code-intel ingest required: database missing",
			), nil
		}

		return candidates, fmt.Errorf("stat code-intel db before ingest guard: %w", err)
	}

	store, err := codeintel.Open(ctx, path)
	if err != nil {
		return candidates, fmt.Errorf("open code-intel db for ingest guard: %w", err)
	}
	defer store.Close()

	for index := range candidates {
		if candidates[index].Skipped {
			continue
		}

		candidatePath := filepath.FromSlash(candidates[index].Path)

		ingested, checkErr := store.SourcePathIngested(
			ctx,
			candidatePath,
			candidates[index].Kind == recordKindDirectory,
		)
		if checkErr != nil {
			return candidates, fmt.Errorf(
				"check code-intel ingest for %s: %w",
				candidatePath,
				checkErr,
			)
		}

		if !ingested {
			candidates[index].Skipped = true
			candidates[index].Reason = "code-intel ingest required before prune"
		}
	}

	return candidates, nil
}

func markAllCandidatesSkipped(
	candidates []PruneCandidate,
	reason string,
) []PruneCandidate {
	for index := range candidates {
		if candidates[index].Skipped {
			continue
		}

		candidates[index].Skipped = true
		candidates[index].Reason = reason
	}

	return candidates
}

func shouldVacuum(scopes map[string]bool) bool {
	return len(scopes) == 0 || scopes["code_intel_db"]
}

func pruneCodeIntelRows(
	ctx context.Context,
	root string,
	policy SurfaceRetentionPolicy,
	options PruneOptions,
	now time.Time,
) (DBMaintenance, bool, error) {
	olderThan := options.OlderThan
	if olderThan == 0 && policy.RowRetentionDays > 0 {
		olderThan = time.Duration(policy.RowRetentionDays) * hoursPerDay * time.Hour
	}

	if olderThan == 0 {
		return DBMaintenance{}, false, nil
	}

	path := filepath.Join(root, ".coding-ethos", "code-intel.db")

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DBMaintenance{}, false, nil
		}

		return DBMaintenance{}, false, fmt.Errorf(
			"stat code-intel db before row prune: %w",
			err,
		)
	}

	store, err := codeintel.Open(ctx, path)
	if err != nil {
		return DBMaintenance{}, false, fmt.Errorf(
			"open code-intel db for row prune: %w",
			err,
		)
	}
	defer store.Close()

	var summary codeintel.RowPruneSummary
	if options.Apply {
		summary, err = store.PruneRows(ctx, olderThan, now)
	} else {
		summary, err = store.PreviewPruneRows(ctx, olderThan, now)
	}

	if err != nil {
		return DBMaintenance{}, false, fmt.Errorf(
			"prune code-intel rows: %w",
			err,
		)
	}

	return DBMaintenance{
		SurfaceID:          "code_intel_db",
		DeletedTraces:      summary.DeletedTraces,
		DeletedProxyEvents: summary.DeletedProxyEvents,
		CutoffUTC:          summary.CutoffUTC,
	}, true, nil
}

func vacuumCodeIntel(ctx context.Context, root string, report *PruneReport) error {
	path := filepath.Join(root, ".coding-ethos", "code-intel.db")

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat code-intel db before vacuum: %w", err)
	}

	store, err := codeintel.Open(ctx, path)
	if err != nil {
		return fmt.Errorf("open code-intel db for vacuum: %w", err)
	}
	defer store.Close()

	err = store.Vacuum(ctx)
	if err != nil {
		return fmt.Errorf("vacuum code-intel db: %w", err)
	}

	if report != nil {
		appendVacuumMaintenance(report)
	}

	return nil
}

func appendVacuumMaintenance(report *PruneReport) {
	for index := range report.DBMaintenance {
		if report.DBMaintenance[index].SurfaceID == "code_intel_db" {
			report.DBMaintenance[index].Vacuumed = true

			return
		}
	}

	report.DBMaintenance = append(report.DBMaintenance, DBMaintenance{
		SurfaceID: "code_intel_db",
		Vacuumed:  true,
	})
}

func writePruneTrace(root string, report *PruneReport, now time.Time) (string, error) {
	dir := filepath.Join(root, ".coding-ethos", "prune-runs")

	err := os.MkdirAll(dir, pruneTraceDirMode)
	if err != nil {
		return "", fmt.Errorf("create prune trace dir: %w", err)
	}

	path := filepath.Join(
		dir,
		fmt.Sprintf("%s-%d.json", now.UTC().Format("20060102T150405Z"), os.Getpid()),
	)
	report.TracePath = filepath.ToSlash(path)

	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		pruneTraceFileMode,
	)
	if err != nil {
		return "", fmt.Errorf("create prune trace: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(report)
	if err != nil {
		return "", fmt.Errorf("write prune trace: %w", err)
	}

	return filepath.ToSlash(path), nil
}
