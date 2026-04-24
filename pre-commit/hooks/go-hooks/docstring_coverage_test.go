package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDocstringCoverageCommand(t *testing.T) {
	command := buildDocstringCoverageCommand(
		docstringCoverageSettings{
			Threshold:       95,
			CheckPaths:      []string{"pkg", "pre-commit/hooks"},
			ExcludePatterns: []string{`tests`, `\.venv`},
			Command:         []string{"interrogate"},
			UseHookProject:  true,
			HooksProject:    "/tmp/hooks",
		},
	)

	wantPrefix := []string{"uv", "run", "--quiet", "--project", "/tmp/hooks", "interrogate", "--fail-under", "95", "--verbose"}
	if len(command) < len(wantPrefix) {
		t.Fatalf("command = %#v, want prefix %#v", command, wantPrefix)
	}
	for i := range wantPrefix {
		if command[i] != wantPrefix[i] {
			t.Fatalf("command[%d] = %q, want %q (%#v)", i, command[i], wantPrefix[i], command)
		}
	}
	if !slicesContains(command, "--ignore-regex") || !slicesContains(command, "pkg") || !slicesContains(command, "pre-commit/hooks") {
		t.Fatalf("command missing expected flags or paths: %#v", command)
	}
}

func TestCheckDocstringCoverageCommandReportsFailure(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  docstring_coverage:
    enabled: true
    threshold: 95
    command:
      - /bin/sh
      - -lc
      - "printf 'Coverage: 10.0\\n'; exit 1"
    use_hook_project: false
    check_paths:
      - pkg
      - pre-commit/hooks
    exclude_patterns:
      - tests
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if got := checkDocstringCoverageCommand(Config{}, nil); got != 1 {
				t.Fatalf("checkDocstringCoverageCommand() = %d, want 1", got)
			}
		})
		if strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})

	if !strings.Contains(stdout, "DOCSTRING COVERAGE CHECK FAILED") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "Threshold: 95%") {
		t.Fatalf("missing threshold in stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "Paths: pkg, pre-commit/hooks") {
		t.Fatalf("missing paths in stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "Coverage: 10.0") {
		t.Fatalf("missing command output in stdout: %q", stdout)
	}
}
