// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

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
	if err := os.Chmod(path, 0o700); err != nil {
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

func TestEvaluateFileLargeFileBlocksNewOversizedFile(t *testing.T) {
	t.Parallel()

	repo := initGuardRepo(t)
	path := filepath.Join(repo, "large.py")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2048)), 0o600); err != nil {
		t.Fatalf("write large file: %v", err)
	}
	runGuardGit(t, repo, "add", "large.py")

	decision := evaluateFileGuardPolicy(
		t,
		"filesystem.large_files",
		EvaluateFileLargeFile,
		Context{
			Cwd:              repo,
			Files:            []string{"large.py"},
			EvaluatorOptions: map[string]any{"max_kb": 1, "suffixes": []string{".py"}},
		},
	)

	if decision.Diagnostics[0].Tool != "large_files" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateFileLargeFileDefaultsDoNotBlockPythonSource(t *testing.T) {
	t.Parallel()

	repo := initGuardRepo(t)
	path := filepath.Join(repo, "large.py")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2048)), 0o600); err != nil {
		t.Fatalf("write large file: %v", err)
	}
	runGuardGit(t, repo, "add", "large.py")

	decisions, err := EvaluateFileLargeFile(
		fileGuardPolicy("filesystem.large_files"),
		Context{
			Cwd:              repo,
			Files:            []string{"large.py"},
			EvaluatorOptions: map[string]any{"max_kb": 1},
		},
	)
	if err != nil {
		t.Fatalf("evaluate large file: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected Python source to be governed by line limits, got %#v", decisions)
	}
}

func TestEvaluateFileLineLimitBlocksGrowthOverLimit(t *testing.T) {
	t.Parallel()

	repo := initGuardRepo(t)
	path := filepath.Join(repo, "app.py")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write app: %v", err)
	}
	runGuardGit(t, repo, "add", "app.py")
	runGuardGit(t, repo, "commit", "-m", "initial")

	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("rewrite app: %v", err)
	}

	decision := evaluateFileGuardPolicy(
		t,
		"filesystem.line_limits",
		EvaluateFileLineLimit,
		Context{
			Cwd:              repo,
			Files:            []string{"app.py"},
			EvaluatorOptions: map[string]any{"python_hard": 2},
		},
	)

	if !strings.Contains(decision.Diagnostics[0].Message, "file grew from 1 to 3") {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
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

	if decision.Diagnostics[0].Tool != "pii" {
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

	if decision.Diagnostics[0].Tool != "pii" {
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
			"// SPDX-License-Identifier: MIT\n\npackage main\n",
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

func TestEvaluateLicenseHeaderBlocksMissingConfiguredLicenseFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	path := filepath.Join(repo, "app.go")
	err := os.WriteFile(
		path,
		[]byte("// SPDX-License-Identifier: MIT\n\npackage main\n"),
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
				"expected_license_text": "MIT License\n",
				"license_file":          "LICENSE",
				"spdx_id":               "MIT",
				"required":              []string{"SPDX-License-Identifier: MIT"},
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
		[]byte("// SPDX-License-Identifier: MIT\n\npackage main\n"),
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
				"expected_license_text": "MIT License\n",
				"license_file":          "LICENSE",
				"spdx_id":               "MIT",
				"required":              []string{"SPDX-License-Identifier: MIT"},
			},
		},
	)

	if decision.Diagnostics[0].Message != "LICENSE does not match the configured SPDX license text" {
		t.Fatalf("unexpected diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluateLicenseHeaderAllowsConfiguredLicenseContract(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	err := os.WriteFile(filepath.Join(repo, "LICENSE"), []byte("MIT License\n"), 0o600)
	if err != nil {
		t.Fatalf("write license: %v", err)
	}
	err = os.WriteFile(
		filepath.Join(repo, "app.go"),
		[]byte(
			"// SPDX-FileCopyrightText: 2026 Example Inc.\n"+
				"// SPDX-License-Identifier: MIT\n\npackage main\n",
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
				"expected_license_text": "MIT License\n",
				"license_file":          "LICENSE",
				"spdx_id":               "MIT",
				"required": []string{
					"SPDX-FileCopyrightText: 2026 Example Inc.",
					"SPDX-License-Identifier: MIT",
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

func writeGuardTestFile(t *testing.T, name string, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

func initGuardRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGuardGit(t, repo, "init")
	runGuardGit(t, repo, "config", "user.email", "test@example.com")
	runGuardGit(t, repo, "config", "user.name", "Test User")
	runGuardGit(t, repo, "config", "commit.gpgsign", "false")

	return repo
}

func runGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
