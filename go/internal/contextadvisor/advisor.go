// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package contextadvisor turns existing telemetry into compact token-economy
// guidance. It is advisory only; callers must not use this package for
// enforcement decisions.
package contextadvisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

const (
	Kind       = "context_token_economy"
	StatusOK   = "OK"
	StatusWarn = "WARN"
	StatusHigh = "HIGH"

	defaultWarningFileReads         = 8
	defaultHighFileReads            = 20
	defaultWarningFileListings      = 3
	defaultHighFileListings         = 8
	defaultWarningToolCalls         = 10
	defaultHighToolCalls            = 25
	defaultWarningTruncations       = 1
	defaultHighTruncations          = 3
	defaultWarningOutputCompression = 1
	defaultHighOutputCompression    = 4
	defaultWarningSpillFiles        = 1
	defaultHighSpillFiles           = 5
	defaultWarningStaleSurfaces     = 1
	defaultHighStaleSurfaces        = 5
	defaultWarningTotalTokens       = 16000
	defaultHighTotalTokens          = 48000

	contextAdvisorTOONBaseLineCount = 16

	adviceFileReads          = "repeated_file_reads"
	adviceBroadReads         = "broad_reads"
	adviceCodeIntelFreshness = "code_intel_freshness"
	adviceToolCalls          = "tool_surface_pressure"
	adviceTruncations        = "proxy_truncations"
	adviceCompression        = "output_compression"
	adviceSpillFiles         = "spill_files"
	adviceStaleSurfaces      = "stale_output_surfaces"
	adviceTotalTokens        = "token_volume"
)

// Thresholds control when advisory context pressure messages appear.
type Thresholds struct {
	WarningFileReads         int  `json:"warning_file_reads"`
	HighFileReads            int  `json:"high_file_reads"`
	WarningFileListings      int  `json:"warning_file_listings"`
	HighFileListings         int  `json:"high_file_listings"`
	WarningToolCalls         int  `json:"warning_tool_calls"`
	HighToolCalls            int  `json:"high_tool_calls"`
	WarningTruncations       int  `json:"warning_truncations"`
	HighTruncations          int  `json:"high_truncations"`
	WarningOutputCompression int  `json:"warning_output_compression"`
	HighOutputCompression    int  `json:"high_output_compression"`
	WarningSpillFiles        int  `json:"warning_spill_files"`
	HighSpillFiles           int  `json:"high_spill_files"`
	WarningStaleSurfaces     int  `json:"warning_stale_surfaces"`
	HighStaleSurfaces        int  `json:"high_stale_surfaces"`
	WarningTotalTokens       int  `json:"warning_total_tokens"`
	HighTotalTokens          int  `json:"high_total_tokens"`
	Enabled                  bool `json:"enabled"`
}

// Metrics is the compact telemetry subset used by the advisor.
type Metrics struct {
	CodeIntelFreshness     string `json:"code_intel_freshness"`
	OutputSurfaces         int    `json:"output_surfaces"`
	StaleOutputSurfaces    int    `json:"stale_output_surfaces"`
	OutputSurfaceBytes     int64  `json:"output_surface_bytes"`
	SpillFiles             int    `json:"spill_files"`
	ProxySessions          int    `json:"proxy_sessions"`
	ProxyEvents            int    `json:"proxy_events"`
	FileReads              int    `json:"file_reads"`
	FileListings           int    `json:"file_listings"`
	ToolCalls              int    `json:"tool_calls"`
	Truncations            int    `json:"truncations"`
	OutputCompression      int    `json:"output_compression"`
	CacheHits              int    `json:"cache_hits"`
	TotalTokens            int    `json:"total_tokens"`
	LinkedTraceCount       int    `json:"linked_trace_count"`
	CodeIntelTraceCount    int    `json:"code_intel_trace_count"`
	CodeIntelProxyEvents   int    `json:"code_intel_proxy_events"`
	CodeIntelStoreReady    bool   `json:"code_intel_store_ready"`
	NoEnforcementDecisions bool   `json:"no_enforcement_decisions"`
}

