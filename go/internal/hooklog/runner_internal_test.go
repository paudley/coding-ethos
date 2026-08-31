// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooklog

import "testing"

func TestShouldForceCodeIntelRefreshSkipsManagedToolRuns(t *testing.T) {
	t.Parallel()

	for _, command := range [][]string{
		{"coding-ethos-run", "policy-tool", "go-test", "go"},
		{"coding-ethos-run", "policy-tool", "ruff", "check", "pkg"},
		{"coding-ethos-run", "policy-tool-group", "type_check"},
		{"coding-ethos-run", "parent-install", "--repo", "/repo"},
		{"coding-ethos-run", "parent-check", "--repo", "/repo"},
		{"coding-ethos-run", "parent-lint", "--repo", "/repo"},
	} {
		if shouldForceCodeIntelRefresh(command) {
			t.Fatalf("shouldForceCodeIntelRefresh(%#v) = true, want false", command)
		}
	}
}

func TestShouldForceCodeIntelRefreshKeepsHookAndRepoGates(t *testing.T) {
	t.Parallel()

	for _, command := range [][]string{
		{"coding-ethos-run", "policy-lint"},
		{"coding-ethos-run", "git-hook", "pre-commit"},
		{"coding-ethos-run", "git-hook", "pre-push"},
		{"make", "check"},
		{"make", "pre-commit"},
	} {
		if !shouldForceCodeIntelRefresh(command) {
			t.Fatalf("shouldForceCodeIntelRefresh(%#v) = false, want true", command)
		}
	}
}
