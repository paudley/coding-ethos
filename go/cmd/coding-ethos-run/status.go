// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

const (
	operatorStatusKind          = "operator_status"
	operatorStatusPass          = "PASS"
	operatorStatusWarn          = "WARN"
	operatorStatusBlocked       = "BLOCKED"
	operatorStatusRecentRunSize = 20
	statusDirMode               = 0o755
	statusFileMode              = 0o600
	hookRuntimeCheckCount       = 2
	toonStatusStaticLines       = 13
	humanStatusStaticLines      = 7
	humanRecommendationHeader   = 2
)

var errUnsupportedStatusFormat = apperror.StaticError("unsupported status format")

type operatorStatusReport struct {
	GeneratedAtUTC     string                `json:"generated_at_utc"`
	Kind               string                `json:"kind"`
	Root               string                `json:"root"`
	Status             string                `json:"status"`
	Summary            string                `json:"summary"`
	Checks             []operatorStatusCheck `json:"checks"`
	Recommendations    []string              `json:"recommendations,omitempty"`
	OutputSurfaceTotal int                   `json:"output_surface_total"`
	OutputSurfaceStale int                   `json:"output_surface_stale"`
	OutputSurfaceError int                   `json:"output_surface_error"`
	RecentHookRuns     int                   `json:"recent_hook_runs"`
	RecentHookFailures int                   `json:"recent_hook_failures"`
	HookReviews        int                   `json:"hook_reviews"`
	FalsePositives     int                   `json:"false_positives"`
}

type operatorStatusCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func runStatusHandler(paths runtimePaths, rest []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "toon", "Output format: toon, human, or json")
	writePath := flags.String("write", "", "Write a portable human handoff report")
	includeTemp := flags.Bool("include-temp", false, "Include OS temp output surfaces")

	err := flags.Parse(rest)
	if err != nil {
		return fmt.Errorf("parse status flags: %w", err)
	}

	report, err := buildOperatorStatus(context.Background(), paths, *includeTemp)
	if err != nil {
		return fmt.Errorf("build operator status: %w", err)
	}

	if strings.TrimSpace(*writePath) != "" {
		err = writeOperatorStatusFile(*writePath, formatOperatorStatusHuman(report))
		if err != nil {
			return err
		}
	}

	return writeOperatorStatus(os.Stdout, report, *format)
}

func buildOperatorStatus(
	ctx context.Context,
	paths runtimePaths,
	includeTemp bool,
) (operatorStatusReport, error) {
	now := time.Now().UTC()

	surfaceReport, err := outputsurface.BuildReport(ctx, outputsurface.Options{
		Root:        paths.Root,
		IncludeTemp: includeTemp,
		Now:         now,
	})
	if err != nil {
		return operatorStatusReport{}, fmt.Errorf("build output surface report: %w", err)
	}

	hookRuns, hookFailures := recentHookRunCounts(paths.Root, operatorStatusRecentRunSize)
	hookReviews, falsePositives := hookReviewCounts(ctx, paths.Root)
	report := operatorStatusReport{
		GeneratedAtUTC:     now.Format(time.RFC3339),
		Kind:               operatorStatusKind,
		Root:               filepath.Clean(paths.Root),
		Status:             operatorStatusPass,
		OutputSurfaceTotal: len(surfaceReport.Surfaces),
		OutputSurfaceStale: outputSurfaceStaleCount(surfaceReport.Surfaces),
		OutputSurfaceError: outputSurfaceErrorCount(surfaceReport.Surfaces),
		RecentHookRuns:     hookRuns,
		RecentHookFailures: hookFailures,
		HookReviews:        hookReviews,
		FalsePositives:     falsePositives,
	}

	report.Checks = append(report.Checks, runtimeArtifactChecks(paths)...)
	report.Checks = append(report.Checks, agentAPIProxyRoutingCheck())
	report.Checks = append(report.Checks, outputSurfaceChecks(surfaceReport)...)
	report.Checks = append(
		report.Checks,
		hookRuntimeChecks(hookRuns, hookFailures, hookReviews, falsePositives)...,
	)
	report.Status = aggregateOperatorStatus(report.Checks)
	report.Recommendations = operatorStatusRecommendations(report)
	report.Summary = operatorStatusSummary(report)

	return report, nil
}