// Advice is one compact recommendation triggered by threshold evidence.
type Advice struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Signal         string `json:"signal"`
	Detail         string `json:"detail"`
	Recommendation string `json:"recommendation"`
}

// Report is the JSON/TOON payload emitted by the context advisor.
type Report struct {
	GeneratedAtUTC string     `json:"generated_at_utc"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	Summary        string     `json:"summary"`
	Advice         []Advice   `json:"advice,omitempty"`
	Metrics        Metrics    `json:"metrics"`
	Thresholds     Thresholds `json:"thresholds"`
}

// DefaultThresholds returns conservative advisory thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		Enabled:                  true,
		WarningFileReads:         defaultWarningFileReads,
		HighFileReads:            defaultHighFileReads,
		WarningFileListings:      defaultWarningFileListings,
		HighFileListings:         defaultHighFileListings,
		WarningToolCalls:         defaultWarningToolCalls,
		HighToolCalls:            defaultHighToolCalls,
		WarningTruncations:       defaultWarningTruncations,
		HighTruncations:          defaultHighTruncations,
		WarningOutputCompression: defaultWarningOutputCompression,
		HighOutputCompression:    defaultHighOutputCompression,
		WarningSpillFiles:        defaultWarningSpillFiles,
		HighSpillFiles:           defaultHighSpillFiles,
		WarningStaleSurfaces:     defaultWarningStaleSurfaces,
		HighStaleSurfaces:        defaultHighStaleSurfaces,
		WarningTotalTokens:       defaultWarningTotalTokens,
		HighTotalTokens:          defaultHighTotalTokens,
	}
}

// LoadThresholds reads repo_config.yaml-style overrides under
// proxy.context_advisor. Missing config keeps compiled defaults.
func LoadThresholds(root string) (Thresholds, error) {
	thresholds := DefaultThresholds()
	root = strings.TrimSpace(root)

	if root == "" {
		return thresholds, nil
	}

	for _, name := range configdata.RepoConfigCandidates() {
		config, err := configdata.LoadYAMLMap(filepath.Join(root, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return Thresholds{}, fmt.Errorf("load context advisor config %s: %w", name, err)
		}

		return ThresholdsFromConfig(config, thresholds), nil
	}

	return thresholds, nil
}

// ThresholdsFromConfig applies proxy.context_advisor overrides to a base set.
func ThresholdsFromConfig(config configdata.Map, base Thresholds) Thresholds {
	settings := configdata.MapValue(
		configdata.GetPath(config, "proxy.context_advisor", map[string]any{}),
	)
	if settings == nil {
		return base
	}

	base.Enabled = boolAt(settings, "enabled", base.Enabled)
	base.WarningFileReads = positiveIntAt(
		settings,
		"warning_file_reads",
		base.WarningFileReads,
	)
	base.HighFileReads = positiveIntAt(settings, "high_file_reads", base.HighFileReads)
	base.WarningFileListings = positiveIntAt(
		settings,
		"warning_file_listings",
		base.WarningFileListings,
	)
	base.HighFileListings = positiveIntAt(
		settings,
		"high_file_listings",
		base.HighFileListings,
	)
	base.WarningToolCalls = positiveIntAt(
		settings,
		"warning_tool_calls",
		base.WarningToolCalls,
	)
	base.HighToolCalls = positiveIntAt(settings, "high_tool_calls", base.HighToolCalls)
	base.WarningTruncations = positiveIntAt(
		settings,
		"warning_truncations",
		base.WarningTruncations,
	)
	base.HighTruncations = positiveIntAt(
		settings,
		"high_truncations",
		base.HighTruncations,
	)
	base.WarningOutputCompression = positiveIntAt(
		settings,
		"warning_output_compression",
		base.WarningOutputCompression,
	)
	base.HighOutputCompression = positiveIntAt(
		settings,
		"high_output_compression",
		base.HighOutputCompression,
	)
	base.WarningSpillFiles = positiveIntAt(
		settings,
		"warning_spill_files",
		base.WarningSpillFiles,
	)
	base.HighSpillFiles = positiveIntAt(settings, "high_spill_files", base.HighSpillFiles)
	base.WarningStaleSurfaces = positiveIntAt(
		settings,
		"warning_stale_surfaces",
		base.WarningStaleSurfaces,
	)
	base.HighStaleSurfaces = positiveIntAt(
		settings,
		"high_stale_surfaces",
		base.HighStaleSurfaces,
	)
	base.WarningTotalTokens = positiveIntAt(
		settings,
		"warning_total_tokens",
		base.WarningTotalTokens,
	)
	base.HighTotalTokens = positiveIntAt(
		settings,
		"high_total_tokens",
		base.HighTotalTokens,
	)

	return base
}

// Analyze converts a session snapshot and output-surface inventory into compact
// context economy advice.
func Analyze(
	snapshot codeintel.SessionSnapshot,
	surfaces outputsurface.Report,
	thresholds Thresholds,
	now time.Time,
) Report {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	report := Report{
		GeneratedAtUTC: now.UTC().Format(time.RFC3339),
		Kind:           Kind,
		Status:         StatusOK,
		Metrics:        collectMetrics(snapshot, surfaces),
		Thresholds:     thresholds,
	}
	if !thresholds.Enabled {
		report.Summary = "context advisor disabled by configuration"

		return report
	}

	report.Advice = append(report.Advice, proxyReadAdvice(report.Metrics, thresholds)...)
	report.Advice = append(report.Advice, codeIntelAdvice(report.Metrics)...)
	report.Advice = append(
		report.Advice,
		toolPressureAdvice(report.Metrics, thresholds)...)
	report.Advice = append(report.Advice, compressionAdvice(report.Metrics, thresholds)...)
	report.Advice = append(
		report.Advice,
		outputSurfaceAdvice(report.Metrics, thresholds)...)
	report.Advice = append(report.Advice, tokenVolumeAdvice(report.Metrics, thresholds)...)
	report.Status = aggregateStatus(report.Advice)
	report.Summary = reportSummary(report)

	return report
}

// FormatTOON renders the advisory report as compact TOON.
func FormatTOON(report Report) string {
	lines := make([]string, 0, contextAdvisorTOONBaseLineCount+len(report.Advice))
	lines = append(lines,
		"kind: "+feedback.Cell(report.Kind),
		"generated_at_utc: "+feedback.Cell(report.GeneratedAtUTC),
		"status: "+feedback.Cell(report.Status),
		"summary: "+feedback.Cell(report.Summary),
		fmt.Sprintf("proxy_sessions: %d", report.Metrics.ProxySessions),
		fmt.Sprintf("proxy_events: %d", report.Metrics.ProxyEvents),
		fmt.Sprintf("file_reads: %d", report.Metrics.FileReads),
		fmt.Sprintf("file_listings: %d", report.Metrics.FileListings),
		fmt.Sprintf("tool_calls: %d", report.Metrics.ToolCalls),
		fmt.Sprintf("truncations: %d", report.Metrics.Truncations),
		fmt.Sprintf("output_compression: %d", report.Metrics.OutputCompression),
		fmt.Sprintf("spill_files: %d", report.Metrics.SpillFiles),
		fmt.Sprintf("stale_output_surfaces: %d", report.Metrics.StaleOutputSurfaces),
		fmt.Sprintf("total_tokens: %d", report.Metrics.TotalTokens),
		"code_intel_freshness: "+feedback.Cell(report.Metrics.CodeIntelFreshness),
		fmt.Sprintf(
			"advice[%d]{id,severity,signal,detail,recommendation}:",
			len(report.Advice),
		),
	)

	for _, advice := range report.Advice {
		lines = append(lines, strings.Join([]string{
			"  " + feedback.Cell(advice.ID),
			feedback.Cell(advice.Severity),
			feedback.Cell(advice.Signal),
			feedback.Cell(advice.Detail),
			feedback.Cell(advice.Recommendation),
		}, ","))
	}

	return strings.Join(lines, "\n")
}

// FormatAdviceTOON renders only triggered advice for lifecycle context. Empty
// advice produces an empty string so hooks stay quiet below threshold.
func FormatAdviceTOON(report Report) string {
	if len(report.Advice) == 0 {
		return ""
	}

	lines := []string{
		"context_token_economy_advice:",
		"status: " + feedback.Cell(report.Status),
		fmt.Sprintf("advice[%d]{id,severity,signal,recommendation}:", len(report.Advice)),
	}
	for _, advice := range report.Advice {
		lines = append(lines, strings.Join([]string{
			"  " + feedback.Cell(advice.ID),
			feedback.Cell(advice.Severity),
			feedback.Cell(advice.Signal),
			feedback.Cell(advice.Recommendation),
		}, ","))
	}

	return strings.Join(lines, "\n")
}

func collectMetrics(
	snapshot codeintel.SessionSnapshot,
	surfaces outputsurface.Report,
) Metrics {
	metrics := Metrics{
		CodeIntelFreshness:     snapshot.CodeIntel.Freshness,
		ProxySessions:          snapshot.Proxy.Sessions,
		ProxyEvents:            snapshot.Proxy.Events,
		FileReads:              snapshot.Proxy.FileReads,
		FileListings:           snapshot.Proxy.FileListings,
		ToolCalls:              snapshot.Proxy.ToolCalls,
		Truncations:            snapshot.Proxy.Truncations,
		OutputCompression:      snapshot.Proxy.OutputCompression,
		CacheHits:              snapshot.Proxy.CacheHits,
		TotalTokens:            snapshot.Proxy.TotalTokens,
		LinkedTraceCount:       snapshot.CodeIntel.LinkedTraceCount,
		CodeIntelTraceCount:    snapshot.CodeIntel.TraceCount,
		CodeIntelProxyEvents:   snapshot.CodeIntel.ProxyEventCount,
		CodeIntelStoreReady:    snapshot.CodeIntel.StoreReady,
		OutputSurfaces:         len(surfaces.Surfaces),
		NoEnforcementDecisions: true,
	}
	if metrics.CodeIntelFreshness == "" {
		metrics.CodeIntelFreshness = "unknown"
	}

	for _, surface := range surfaces.Surfaces {
		metrics.OutputSurfaceBytes += surface.TotalBytes
		metrics.StaleOutputSurfaces += surface.StaleCount

		if surface.Root == "temp" || surface.ID == "proxy_temp_evidence" {
			metrics.SpillFiles += surface.FileCount
		}
	}

	return metrics
}

func proxyReadAdvice(metrics Metrics, thresholds Thresholds) []Advice {
	advice := []Advice{}
	if item, matched := thresholdAdvice(
		adviceFileReads,
		metrics.FileReads,
		thresholds.WarningFileReads,
		thresholds.HighFileReads,
		"proxy_file_reads",
		"Proxy file reads are repeating in this session.",
		"Use code-intel context-card or narrower path reads before more file fetches.",
	); matched {
		advice = append(advice, item)
	}

	if item, matched := thresholdAdvice(
		adviceBroadReads,
		metrics.FileListings,
		thresholds.WarningFileListings,
		thresholds.HighFileListings,
		"proxy_file_listings",
		"Broad directory/listing reads are accumulating.",
		"Use repo-map or targeted search before another broad listing.",
	); matched {
		advice = append(advice, item)
	}

	return advice
}

func codeIntelAdvice(metrics Metrics) []Advice {
	if metrics.CodeIntelFreshness == "ready" || !hasContextPressure(metrics) {
		return nil
	}

	return []Advice{{
		ID:             adviceCodeIntelFreshness,
		Severity:       StatusWarn,
		Signal:         "code_intel_freshness=" + metrics.CodeIntelFreshness,
		Detail:         "Code-intel context is not ready for broad exploration.",
		Recommendation: "Refresh code intelligence before depending on repo-wide context.",
	}}
}

func hasContextPressure(metrics Metrics) bool {
	return metrics.FileReads > 0 ||
		metrics.FileListings > 0 ||
		metrics.ToolCalls > 0 ||
		metrics.Truncations > 0 ||
		metrics.OutputCompression > 0 ||
		metrics.SpillFiles > 0 ||
		metrics.TotalTokens > 0
}

func toolPressureAdvice(metrics Metrics, thresholds Thresholds) []Advice {
	item, matched := thresholdAdvice(
		adviceToolCalls,
		metrics.ToolCalls,
		thresholds.WarningToolCalls,
		thresholds.HighToolCalls,
		"proxy_tool_calls",
		"MCP/tool-call pressure is high for the session.",
		"Consolidate next reads around the current plan and avoid exploratory tool loops.",
	)
	if !matched {
		return nil
	}

	return []Advice{item}
}

func compressionAdvice(metrics Metrics, thresholds Thresholds) []Advice {
	advice := []Advice{}
	if item, matched := thresholdAdvice(
		adviceTruncations,
		metrics.Truncations,
		thresholds.WarningTruncations,
		thresholds.HighTruncations,
		"proxy_truncations",
		"Proxy output truncation occurred.",
		"Inspect full evidence paths before relying on compressed summaries.",
	); matched {
		advice = append(advice, item)
	}

	if item, matched := thresholdAdvice(
		adviceCompression,
		metrics.OutputCompression,
		thresholds.WarningOutputCompression,
		thresholds.HighOutputCompression,
		"proxy_output_compression",
		"Output compression transformed tool output.",
		"Prefer narrower commands or retrieve full output evidence when details matter.",
	); matched {
		advice = append(advice, item)
	}

	return advice
}

func outputSurfaceAdvice(metrics Metrics, thresholds Thresholds) []Advice {
	advice := []Advice{}
	if item, matched := thresholdAdvice(
		adviceSpillFiles,
		metrics.SpillFiles,
		thresholds.WarningSpillFiles,
		thresholds.HighSpillFiles,
		"output_spill_files",
		"Proxy spill/evidence files are present.",
		"Review retained full-output evidence before summarizing compressed results.",
	); matched {
		advice = append(advice, item)
	}

	if item, matched := thresholdAdvice(
		adviceStaleSurfaces,
		metrics.StaleOutputSurfaces,
		thresholds.WarningStaleSurfaces,
		thresholds.HighStaleSurfaces,
		"stale_output_surfaces",
		"Some output surfaces are past their retention window.",
		"Run output report or prune before treating retained telemetry as complete.",
	); matched {
		advice = append(advice, item)
	}

	return advice
}

func tokenVolumeAdvice(metrics Metrics, thresholds Thresholds) []Advice {
	item, matched := thresholdAdvice(
		adviceTotalTokens,
		metrics.TotalTokens,
		thresholds.WarningTotalTokens,
		thresholds.HighTotalTokens,
		"proxy_total_tokens",
		"Session token volume is high.",
		"Summarize decisions and narrow the next command before more broad output.",
	)
	if !matched {
		return nil
	}

	return []Advice{item}
}

func thresholdAdvice(
	adviceID string,
	value int,
	warningThreshold int,
	highThreshold int,
	signal string,
	detail string,
	recommendation string,
) (Advice, bool) {
	switch {
	case highThreshold > 0 && value >= highThreshold:
		return Advice{
			ID:             adviceID,
			Severity:       StatusHigh,
			Signal:         fmt.Sprintf("%s=%d", signal, value),
			Detail:         detail,
			Recommendation: recommendation,
		}, true
	case warningThreshold > 0 && value >= warningThreshold:
		return Advice{
			ID:             adviceID,
			Severity:       StatusWarn,
			Signal:         fmt.Sprintf("%s=%d", signal, value),
			Detail:         detail,
			Recommendation: recommendation,
		}, true
	default:
		return Advice{}, false
	}
}

func aggregateStatus(advice []Advice) string {
	status := StatusOK

	for _, item := range advice {
		if item.Severity == StatusHigh {
			return StatusHigh
		}

		if item.Severity == StatusWarn {
			status = StatusWarn
		}
	}

	return status
}

func reportSummary(report Report) string {
	if len(report.Advice) == 0 {
		return "context pressure below configured advice thresholds"
	}

	return fmt.Sprintf(
		"%d context pressure signal(s) crossed configured thresholds",
		len(report.Advice),
	)
}

func boolAt(values configdata.Map, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok {
		return fallback
	}

	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}

	return fallback
}

func positiveIntAt(values configdata.Map, key string, fallback int) int {
	value := configdata.IntAt(values, key)
	if value > 0 {
		return value
	}

	return fallback
}
