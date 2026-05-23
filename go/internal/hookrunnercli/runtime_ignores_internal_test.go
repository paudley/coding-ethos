// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import "testing"

const testSeverityError = "error"

func TestRuntimeIgnoreFindingsSkipsEmptyPaths(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)

	findings := runtimeIgnoreFindings([]string{"", "   "})
	if len(findings) != 0 {
		t.Fatalf("runtimeIgnoreFindings() = %#v, want no findings", findings)
	}
}

func TestRuntimeIgnoreHookFindingsAreStructured(t *testing.T) {
	t.Parallel()

	findings := runtimeIgnoreHookFindings([]string{
		".coding-ethos/cache/ is not ignored; add coding-ethos runtime paths",
	})
	if len(findings) != 1 {
		t.Fatalf("runtimeIgnoreHookFindings() = %#v, want one finding", findings)
	}

	finding := findings[0]
	if finding.File != ".coding-ethos/cache/" ||
		finding.PolicyID != "runtime.ignored_paths" ||
		finding.SkillID != "managed-toolchain" ||
		finding.Severity != testSeverityError {
		t.Fatalf("runtime ignore finding lost structure: %#v", finding)
	}
}
