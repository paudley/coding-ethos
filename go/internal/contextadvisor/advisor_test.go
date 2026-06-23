// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package contextadvisor

import (
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

func TestAnalyzeProducesNoAdviceBelowThresholds(t *testing.T) {
	t.Parallel()

	report := Analyze(
		readySnapshot(),
		outputsurface.Report{},
		DefaultThresholds(),
		time.Date(2026, 6, 23, 8, 0, 0, 0, time.UTC),
	)

	if report.Status != StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, StatusOK)
	}
	if len(report.Advice) != 0 {
		t.Fatalf("advice = %#v, want none", report.Advice)
	}
	if !report.Metrics.NoEnforcementDecisions {
		t.Fatal("advisor did not mark output as advisory-only")
	}
	if got := FormatAdviceTOON(report); got != "" {
		t.Fatalf("advice-only TOON should be empty below threshold:\n%s", got)
	}
}

func TestAnalyzeProducesWarningAdviceAtThreshold(t *testing.T) {
	t.Parallel()

	snapshot := readySnapshot()
	snapshot.Proxy.FileReads = DefaultThresholds().WarningFileReads

	report := Analyze(snapshot, outputsurface.Report{}, DefaultThresholds(), time.Time{})

	if report.Status != StatusWarn {
		t.Fatalf("status = %q, want %q", report.Status, StatusWarn)
	}
	if len(report.Advice) != 1 {
		t.Fatalf("advice count = %d, want 1: %#v", len(report.Advice), report.Advice)
	}
	if report.Advice[0].ID != adviceFileReads ||
		report.Advice[0].Severity != StatusWarn {
		t.Fatalf("unexpected advice: %#v", report.Advice[0])
	}

	toon := FormatAdviceTOON(report)
	for _, expected := range []string{
		"context_token_economy_advice:",
		"repeated_file_reads",
		"WARN",
		"proxy_file_reads=8",
	} {
		if !strings.Contains(toon, expected) {
			t.Fatalf("TOON missing %q:\n%s", expected, toon)
		}
	}
}

func TestAnalyzeProducesHighPressureAdvice(t *testing.T) {
	t.Parallel()

	thresholds := DefaultThresholds()
	snapshot := readySnapshot()
	snapshot.Proxy.Truncations = thresholds.HighTruncations
	snapshot.Proxy.OutputCompression = thresholds.HighOutputCompression
	snapshot.Proxy.TotalTokens = thresholds.HighTotalTokens

	surfaces := outputsurface.Report{
		Surfaces: []outputsurface.Inventory{
			{
				Definition: outputsurface.Definition{
					ID:   "proxy_temp_evidence",
					Root: "temp",
				},
				FileCount:  thresholds.HighSpillFiles,
				StaleCount: thresholds.HighStaleSurfaces,
				TotalBytes: 1024,
			},
		},
	}

	report := Analyze(snapshot, surfaces, thresholds, time.Time{})

	if report.Status != StatusHigh {
		t.Fatalf("status = %q, want %q", report.Status, StatusHigh)
	}
	for _, id := range []string{
		adviceTruncations,
		adviceCompression,
		adviceSpillFiles,
		adviceStaleSurfaces,
		adviceTotalTokens,
	} {
		assertAdvice(t, report, id, StatusHigh)
	}
	if report.Metrics.SpillFiles != thresholds.HighSpillFiles {
		t.Fatalf(
			"spill files = %d, want %d",
			report.Metrics.SpillFiles,
			thresholds.HighSpillFiles,
		)
	}
}

func TestThresholdsFromConfigOverridesDefaults(t *testing.T) {
	t.Parallel()

	thresholds := ThresholdsFromConfig(
		configdata.Map{
			"proxy": map[string]any{
				"context_advisor": map[string]any{
					"enabled":            false,
					"warning_file_reads": 2,
					"high_file_reads":    4,
				},
			},
		},
		DefaultThresholds(),
	)

	if thresholds.Enabled {
		t.Fatal("enabled override was not applied")
	}
	if thresholds.WarningFileReads != 2 || thresholds.HighFileReads != 4 {
		t.Fatalf("threshold override failed: %#v", thresholds)
	}
}

func TestThresholdsFromConfigAllowsDisabledThresholds(t *testing.T) {
	t.Parallel()

	thresholds := ThresholdsFromConfig(
		configdata.Map{
			"proxy": map[string]any{
				"context_advisor": map[string]any{
					"warning_file_reads": 0,
					"high_file_reads":    -1,
				},
			},
		},
		DefaultThresholds(),
	)

	if thresholds.WarningFileReads != 0 || thresholds.HighFileReads != -1 {
		t.Fatalf("disabled thresholds were not preserved: %#v", thresholds)
	}

	snapshot := readySnapshot()
	snapshot.Proxy.FileReads = defaultHighFileReads

	report := Analyze(snapshot, outputsurface.Report{}, thresholds, time.Time{})
	if report.Status != StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, StatusOK)
	}
	if len(report.Advice) != 0 {
		t.Fatalf("disabled threshold produced advice: %#v", report.Advice)
	}
}

func readySnapshot() codeintel.SessionSnapshot {
	return codeintel.SessionSnapshot{
		CodeIntel: codeintel.SessionCodeIntelSummary{
			Freshness:        "ready",
			LinkedTraceCount: 1,
			ProxyEventCount:  1,
			StoreReady:       true,
			TraceCount:       1,
		},
	}
}

func assertAdvice(t *testing.T, report Report, id, severity string) {
	t.Helper()

	for _, advice := range report.Advice {
		if advice.ID == id {
			if advice.Severity != severity {
				t.Fatalf("advice %s severity = %q, want %q", id, advice.Severity, severity)
			}

			return
		}
	}

	t.Fatalf("missing advice %q in %#v", id, report.Advice)
}