func runtimeArtifactChecks(paths runtimePaths) []operatorStatusCheck {
	checks := []operatorStatusCheck{
		fileStatusCheck("policy_bundle", paths.PolicyBundle),
		fileStatusCheck("policy_metadata", paths.PolicyMetadata),
		fileStatusCheck("hook_runner", paths.GitHookRunner),
		fileStatusCheck("managed_toolchain_manifest", paths.ManagedManifest),
		directoryStatusCheck("hooks_dir", paths.HooksDir),
	}

	return checks
}

func agentAPIProxyRoutingCheck() operatorStatusCheck {
	enabled := strings.TrimSpace(os.Getenv(envAgentAPIProxyEnabled)) == "1"
	proxyURL := strings.TrimSpace(os.Getenv(envAgentAPIProxyURL))

	if !enabled {
		return operatorStatusCheck{
			Name:   "agent_api_proxy",
			Status: operatorStatusPass,
			Detail: "routing disabled",
		}
	}

	if proxyURL == "" {
		return operatorStatusCheck{
			Name:   "agent_api_proxy",
			Status: operatorStatusWarn,
			Detail: "routing enabled without CODE_ETHOS_AGENT_API_PROXY_URL",
		}
	}

	if !validAgentAPIProxyURL(proxyURL) {
		return operatorStatusCheck{
			Name:   "agent_api_proxy",
			Status: operatorStatusWarn,
			Detail: "routing enabled with invalid CODE_ETHOS_AGENT_API_PROXY_URL",
		}
	}

	return operatorStatusCheck{
		Name:   "agent_api_proxy",
		Status: operatorStatusPass,
		Detail: "routing enabled via explicit proxy URL",
	}
}

func validAgentAPIProxyURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}

	return parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func outputSurfaceChecks(report outputsurface.Report) []operatorStatusCheck {
	stale := outputSurfaceStaleCount(report.Surfaces)
	errors := outputSurfaceErrorCount(report.Surfaces)
	codeIntelDB, codeIntelFound := outputSurfaceByID(report, "code_intel_db")
	checks := []operatorStatusCheck{
		{
			Name:   "output_surfaces",
			Status: statusForCount(errors, stale),
			Detail: fmt.Sprintf(
				"surfaces=%d stale=%d errors=%d",
				len(report.Surfaces),
				stale,
				errors,
			),
		},
	}

	switch {
	case !codeIntelFound || !codeIntelDB.Exists:
		checks = append(checks, operatorStatusCheck{
			Name:   "code_intel_db",
			Status: operatorStatusWarn,
			Detail: "code-intel DuckDB store is missing; run code-intel rebuild-index",
		})
	case codeIntelDB.DBStats == nil:
		checks = append(checks, operatorStatusCheck{
			Name:   "code_intel_db",
			Status: operatorStatusWarn,
			Detail: "code-intel DuckDB store exists but stats were unavailable",
		})
	default:
		checks = append(checks, operatorStatusCheck{
			Name:   "code_intel_db",
			Status: operatorStatusPass,
			Detail: fmt.Sprintf(
				"files=%d chunks=%d traces=%d health_snapshots=%d",
				codeIntelDB.DBStats.Files,
				codeIntelDB.DBStats.CodeChunks,
				codeIntelDB.DBStats.Traces,
				codeIntelDB.DBStats.CodeHealthSnapshots,
			),
		})
	}

	return checks
}

func hookRuntimeChecks(
	runs int,
	failures int,
	hookReviews int,
	falsePositives int,
) []operatorStatusCheck {
	status := operatorStatusPass
	detail := fmt.Sprintf("recent_runs=%d recent_failures=%d", runs, failures)

	if failures > 0 {
		status = operatorStatusWarn
	}

	checks := make([]operatorStatusCheck, 0, hookRuntimeCheckCount)
	checks = append(checks, operatorStatusCheck{
		Name:   "recent_hooks",
		Status: status,
		Detail: detail,
	})

	reviewStatus := operatorStatusPass
	if hookReviews > 0 || falsePositives > 0 {
		reviewStatus = operatorStatusWarn
	}

	checks = append(checks, operatorStatusCheck{
		Name:   "hook_reviews",
		Status: reviewStatus,
		Detail: fmt.Sprintf(
			"reviews=%d false_positives=%d",
			hookReviews,
			falsePositives,
		),
	})

	return checks
}

