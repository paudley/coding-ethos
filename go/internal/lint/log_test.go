// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/lint"
)

func TestLogResultWritesNormalizedTrace(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	result := Result{
		Scope:  ScopeStaged,
		Status: "blocked",
		Findings: []Finding{{
			CheckID:    "python.direct_imports",
			Severity:   "block",
			Status:     "fail",
			Message:    "direct import violation",
			SourceTool: "ruff",
			Blocking:   true,
		}},
	}

	path, err := LogResult(repo, result)
	if err != nil {
		t.Fatalf("LogResult() returned error: %v", err)
	}

	if filepath.Dir(path) != filepath.Join(repo, ".coding-ethos", "lint-runs") {
		t.Fatalf("trace path = %q", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}

	var record TraceRecord
	if err := json.Unmarshal(content, &record); err != nil {
		t.Fatalf("decode trace: %v\n%s", err, content)
	}

	if record.RepoRoot != repo ||
		record.Result.Scope != ScopeStaged ||
		len(record.Result.Findings) != 1 {
		t.Fatalf("trace record = %#v", record)
	}
}
