// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const piiToolName = "pii"

func TestEvaluateFileMergeConflictBlocksMarkers(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "conflict.txt", "<<<<<<< HEAD\n")
	decision := evaluateFileGuardPolicy(
		t,
		"syntax.merge_conflict",
		EvaluateFileMergeConflict,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Tool != "merge_conflict" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileMergeConflictAllowsDecorativeSeparator(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "cv.txt", "Experience\n=======\nProjects\n")

	decisions, err := EvaluateFileMergeConflict(
		fileGuardPolicy("syntax.merge_conflict"),
		Context{Files: []string{path}},
	)
	if err != nil {
		t.Fatalf("evaluate merge conflicts: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected decorative separator to pass, got %#v", decisions)
	}
}

func TestEvaluateFileMergeConflictBlocksSeparatorInConflictContext(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(
		t,
		"conflict.txt",
		"<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n",
	)
	decision := evaluateFileGuardPolicy(
		t,
		"syntax.merge_conflict",
		EvaluateFileMergeConflict,
		Context{
			EvaluatorOptions: map[string]any{"markers": []any{"======="}},
			Files:            []string{path},
		},
	)

	if decision.Diagnostics[0].Code != "=======" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileMergeConflictReportsSeparatorWithDefaultMarkers(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(
		t,
		"conflict.txt",
		"<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n",
	)
	decision := evaluateFileGuardPolicy(
		t,
		"syntax.merge_conflict",
		EvaluateFileMergeConflict,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Code != "=======" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileMergeConflictSkipsDirectories(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateFileMergeConflict(
		fileGuardPolicy("syntax.merge_conflict"),
		Context{Files: []string{t.TempDir()}},
	)
	if err != nil {
		t.Fatalf("evaluate merge conflicts: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions for directory path, got %#v", decisions)
	}
}

func TestEvaluateFilePrivateKeyBlocksKeyMaterial(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(
		t,
		"secret.pem",
		"-----BEGIN RSA "+"PRIVATE KEY-----\nredacted\n",
	)
	decision := evaluateFileGuardPolicy(
		t,
		"security.private_key",
		EvaluateFilePrivateKey,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Tool != "private_key" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileShebangBlocksExecutableWithoutShebang(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "script.sh", "echo ok\n")

	err := os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod script: %v", err)
	}

	decision := evaluateFileGuardPolicy(
		t,
		"filesystem.shebangs",
		EvaluateFileShebang,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Message != "executable file has no shebang" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileShebangSkipsDirectories(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateFileShebang(
		fileGuardPolicy("filesystem.shebangs"),
		Context{Files: []string{t.TempDir()}},
	)
	if err != nil {
		t.Fatalf("evaluate shebangs: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions for directory path, got %#v", decisions)
	}
}

func TestEvaluatePIIScrubberBlocksLocalMachineDetails(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "notes.md", "path: /"+"home/example/project\n")
	decision := evaluateFileGuardPolicy(
		t,
		"repo.pii_scrubber",
		EvaluatePIIScrubber,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Tool != piiToolName {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluatePIIScrubberBlocksConfiguredLiteral(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "notes.md", "host: build-host-17\n")
	decision := evaluateFileGuardPolicy(
		t,
		"repo.pii_scrubber",
		EvaluatePIIScrubber,
		Context{
			Files:            []string{path},
			EvaluatorOptions: map[string]any{"literals": []string{"build-host-17"}},
		},
	)

	if decision.Diagnostics[0].Tool != piiToolName {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluatePIIScrubberSkipsHiddenDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, path := range []string{
		".claude/settings.local.json",
		".codex/config.toml",
		".gemini/settings.json",
		"nested/.wolf/hooks/session.json",
	} {
		fullPath := filepath.Join(root, filepath.FromSlash(path))

		err := os.MkdirAll(filepath.Dir(fullPath), 0o700)
		if err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}

		err = os.WriteFile(
			fullPath,
			[]byte("path: /"+"home/example/project\n"),
			0o600,
		)
		if err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	decisions, err := EvaluatePIIScrubber(
		fileGuardPolicy("repo.pii_scrubber"),
		Context{
			Cwd: root,
			Files: []string{
				".claude/settings.local.json",
				".codex/config.toml",
				".gemini/settings.json",
				"nested/.wolf/hooks/session.json",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate PII scrubber: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected hidden directories to be skipped, got %#v", decisions)
	}
}

func TestEvaluatePIIScrubberStillScansDotFiles(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, ".env", "path: /"+"home/example/project\n")
	decision := evaluateFileGuardPolicy(
		t,
		"repo.pii_scrubber",
		EvaluatePIIScrubber,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Tool != piiToolName {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateLicenseHeaderBlocksMissingSPDX(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "app.go", "package main\n")
	decision := evaluateFileGuardPolicy(
		t,
		"repo.license_header",
		EvaluateLicenseHeader,
		Context{Files: []string{path}},
	)

	if decision.Diagnostics[0].Tool != "license_header" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateLicenseHeaderAllowsSPDX(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(
		t,
		"app.go",
		"// SPDX-FileCopyrightText: 2026 Example Inc.\n"+
			"// SPDX-License-Identifier: AGPL-3.0-only\n\npackage main\n",
	)

	decisions, err := EvaluateLicenseHeader(
		fileGuardPolicy("repo.license_header"),
		Context{Files: []string{path}},
	)
	if err != nil {
		t.Fatalf("evaluate license header: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}

func TestEvaluateLicenseHeaderIgnoresYAMLByDefault(t *testing.T) {
	t.Parallel()

	path := writeGuardTestFile(t, "config.yaml", "name: app\n")

	decisions, err := EvaluateLicenseHeader(
		fileGuardPolicy("repo.license_header"),
		Context{Files: []string{path}},
	)
	if err != nil {
		t.Fatalf("evaluate license header: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf(
			"yaml files should not require license or copyright headers: %#v",
			decisions,
		)
	}
}

func TestEvaluateLicenseHeaderBlocksMissingConfiguredLicenseFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	path := filepath.Join(repo, "app.go")

	err := os.WriteFile(
		path,
		[]byte("// SPDX-License-Identifier: AGPL-3.0-only\n\npackage main\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write app: %v", err)
	}

	decision := evaluateFileGuardPolicy(
		t,
		"repo.license_header",
		EvaluateLicenseHeader,
		Context{
			Cwd:   repo,
			Files: []string{"app.go"},
			EvaluatorOptions: map[string]any{
				"expected_license_text": "                    GNU AFFERO GENERAL PUBLIC LICENSE\n",
				"license_file":          "LICENSE",
				"spdx_id":               "AGPL-3.0-only",
				"required":              []string{"SPDX-License-Identifier: AGPL-3.0-only"},
			},
		},
	)

	if decision.Diagnostics[0].Tool != "license_file" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateLicenseHeaderBlocksMismatchedConfiguredLicenseFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	err := os.WriteFile(filepath.Join(repo, "LICENSE"), []byte("wrong\n"), 0o600)
	if err != nil {
		t.Fatalf("write license: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(repo, "app.go"),
		[]byte("// SPDX-License-Identifier: AGPL-3.0-only\n\npackage main\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write app: %v", err)
	}

	decision := evaluateFileGuardPolicy(
		t,
		"repo.license_header",
		EvaluateLicenseHeader,
		Context{
			Cwd:   repo,
			Files: []string{"app.go"},
			EvaluatorOptions: map[string]any{
				"expected_license_text": "                    GNU AFFERO GENERAL PUBLIC LICENSE\n",
				"license_file":          "LICENSE",
				"spdx_id":               "AGPL-3.0-only",
				"required":              []string{"SPDX-License-Identifier: AGPL-3.0-only"},
			},
		},
	)

	if decision.Diagnostics[0].Message !=
		"LICENSE does not match the configured SPDX license text" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateLicenseHeaderAllowsConfiguredLicenseContract(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	err := os.WriteFile(
		filepath.Join(repo, "LICENSE"),
		[]byte("                    GNU AFFERO GENERAL PUBLIC LICENSE\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write license: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(repo, "app.go"),
		[]byte(
			"// SPDX-FileCopyrightText: 2026 Example Inc.\n"+
				"// SPDX-License-Identifier: AGPL-3.0-only\n\npackage main\n",
		),
		0o600,
	)
	if err != nil {
		t.Fatalf("write app: %v", err)
	}

	decisions, err := EvaluateLicenseHeader(
		fileGuardPolicy("repo.license_header"),
		Context{
			Cwd:   repo,
			Files: []string{"app.go"},
			EvaluatorOptions: map[string]any{
				"expected_license_text": "                    GNU AFFERO GENERAL PUBLIC LICENSE\n",
				"license_file":          "LICENSE",
				"spdx_id":               "AGPL-3.0-only",
				"required": []string{
					"SPDX-FileCopyrightText: 2026 Example Inc.",
					"SPDX-License-Identifier: AGPL-3.0-only",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate license header: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}

func evaluateFileGuardPolicy(
	t *testing.T,
	policyID string,
	evaluator func(policy.Policy, Context) ([]policy.Decision, error),
	context Context,
) policy.Decision {
	t.Helper()

	decisions, err := evaluator(fileGuardPolicy(policyID), context)
	if err != nil {
		t.Fatalf("evaluate %s: %v", policyID, err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	return decisions[0]
}

func fileGuardPolicy(policyID string) policy.Policy {
	return policy.Policy{
		ID:              policyID,
		DefaultSeverity: "block",
		Message:         policyID + " failed",
		Suggestion:      "fix " + policyID,
	}
}

func writeGuardTestFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}