func fileStatusCheck(name, path string) operatorStatusCheck {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return operatorStatusCheck{
			Name:   name,
			Status: operatorStatusPass,
			Detail: path,
		}
	}

	return missingStatusCheck(name, path)
}

func directoryStatusCheck(name, path string) operatorStatusCheck {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return operatorStatusCheck{
			Name:   name,
			Status: operatorStatusPass,
			Detail: path,
		}
	}

	return missingStatusCheck(name, path)
}

func missingStatusCheck(name, path string) operatorStatusCheck {
	return operatorStatusCheck{
		Name:   name,
		Status: operatorStatusBlocked,
		Detail: "missing: " + path,
	}
}

func outputSurfaceStaleCount(surfaces []outputsurface.Inventory) int {
	total := 0
	for _, surface := range surfaces {
		total += surface.StaleCount
	}

	return total
}

func outputSurfaceErrorCount(surfaces []outputsurface.Inventory) int {
	total := 0
	for _, surface := range surfaces {
		total += len(surface.Errors)
	}

	return total
}

func outputSurfaceByID(
	report outputsurface.Report,
	id string,
) (outputsurface.Inventory, bool) {
	for _, surface := range report.Surfaces {
		if surface.ID == id {
			return surface, true
		}
	}

	return outputsurface.Inventory{}, false
}

func statusForCount(errors, stale int) string {
	switch {
	case errors > 0:
		return operatorStatusWarn
	case stale > 0:
		return operatorStatusWarn
	default:
		return operatorStatusPass
	}
}

func aggregateOperatorStatus(checks []operatorStatusCheck) string {
	status := operatorStatusPass

	for _, check := range checks {
		switch check.Status {
		case operatorStatusBlocked:
			return operatorStatusBlocked
		case operatorStatusWarn:
			status = operatorStatusWarn
		}
	}

	return status
}

func operatorStatusRecommendations(report operatorStatusReport) []string {
	recommendations := []string{}

	if report.Status == operatorStatusBlocked {
		recommendations = append(
			recommendations,
			"Run make build to regenerate runtime artifacts.",
		)
	}

	if report.OutputSurfaceStale > 0 {
		recommendations = append(
			recommendations,
			"Run coding-ethos-run output prune --all --apply.",
		)
	}

	if report.RecentHookFailures > 0 {
		recommendations = append(
			recommendations,
			"Inspect coding-ethos-run output report and recent hook-runs.",
		)
	}

	if report.HookReviews > 0 || report.FalsePositives > 0 {
		recommendations = append(
			recommendations,
			"Review code-intel hook-reviews before clearing operator handoff.",
		)
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "No operator action required.")
	}

	return recommendations
}

func operatorStatusSummary(report operatorStatusReport) string {
	return fmt.Sprintf(
		"%s: checks=%d surfaces=%d stale=%d hook_failures=%d hook_reviews=%d",
		report.Status,
		len(report.Checks),
		report.OutputSurfaceTotal,
		report.OutputSurfaceStale,
		report.RecentHookFailures,
		report.HookReviews,
	)
}

func recentHookRunCounts(root string, limit int) (int, int) {
	entries, err := os.ReadDir(filepath.Join(root, ".coding-ethos", "hook-runs"))
	if err != nil {
		return 0, 0
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	slices.Sort(names)
	slices.Reverse(names)

	if len(names) > limit {
		names = names[:limit]
	}

	failures := 0

	for _, name := range names {
		exitCode, found := hookRunExitCode(
			filepath.Join(root, ".coding-ethos", "hook-runs", name, "metadata.env"),
		)
		if found && exitCode != 0 {
			failures++
		}
	}

	return len(names), failures
}

func hookReviewCounts(ctx context.Context, root string) (int, int) {
	dbPath := codeintel.DefaultDBPath(root)

	info, err := os.Stat(dbPath)
	if err != nil || info.IsDir() {
		return 0, 0
	}

	store, err := codeintel.Open(ctx, dbPath)
	if err != nil {
		return 0, 0
	}

	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		return 0, 0
	}

	falsePositiveReviews, err := store.HookReviews(ctx, codeintel.HookReviewQuery{
		Disposition: "false_positive",
		Limit:       stats.HookReviews + 1,
	})
	if err != nil {
		return stats.HookReviews, 0
	}

	return stats.HookReviews, len(falsePositiveReviews)
}

