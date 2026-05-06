// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"encoding/json"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

func FuzzFormatLintResultSARIF(f *testing.F) {
	f.Add("policy.example", "pkg/app.py", "message", "detail")
	f.Add("shell.forbidden_strings", ".claude/settings.json", "blocked", "advice")
	f.Add("repo.large_file_growth", "lib/example.py", "split file", "line count")
	f.Add("runtime.sandbox_denial", "", "sandbox denied", "bubblewrap missing")
	f.Add("sarif.path.edge", "../outside.py", "relative path", "path normalization")

	f.Fuzz(func(t *testing.T, policyID, file, message, detail string) {
		result := lint.Result{
			Scope:  lint.ScopeFiles,
			Status: "blocked",
			Diagnostics: []diagnostics.Diagnostic{{
				Tool:     "policy-lint",
				File:     file,
				Line:     1,
				Column:   1,
				Severity: "error",
				PolicyID: policyID,
				Message:  message,
				Detail:   detail,
			}},
		}

		output, err := FormatLintResultSARIF(result)
		if err != nil {
			t.Fatalf("format SARIF: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("decode SARIF: %v", err)
		}

		if payload["version"] != "2.1.0" {
			t.Fatalf("unexpected SARIF version in %#v", payload)
		}

		runs, ok := payload["runs"].([]any)
		if !ok || len(runs) != 1 {
			t.Fatalf("unexpected SARIF runs in %#v", payload)
		}
	})
}
