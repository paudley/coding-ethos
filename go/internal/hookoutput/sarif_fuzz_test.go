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
	})
}
