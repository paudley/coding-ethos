// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHookLogSummaryReadsMetadataRuns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-a", "metadata.env"),
		"run_id=run-a\nstarted_at_utc=20260427T000000Z\nexit_code=0\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-b", "metadata.env"),
		"run_id=run-b\nstarted_at_utc=20260427T000001Z\nexit_code=1\n",
	)

	summary, err := loadHookLogSummary(root, hookOutputFormatTOON)
	if err != nil {
		t.Fatalf("loadHookLogSummary() returned error: %v", err)
	}

	if summary.Total != 2 || summary.Passed != 1 || summary.Failed != 1 {
		t.Fatalf("summary counts = %#v", summary)
	}

	output := formatHookLogSummary(summary)
	if !strings.Contains(output, "runs[2]{run_id,started_at_utc,exit_code}:") {
		t.Fatalf("TOON summary missing runs table:\n%s", output)
	}
}

func TestParseMetadataEnvTrimsQuotedValues(t *testing.T) {
	t.Parallel()

	values := parseMetadataEnv("run_id='abc'\nexit_code=\"1\"\n")
	if values["run_id"] != "abc" || values["exit_code"] != "1" {
		t.Fatalf("parseMetadataEnv() = %#v", values)
	}
}