func hookRunExitCode(path string) (int, bool) {
	// #nosec G304 -- path is derived from the configured repo root and known
	// hook-run metadata layout.
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}

	for line := range strings.Lines(string(content)) {
		value, found := strings.CutPrefix(line, "exit_code=")
		if !found {
			continue
		}

		parsed, err := strconv.Atoi(strings.Trim(strings.TrimSpace(value), "'\""))
		if err != nil {
			return 0, false
		}

		return parsed, true
	}

	return 0, false
}

func writeOperatorStatusFile(path, content string) error {
	cleanPath := filepath.Clean(path)

	err := os.MkdirAll(filepath.Dir(cleanPath), statusDirMode)
	if err != nil {
		return fmt.Errorf("create status report parent %s: %w", cleanPath, err)
	}

	err = os.WriteFile(cleanPath, []byte(content), statusFileMode)
	if err != nil {
		return fmt.Errorf("write status report %s: %w", path, err)
	}

	return nil
}

func writeOperatorStatus(
	output *os.File,
	report operatorStatusReport,
	format string,
) error {
	switch format {
	case "json":
		err := feedback.WriteJSON(output, report)
		if err != nil {
			return fmt.Errorf("write status JSON: %w", err)
		}

		return nil
	case "human":
		err := feedback.WriteRendered(
			output,
			formatOperatorStatusHuman(report),
			feedback.FormatHuman,
		)
		if err != nil {
			return fmt.Errorf("write status human: %w", err)
		}

		return nil
	case "", "toon":
		err := feedback.WriteRendered(
			output,
			formatOperatorStatusTOON(report),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write status TOON: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedStatusFormat, format)
	}
}

func formatOperatorStatusTOON(report operatorStatusReport) string {
	lines := make(
		[]string,
		0,
		toonStatusStaticLines+len(report.Checks)+len(report.Recommendations),
	)
	lines = append(lines,
		"kind: "+toonCell(report.Kind),
		"root: "+toonCell(report.Root),
		"generated_at_utc: "+toonCell(report.GeneratedAtUTC),
		"status: "+toonCell(report.Status),
		"summary: "+toonCell(report.Summary),
		fmt.Sprintf("output_surface_total: %d", report.OutputSurfaceTotal),
		fmt.Sprintf("output_surface_stale: %d", report.OutputSurfaceStale),
		fmt.Sprintf("output_surface_error: %d", report.OutputSurfaceError),
		fmt.Sprintf("recent_hook_runs: %d", report.RecentHookRuns),
		fmt.Sprintf("recent_hook_failures: %d", report.RecentHookFailures),
		fmt.Sprintf("hook_reviews: %d", report.HookReviews),
		fmt.Sprintf("false_positives: %d", report.FalsePositives),
		fmt.Sprintf("checks[%d]{name,status,detail}:", len(report.Checks)),
	)

	for _, check := range report.Checks {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s",
			toonCell(check.Name),
			toonCell(check.Status),
			toonCell(check.Detail),
		))
	}

	lines = append(lines, fmt.Sprintf(
		"recommendations[%d]{message}:",
		len(report.Recommendations),
	))
	for _, recommendation := range report.Recommendations {
		lines = append(lines, "  "+toonCell(recommendation))
	}

	return strings.Join(lines, "\n")
}

func formatOperatorStatusHuman(report operatorStatusReport) string {
	lines := make(
		[]string,
		0,
		humanStatusStaticLines+len(report.Checks)+humanRecommendationHeader+
			len(report.Recommendations),
	)
	lines = append(lines,
		"coding-ethos operator status",
		"root: "+report.Root,
		"generated_at_utc: "+report.GeneratedAtUTC,
		"status: "+report.Status,
		"summary: "+report.Summary,
		"",
		"Checks:",
	)

	for _, check := range report.Checks {
		lines = append(lines, fmt.Sprintf(
			"- %s: %s - %s",
			check.Name,
			check.Status,
			check.Detail,
		))
	}

	lines = append(lines, "", "Recommendations:")
	for _, recommendation := range report.Recommendations {
		lines = append(lines, "- "+recommendation)
	}

	return strings.Join(lines, "\n") + "\n"
}

func toonCell(value string) string {
	return feedback.Cell(value)
}
