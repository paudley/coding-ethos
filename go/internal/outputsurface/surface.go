// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package outputsurface inventories coding-ethos runtime files written to disk.
package outputsurface

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

const (
	recordKindDirectory  = "directory"
	recordKindFile       = "file"
	recordKindGlob       = "glob"
	rootRepo             = "repo"
	rootTemp             = "temp"
	codeIntelDBSurfaceID = "code_intel_db"
	codeIntelDuckDBWALID = "code_intel_duckdb_wal"
	codeIntelEventsID    = "code_intel_events"
	codeIntelLockID      = "code_intel_rebuild_lock"

	// DefaultTempEvidenceMaxAge is the retention age for proxy temp evidence.
	DefaultTempEvidenceMaxAge = 24 * time.Hour
	// DefaultCodeIntelRowRetentionDays is the automatic row retention window for
	// derived code-intelligence trace and proxy-event records.
	DefaultCodeIntelRowRetentionDays = 90
	otherRepoAuditDefinitionCount    = 10
	codeIntelSurfaceDefinitionCount  = 4
	toonReportStaticLines            = 5
	humanReportStaticLines           = 3
	humanSurfaceLineEstimate         = 3
	pruneTOONDBMaintenanceFieldCount = 9
)

// Definition describes one known coding-ethos disk output surface.
type Definition struct {
	ReplayValue          string `json:"replay_value"`
	RetentionClass       string `json:"retention_class"`
	PathPattern          string `json:"path_pattern"`
	Root                 string `json:"root"`
	RecordKind           string `json:"record_kind"`
	Producer             string `json:"producer"`
	Consumers            string `json:"consumers"`
	Sensitivity          string `json:"sensitivity"`
	Description          string `json:"description"`
	DefaultMaxAge        string `json:"default_max_age,omitempty"`
	ID                   string `json:"id"`
	maxAge               time.Duration
	CommandPrune         bool `json:"command_prune"`
	RequiresIngest       bool `json:"requires_ingest"`
	DBMaintenance        bool `json:"db_maintenance"`
	AutomaticPrune       bool `json:"automatic_prune"`
	includeTempByDefault bool
}

// Report summarizes all inspected output surfaces.
type Report struct {
	GeneratedAtUTC string      `json:"generated_at_utc"`
	Root           string      `json:"root"`
	Surfaces       []Inventory `json:"surfaces"`
	IncludeTemp    bool        `json:"include_temp"`
}

