// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import "testing"

func TestDecisionEvidenceFilesPrefersCanonicalFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"files":        []string{"pyproject.toml"},
			"staged_files": []string{"bin/coding-ethos-run"},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "pyproject.toml" {
		t.Fatalf("files mismatch: %#v", files)
	}
}

func TestDecisionEvidenceFilesFallsBackToStagedFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"staged_files": []any{"bin/coding-ethos-run", ""},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "bin/coding-ethos-run" {
		t.Fatalf("files mismatch: %#v", files)
	}
}