// Inventory reports current filesystem state for a surface definition.
type Inventory struct {
	Definition

	DBStats      *codeintel.Stats       `json:"db_stats,omitempty"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	OldestMTime  string                 `json:"oldest_mtime,omitempty"`
	Path         string                 `json:"path,omitempty"`
	LargestFile  string                 `json:"largest_file,omitempty"`
	NewestMTime  string                 `json:"newest_mtime,omitempty"`
	Errors       []string               `json:"errors,omitempty"`
	Retention    SurfaceRetentionPolicy `json:"retention"`
	FileCount    int                    `json:"file_count"`
	DirCount     int                    `json:"dir_count"`
	TotalBytes   int64                  `json:"total_bytes"`
	LargestBytes int64                  `json:"largest_bytes,omitempty"`
	StaleCount   int                    `json:"stale_count"`
	Exists       bool                   `json:"exists"`
}

// Options controls inventory collection.
type Options struct {
	Settings    *Settings
	Now         time.Time
	Root        string
	IncludeTemp bool
}

// Definitions returns the canonical output surface registry.
func Definitions() []Definition {
	definitions := repoAuditDefinitions()
	definitions = append(definitions, repoStateDefinitions()...)
	definitions = append(definitions, tempDefinitions()...)

	return definitions
}

func repoAuditDefinitions() []Definition {
	definitions := hookAuditDefinitions()
	definitions = append(definitions, otherRepoAuditDefinitions()...)

	return definitions
}

func hookAuditDefinitions() []Definition {
	return []Definition{
		repoDir(
			"hook_runs",
			".coding-ethos/hook-runs",
			"Hook run logs, metadata, event traces, and debug logs.",
			"go/internal/hooklog",
			"hook-log summary/analyze; code-intel ingest-traces",
			"high",
			"high",
			"audit_evidence",
			true,
		),
		repoGlob(
			"hook_stdout_logs",
			".coding-ethos/hook-runs/*/stdout.log",
			"Hook command stdout logs.",
			"go/internal/hooklog",
			"hook-log summary/analyze",
			"high",
			"medium",
			true,
		),
		repoGlob(
			"hook_stderr_logs",
			".coding-ethos/hook-runs/*/stderr.log",
			"Hook command stderr logs.",
			"go/internal/hooklog",
			"hook-log summary/analyze",
			"high",
			"medium",
			true,
		),
		repoGlob(
			"hook_metadata_env",
			".coding-ethos/hook-runs/*/metadata.env",
			"Hook run metadata environment files.",
			"go/internal/hooklog",
			"hook-log summary",
			"medium",
			"medium",
			true,
		),
		repoGlob(
			"hook_event_traces",
			".coding-ethos/hook-runs/*/event.json",
			"Agent hook event traces.",
			"go/internal/hooks",
			"code-intel ingest-traces",
			"high",
			"high",
			true,
		),
		repoGlob(
			"hook_debug_logs",
			".coding-ethos/hook-runs/*/debug.log",
			"Structured hook debug logs.",
			"go/internal/debuglog",
			"operator debugging",
			"high",
			"medium",
			false,
		),
	}
}

func otherRepoAuditDefinitions() []Definition {
	definitions := make([]Definition, 0, otherRepoAuditDefinitionCount)
	definitions = append(definitions,
		repoDir(
			"lint_traces",
			".coding-ethos/lint-runs",
			"Normalized managed lint traces.",
			"go/internal/lint",
			"policy-lint replay/analyze; code-intel ingest-traces",
			"high",
			"high",
			"audit_evidence",
			true,
		),
		repoDir(
			"prune_traces",
			".coding-ethos/prune-runs",
			"Output surface prune run traces.",
			"go/internal/outputsurface",
			"operator audit and later lifecycle analysis",
			"medium",
			"medium",
			"audit_evidence",
			false,
		),
	)
	definitions = append(definitions, codeIntelSurfaceDefinitions()...)
	definitions = append(definitions, repoFile(
		"ci_sarif_artifact",
		"coding-ethos.sarif",
		"Local CI SARIF artifact path.",
		"go/cmd/coding-ethos-run",
		"GitHub/GitLab code scanning",
		"high",
		"audit_evidence",
		true,
		false,
		false,
	))

	return definitions
}

func codeIntelSurfaceDefinitions() []Definition {
	definitions := make([]Definition, 0, codeIntelSurfaceDefinitionCount)
	definitions = append(definitions, codeIntelStoreSurfaceDefinitions()...)
	definitions = append(definitions, codeIntelSidecarSurfaceDefinitions()...)
	eventLogs := repoDir(
		codeIntelEventsID,
		".coding-ethos/events",
		"Append-only code intelligence event logs.",
		"go/internal/codeintel",
		"DuckDB rebuild and downstream-analysis",
		"medium",
		"high",
		"audit_evidence",
		false,
	)
	eventLogs.AutomaticPrune = true
	definitions = append(definitions, eventLogs)

	return definitions
}

func codeIntelStoreSurfaceDefinitions() []Definition {
	return []Definition{
		repoFile(
			codeIntelDBSurfaceID,
			".coding-ethos/code-intel.duckdb",
			"Repo-local code intelligence DuckDB query index and row-retention surface.",
			"go/internal/codeintel",
			"code-intel CLI, MCP, and downstream-analysis",
			"high",
			"derived_index",
			false,
			true,
			true,
		),
	}
}

func codeIntelSidecarSurfaceDefinitions() []Definition {
	return []Definition{
		repoFile(
			codeIntelDuckDBWALID,
			".coding-ethos/code-intel.duckdb.wal",
			"Repo-local code intelligence DuckDB write-ahead log.",
			"go/internal/codeintel",
			"code-intel CLI and downstream-analysis",
			"medium",
			"derived_index",
			false,
			false,
			false,
		),
		repoFile(
			codeIntelLockID,
			".coding-ethos/code-intel-rebuild.lock",
			"Repo-local code-intel rebuild process lock.",
			"go/internal/codeintel",
			"code-intel rebuild coordination",
			"low",
			"ephemeral",
			true,
			true,
			false,
		),
	}
}

func repoStateDefinitions() []Definition {
	definitions := repoCacheDefinitions()
	definitions = append(definitions, repoMutableStateDefinitions()...)

	return definitions
}

func repoCacheDefinitions() []Definition {
	const (
		hoursPerDay                = 24
		modernWebGuidanceCacheDays = 30
	)

	modernWebGuidance := repoDir(
		"modern_web_guidance_cache",
		".coding-ethos/cache/modern-web-guidance",
		"Modern Web Guidance response, npm, and provenance cache.",
		"go/internal/webguidance",
		"web-guidance CLI and MCP tools",
		"medium",
		"low",
		"cache",
		false,
	)
	modernWebGuidance.AutomaticPrune = true
	modernWebGuidance.maxAge = modernWebGuidanceCacheDays * hoursPerDay * time.Hour

	return []Definition{
		repoDir(
			"sandbox_tmp",
			".coding-ethos/cache/sandbox-tmp",
			"Managed sandbox temporary write area.",
			"go/internal/managedcapture",
			"managed tool runtime",
			"medium",
			"low",
			"ephemeral",
			false,
		),
		repoDir(
			"sandbox_go_cache",
			".coding-ethos/cache/go-build",
			"Managed Go build cache.",
			"go/internal/managedcapture",
			"managed Go tooling",
			"low",
			"none",
			"cache",
			false,
		),
		repoDir(
			"sandbox_golangci_cache",
			".coding-ethos/cache/golangci-lint",
			"Managed golangci-lint cache.",
			"go/internal/managedcapture",
			"managed Go linting",
			"low",
			"none",
			"cache",
			false,
		),
		repoDir(
			"agent_shell_cache",
			".coding-ethos/cache/agent-shell",
			"Agent shell temporary runtime assets.",
			"go/cmd/coding-ethos-run",
			"agent-shell runtime",
			"medium",
			"low",
			"ephemeral",
			false,
		),
		modernWebGuidance,
		repoDir(
			"runtime_cache",
			".coding-ethos/cache",
			"Generated runtime cache root.",
			"go/internal/hookrunnercli",
			"runtime and Gemini cache users",
			"medium",
			"low",
			"cache",
			false,
		),
	}
}

func repoMutableStateDefinitions() []Definition {
	return []Definition{
		repoDir(
			"agent_shell_state",
			".coding-ethos/state/agent-shell-tools",
			"Reusable agent-shell managed tool copies.",
			"go/internal/managedcapture",
			"agent-shell runtime",
			"medium",
			"medium",
			"state",
			false,
		),
		repoFile(
			"commit_head_state",
			".coding-ethos/state/commit-head.json",
			"Pending commit HEAD advancement state.",
			"go/internal/evaluators",
			"git.commit_head_advanced policy",
			"medium",
			"state",
			true,
			false,
			false,
		),
	}
}

func tempDefinitions() []Definition {
	return []Definition{
		tempGlob(
			"proxy_temp_evidence",
			"coding-ethos-tool-output-*.log",
			"Full proxy tool output evidence files in the OS temp directory.",
			"go/internal/agentproxy",
			"visible proxy output markers; proxy transform metadata",
			"high",
			"medium",
			"ephemeral",
			DefaultTempEvidenceMaxAge,
		),
		tempGlob(
			"process_state_locks",
			"coding-ethos-*.lock",
			"Process-global test lock files in the OS temp directory.",
			"go/internal/testlock",
			"Go tests",
			"low",
			"none",
			"ephemeral",
			0,
		),
	}
}

// BuildReport inventories all registered surfaces.
func BuildReport(ctx context.Context, options Options) (Report, error) {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		root = "."
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("resolve output report root: %w", err)
	}

	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	report := Report{
		GeneratedAtUTC: now.UTC().Format(time.RFC3339),
		Root:           filepath.Clean(absoluteRoot),
		IncludeTemp:    options.IncludeTemp,
	}

	settings := DefaultSettings()
	if options.Settings != nil {
		settings = *options.Settings
	}

	if settings.Prune.Surfaces == nil {
		settings.Prune.Surfaces = DefaultSettings().Prune.Surfaces
	}

	for _, definition := range Definitions() {
		if definition.Root == rootTemp && !options.IncludeTemp &&
			!definition.includeTempByDefault {
			continue
		}

		inventory := inventorySurface(ctx, report.Root, definition, settings, now)
		report.Surfaces = append(report.Surfaces, inventory)
	}

	return report, nil
}

func repoDir(
	surfaceID string,
	path string,
	description string,
	producer string,
	consumers string,
	sensitivity string,
	replayValue string,
	retentionClass string,
	requiresIngest bool,
) Definition {
	return Definition{
		ID:             surfaceID,
		Description:    description,
		PathPattern:    filepath.ToSlash(path),
		Root:           rootRepo,
		RecordKind:     recordKindDirectory,
		Producer:       producer,
		Consumers:      consumers,
		Sensitivity:    sensitivity,
		ReplayValue:    replayValue,
		RetentionClass: retentionClass,
		CommandPrune:   true,
		RequiresIngest: requiresIngest,
	}
}

func repoFile(
	surfaceID string,
	path string,
	description string,
	producer string,
	consumers string,
	replayValue string,
	retentionClass string,
	commandPrune bool,
	automaticPrune bool,
	dbMaintenance bool,
) Definition {
	return Definition{
		ID:             surfaceID,
		Description:    description,
		PathPattern:    filepath.ToSlash(path),
		Root:           rootRepo,
		RecordKind:     recordKindFile,
		Producer:       producer,
		Consumers:      consumers,
		Sensitivity:    "medium",
		ReplayValue:    replayValue,
		RetentionClass: retentionClass,
		CommandPrune:   commandPrune,
		AutomaticPrune: automaticPrune,
		DBMaintenance:  dbMaintenance,
	}
}

func repoGlob(
	surfaceID string,
	path string,
	description string,
	producer string,
	consumers string,
	sensitivity string,
	replayValue string,
	requiresIngest bool,
) Definition {
	return Definition{
		ID:             surfaceID,
		Description:    description,
		PathPattern:    filepath.ToSlash(path),
		Root:           rootRepo,
		RecordKind:     recordKindGlob,
		Producer:       producer,
		Consumers:      consumers,
		Sensitivity:    sensitivity,
		ReplayValue:    replayValue,
		RetentionClass: "audit_evidence",
		RequiresIngest: requiresIngest,
	}
}

func tempGlob(
	surfaceID string,
	pattern string,
	description string,
	producer string,
	consumers string,
	sensitivity string,
	replayValue string,
	retentionClass string,
	maxAge time.Duration,
) Definition {
	definition := Definition{
		ID:             surfaceID,
		Description:    description,
		PathPattern:    pattern,
		Root:           rootTemp,
		RecordKind:     recordKindGlob,
		Producer:       producer,
		Consumers:      consumers,
		Sensitivity:    sensitivity,
		ReplayValue:    replayValue,
		RetentionClass: retentionClass,
		AutomaticPrune: maxAge > 0,
		CommandPrune:   true,
		maxAge:         maxAge,
	}
	if maxAge > 0 {
		definition.DefaultMaxAge = maxAge.String()
	}

	return definition
}

func inventorySurface(
	ctx context.Context,
	root string,
	definition Definition,
	settings Settings,
	now time.Time,
) Inventory {
	inventory := Inventory{
		Definition: definition,
		Retention:  retentionPolicy(settings, definition),
	}

	switch definition.Root {
	case rootRepo:
		path := filepath.Join(root, filepath.FromSlash(definition.PathPattern))

		inventory.Path = path
		if definition.RecordKind == recordKindGlob {
			inspectGlob(path, inventory.Retention, now, &inventory)
		} else {
			inspectPath(path, inventory.Retention, now, &inventory)
		}

		if definition.ID == codeIntelDBSurfaceID && inventory.Exists {
			addCodeIntelStats(ctx, path, &inventory)
		}
	case rootTemp:
		path := filepath.Join(os.TempDir(), definition.PathPattern)
		inventory.Path = path
		inspectGlob(path, inventory.Retention, now, &inventory)
	default:
		inventory.Errors = append(inventory.Errors, "unknown root kind "+definition.Root)
	}

	return inventory
}

func inspectPath(
	path string,
	policy SurfaceRetentionPolicy,
	now time.Time,
	inventory *Inventory,
) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}

		inventory.Errors = append(inventory.Errors, err.Error())

		return
	}

	inventory.Exists = true
	if info.IsDir() {
		inspectDir(path, policy, now, inventory)

		return
	}

	inspectFile(path, info, policy, now, inventory)
}

func inspectGlob(
	pattern string,
	policy SurfaceRetentionPolicy,
	now time.Time,
	inventory *Inventory,
) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		inventory.Errors = append(inventory.Errors, err.Error())

		return
	}

	slices.Sort(matches)

	for _, match := range matches {
		info, statErr := os.Lstat(match)
		if statErr != nil {
			inventory.Errors = append(inventory.Errors, statErr.Error())

			continue
		}

		if info.IsDir() {
			inventory.Exists = true
			inventory.DirCount++

			continue
		}

		inventory.Exists = true
		inspectFile(match, info, policy, now, inventory)
	}
}

func inspectDir(
	path string,
	policy SurfaceRetentionPolicy,
	now time.Time,
	inventory *Inventory,
) {
	inventory.DirCount++

	err := filepath.WalkDir(
		path,
		func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				inventory.Errors = append(inventory.Errors, err.Error())

				return nil
			}

			if current == path {
				return nil
			}

			info, statErr := entry.Info()
			if statErr != nil {
				inventory.Errors = append(inventory.Errors, statErr.Error())

				return nil
			}

			if info.IsDir() {
				inventory.DirCount++

				return nil
			}

			inspectFile(current, info, policy, now, inventory)

			return nil
		},
	)
	if err != nil {
		inventory.Errors = append(inventory.Errors, err.Error())
	}
}

func inspectFile(
	path string,
	info fs.FileInfo,
	policy SurfaceRetentionPolicy,
	now time.Time,
	inventory *Inventory,
) {
	inventory.FileCount++

	inventory.TotalBytes += info.Size()
	if info.Size() > inventory.LargestBytes {
		inventory.LargestBytes = info.Size()
		inventory.LargestFile = filepath.ToSlash(path)
	}

	recordMTime(info.ModTime(), inventory)

	if policy.MaxAge > 0 && now.Sub(info.ModTime()) > policy.MaxAge {
		inventory.StaleCount++
	}
}

func retentionPolicy(settings Settings, definition Definition) SurfaceRetentionPolicy {
	policy, ok := settings.Prune.Surfaces[definition.ID]
	if !ok {
		policy = SurfaceRetentionPolicy{
			Enabled:                definition.CommandPrune || definition.DBMaintenance,
			Auto:                   definition.AutomaticPrune,
			MaxAge:                 definition.maxAge,
			MaxAgeText:             durationText(definition.maxAge),
			RequireCodeIntelIngest: definition.RequiresIngest,
			VacuumAfterPrune:       definition.DBMaintenance,
		}
	}

	if !settings.Prune.Enabled {
		policy.Enabled = false
	}

	if !settings.Prune.AutoEnabled {
		policy.Auto = false
	}

	return policy
}

func recordMTime(modTime time.Time, inventory *Inventory) {
	value := modTime.UTC().Format(time.RFC3339)
	if inventory.OldestMTime == "" || value < inventory.OldestMTime {
		inventory.OldestMTime = value
	}

	if inventory.NewestMTime == "" || value > inventory.NewestMTime {
		inventory.NewestMTime = value
	}
}

func addCodeIntelStats(ctx context.Context, path string, inventory *Inventory) {
	store, err := codeintel.Open(ctx, path)
	if err != nil {
		inventory.Errors = append(inventory.Errors, "read code-intel stats: "+err.Error())

		return
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		inventory.Errors = append(inventory.Errors, "read code-intel stats: "+err.Error())

		return
	}

	inventory.DBStats = &stats
}

// FormatTOON renders a compact operator-facing report.
func FormatTOON(report Report) string {
	lines := make([]string, 0, toonReportStaticLines+len(report.Surfaces))
	lines = append(lines,
		"root: "+toonCell(report.Root),
		"generated_at_utc: "+toonCell(report.GeneratedAtUTC),
		fmt.Sprintf("include_temp: %t", report.IncludeTemp),
		fmt.Sprintf(
			"surfaces[%d]{id,exists,root,kind,files,dirs,bytes,stale,path}:",
			len(report.Surfaces),
		),
	)

	for _, surface := range report.Surfaces {
		lines = append(lines, fmt.Sprintf(
			"  %s,%t,%s,%s,%d,%d,%d,%d,%s",
			toonCell(surface.ID),
			surface.Exists,
			toonCell(surface.Root),
			toonCell(surface.RecordKind),
			surface.FileCount,
			surface.DirCount,
			surface.TotalBytes,
			surface.StaleCount,
			toonCell(surface.Path),
		))
	}

	lines = appendCodeIntelDBStatsTOON(lines, report.Surfaces)

	return strings.Join(lines, "\n") + "\n"
}

// FormatHuman renders a readable report for local operators.
func FormatHuman(report Report) string {
	lines := make(
		[]string,
		0,
		humanReportStaticLines+(len(report.Surfaces)*humanSurfaceLineEstimate),
	)
	lines = append(lines,
		"coding-ethos output surface report",
		"root: "+report.Root,
		"generated_at_utc: "+report.GeneratedAtUTC,
	)

	for _, surface := range report.Surfaces {
		status := "missing"
		if surface.Exists {
			status = "present"
		}

		lines = append(lines, fmt.Sprintf(
			"- %s: %s, files=%d, dirs=%d, bytes=%d, stale=%d, path=%s",
			surface.ID,
			status,
			surface.FileCount,
			surface.DirCount,
			surface.TotalBytes,
			surface.StaleCount,
			surface.Path,
		))
		if surface.DBStats != nil {
			lines = append(lines, fmt.Sprintf(
				"  code-intel: traces=%d hook_events=%d proxy_sessions=%d "+
					"fts_rows=%d health_snapshots=%d health_targets=%d "+
					"health_coverage=%d",
				surface.DBStats.Traces,
				surface.DBStats.HookEvents,
				surface.DBStats.ProxySessions,
				surface.DBStats.FtsRows,
				surface.DBStats.CodeHealthSnapshots,
				surface.DBStats.CodeHealthTargets,
				surface.DBStats.CodeHealthCoverage,
			))
		}

		if len(surface.Errors) > 0 {
			lines = append(lines, "  errors: "+strings.Join(surface.Errors, "; "))
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

func appendCodeIntelDBStatsTOON(lines []string, surfaces []Inventory) []string {
	stats := []Inventory{}

	for _, surface := range surfaces {
		if surface.DBStats != nil {
			stats = append(stats, surface)
		}
	}

	if len(stats) == 0 {
		return lines
	}

	lines = append(lines,
		fmt.Sprintf(
			"code_intel_db_stats[%d]{surface,traces,hook_events,"+
				"proxy_sessions,fts_rows,health_snapshots,health_targets,"+
				"health_coverage}:",
			len(stats),
		),
	)

	for _, surface := range stats {
		lines = append(lines, fmt.Sprintf(
			"  %s,%d,%d,%d,%d,%d,%d,%d",
			toonCell(surface.ID),
			surface.DBStats.Traces,
			surface.DBStats.HookEvents,
			surface.DBStats.ProxySessions,
			surface.DBStats.FtsRows,
			surface.DBStats.CodeHealthSnapshots,
			surface.DBStats.CodeHealthTargets,
			surface.DBStats.CodeHealthCoverage,
		))
	}

	return lines
}

// FormatPruneTOON renders a compact prune report.
func FormatPruneTOON(report PruneReport) string {
	lines := []string{
		"root: " + toonCell(report.Root),
		"generated_at_utc: " + toonCell(report.GeneratedAtUTC),
		fmt.Sprintf("apply: %t", report.Apply),
		pruneSummaryLine(report),
		fmt.Sprintf(
			"candidates[%d]{surface,path,kind,bytes,deleted,skipped,reason}:",
			len(report.Candidates),
		),
	}
	for _, candidate := range report.Candidates {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%d,%t,%t,%s",
			toonCell(candidate.SurfaceID),
			toonCell(candidate.Path),
			toonCell(candidate.Kind),
			candidate.Bytes,
			candidate.Deleted,
			candidate.Skipped,
			toonCell(candidate.Reason),
		))
	}

	if report.TracePath != "" {
		lines = append(lines, "trace_path: "+toonCell(report.TracePath))
	}

	appendPruneTOONDBMaintenance(&lines, report.DBMaintenance)

	if len(report.LockMaintenance) > 0 {
		lines = append(lines, fmt.Sprintf(
			"lock_maintenance[%d]{path,pid,age_seconds,stale,removed,reason}:",
			len(report.LockMaintenance),
		))
		for _, maintenance := range report.LockMaintenance {
			lines = append(lines, fmt.Sprintf(
				"  %s,%d,%d,%t,%t,%s",
				toonCell(maintenance.Path),
				maintenance.PID,
				maintenance.AgeSeconds,
				maintenance.Stale,
				maintenance.Removed,
				toonCell(maintenance.Reason),
			))
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// FormatPruneHuman renders a readable prune report.
func FormatPruneHuman(report PruneReport) string {
	mode := "dry-run"
	if report.Apply {
		mode = "apply"
	}

	lines := []string{
		"coding-ethos output prune report",
		"root: " + report.Root,
		"generated_at_utc: " + report.GeneratedAtUTC,
		"mode: " + mode,
		pruneSummaryLine(report),
	}
	for _, candidate := range report.Candidates {
		action := "would delete"
		if candidate.Deleted {
			action = "deleted"
		}

		if candidate.Skipped {
			action = "skipped"
		}

		lines = append(lines, fmt.Sprintf(
			"- %s %s %s bytes=%d reason=%s",
			candidate.SurfaceID,
			action,
			candidate.Path,
			candidate.Bytes,
			candidate.Reason,
		))
	}

	if report.TracePath != "" {
		lines = append(lines, "trace_path: "+report.TracePath)
	}

	appendPruneHumanDBMaintenance(&lines, report.DBMaintenance)

	for _, maintenance := range report.LockMaintenance {
		action := "retained"
		if maintenance.Removed {
			action = "removed"
		}

		lines = append(lines, fmt.Sprintf(
			"- code-intel rebuild lock %s path=%s pid=%d age_seconds=%d stale=%t reason=%s",
			action,
			maintenance.Path,
			maintenance.PID,
			maintenance.AgeSeconds,
			maintenance.Stale,
			maintenance.Reason,
		))
	}

	if len(report.Errors) > 0 {
		lines = append(lines, "errors: "+strings.Join(report.Errors, "; "))
	}

	return strings.Join(lines, "\n") + "\n"
}

func pruneSummaryLine(report PruneReport) string {
	return fmt.Sprintf(
		"summary: candidates=%d deleted_files=%d deleted_dirs=%d deleted_bytes=%d skipped=%d",
		len(report.Candidates),
		report.DeletedFiles,
		report.DeletedDirs,
		report.DeletedBytes,
		report.Skipped,
	)
}

func appendPruneTOONDBMaintenance(lines *[]string, maintenances []DBMaintenance) {
	if len(maintenances) == 0 {
		return
	}

	*lines = append(*lines, fmt.Sprintf(
		"db_maintenance[%d]{surface,deleted_traces,deleted_proxy_events,"+
			"vacuumed,checkpointed,compacted,cutoff,size_before,size_after}:",
		len(maintenances),
	))
	for _, maintenance := range maintenances {
		*lines = append(*lines, pruneTOONDBMaintenanceLine(maintenance))
	}
}

func pruneTOONDBMaintenanceLine(maintenance DBMaintenance) string {
	values := make([]string, 0, pruneTOONDBMaintenanceFieldCount)
	values = append(values,
		toonCell(maintenance.SurfaceID),
		strconv.Itoa(maintenance.DeletedTraces),
		strconv.Itoa(maintenance.DeletedProxyEvents),
		strconv.FormatBool(maintenance.Vacuumed),
		strconv.FormatBool(maintenance.Checkpointed),
		strconv.FormatBool(maintenance.Compacted),
		toonCell(maintenance.CutoffUTC),
	)

	sizeBefore := ""
	sizeAfter := ""

	if maintenance.SizeBeforeBytes > 0 {
		sizeBefore = strconv.FormatInt(maintenance.SizeBeforeBytes, 10)
	}

	if maintenance.SizeAfterBytes > 0 {
		sizeAfter = strconv.FormatInt(maintenance.SizeAfterBytes, 10)
	}

	values = append(values, sizeBefore, sizeAfter)

	return "  " + strings.Join(values, ",")
}

func appendPruneHumanDBMaintenance(lines *[]string, maintenances []DBMaintenance) {
	for _, maintenance := range maintenances {
		parts := []string{
			fmt.Sprintf("- %s db:", maintenance.SurfaceID),
			"deleted_traces=" + strconv.Itoa(maintenance.DeletedTraces),
			"deleted_proxy_events=" + strconv.Itoa(maintenance.DeletedProxyEvents),
			"vacuumed=" + strconv.FormatBool(maintenance.Vacuumed),
			"checkpointed=" + strconv.FormatBool(maintenance.Checkpointed),
			"compacted=" + strconv.FormatBool(maintenance.Compacted),
		}

		if maintenance.SizeBeforeBytes > 0 || maintenance.SizeAfterBytes > 0 {
			parts = append(
				parts,
				"size_before="+strconv.FormatInt(maintenance.SizeBeforeBytes, 10),
				"size_after="+strconv.FormatInt(maintenance.SizeAfterBytes, 10),
			)
		}

		if maintenance.CutoffUTC != "" {
			parts = append(parts, "cutoff="+maintenance.CutoffUTC)
		}

		*lines = append(*lines, strings.Join(parts, " "))
	}
}

func toonCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, ",", "\\,")

	return value
}

// SortInventories orders inventories by surface ID. It is intended for tests.
func SortInventories(items []Inventory) {
	sort.Slice(items, func(left, right int) bool {
		return items[left].ID < items[right].ID
	})
}
