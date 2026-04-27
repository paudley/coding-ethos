// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func TestResolveTypeCheckerCommandInjectsRepoConfig(t *testing.T) {
	tempDir := t.TempDir()
	bundleRoot := filepath.Join(tempDir, "pre-commit")
	consumerRoot := filepath.Join(tempDir, "repo")
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "pyproject.toml"),
		"[tool.mypy]\n",
	)
	mustWriteTestFile(t, filepath.Join(consumerRoot, "pyrightconfig.json"), "{}\n")

	command := resolveTypeCheckerCommand(
		typeCheckerConfig{
			Name:                 "pyright",
			Command:              []string{"pyright"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--project", "-p"},
			RepoConfig:           "pyrightconfig.json",
			FallbackBundleConfig: "hooks/pyproject.toml",
		},
		typeCheckSettings{
			BundleRoot:   bundleRoot,
			ConsumerRoot: consumerRoot,
			HooksProject: filepath.Join(bundleRoot, "hooks"),
		},
	)

	wantPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		filepath.Join(bundleRoot, "hooks"),
		"pyright",
		"--project",
		filepath.Join(consumerRoot, "pyrightconfig.json"),
	}
	if len(command) < len(wantPrefix) {
		t.Fatalf(
			"resolveTypeCheckerCommand() = %#v, want prefix %#v",
			command,
			wantPrefix,
		)
	}
	for i := range wantPrefix {
		if command[i] != wantPrefix[i] {
			t.Fatalf(
				"command[%d] = %q, want %q (%#v)",
				i,
				command[i],
				wantPrefix[i],
				command,
			)
		}
	}
}

func TestDefaultTypeCheckersIncludeStaticAnalysisSet(t *testing.T) {
	checkers := defaultTypeCheckers()
	names := make(map[string]bool, len(checkers))
	for _, checker := range checkers {
		names[checker.Name] = true
	}
	for _, name := range []string{"ruff", "mypy", "pyright", "pylint"} {
		if !names[name] {
			t.Fatalf("defaultTypeCheckers() missing %q: %#v", name, checkers)
		}
	}
}

func TestDefaultTypeCheckersRequestJsonOutput(t *testing.T) {
	checkers := defaultTypeCheckers()
	commands := make(map[string][]string, len(checkers))
	for _, checker := range checkers {
		commands[checker.Name] = checker.Command
	}
	for name, token := range map[string]string{
		"ruff":    "--output-format",
		"pyright": "--outputjson",
		"mypy":    "--output",
		"pylint":  "--output-format=json",
	} {
		if !stringSliceContains(commands[name], token) {
			t.Fatalf("%s command missing %q: %#v", name, token, commands[name])
		}
	}
}

func TestParseTypeCheckDiagnostics(t *testing.T) {
	cases := []struct {
		name    string
		checker string
		output  string
		want    typeCheckDiagnostic
	}{
		{
			name:    "ruff",
			checker: "ruff",
			output:  `[{"filename":"pkg/app.py","code":"F401","message":"unused import","location":{"row":3,"column":5}}]`,
			want: typeCheckDiagnostic{
				Checker:  "ruff",
				File:     "pkg/app.py",
				Severity: "error",
				Code:     "F401",
				Message:  "unused import",
				Line:     3,
				Column:   5,
			},
		},
		{
			name:    "pyright",
			checker: "pyright",
			output:  `{"generalDiagnostics":[{"file":"pkg/app.py","severity":"error","message":"bad type","rule":"reportAssignmentType","range":{"start":{"line":4,"character":2}}}]}`,
			want: typeCheckDiagnostic{
				Checker:  "pyright",
				File:     "pkg/app.py",
				Severity: "error",
				Code:     "reportAssignmentType",
				Message:  "bad type",
				Line:     5,
				Column:   3,
			},
		},
		{
			name:    "mypy",
			checker: "mypy",
			output:  `{"file":"pkg/app.py","line":7,"column":1,"message":"missing type","code":"no-untyped-def","severity":"error"}`,
			want: typeCheckDiagnostic{
				Checker:  "mypy",
				File:     "pkg/app.py",
				Severity: "error",
				Code:     "no-untyped-def",
				Message:  "missing type",
				Line:     7,
				Column:   1,
			},
		},
		{
			name:    "pylint",
			checker: "pylint",
			output:  `[{"path":"pkg/app.py","type":"convention","symbol":"missing-function-docstring","message":"Missing function or method docstring","line":11,"column":0}]`,
			want: typeCheckDiagnostic{
				Checker:  "pylint",
				File:     "pkg/app.py",
				Severity: "convention",
				Code:     "missing-function-docstring",
				Message:  "Missing function or method docstring",
				Line:     11,
				Column:   1,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTypeCheckDiagnostics(tc.checker, tc.output)
			if len(got) != 1 {
				t.Fatalf("parseTypeCheckDiagnostics() = %#v, want one diagnostic", got)
			}
			if got[0] != tc.want {
				t.Fatalf("parseTypeCheckDiagnostics()[0] = %#v, want %#v", got[0], tc.want)
			}
		})
	}
}

func TestFormatTypeCheckResultsGroupsDiagnosticsByFile(t *testing.T) {
	output := formatTypeCheckResults(
		[]typeCheckResult{
			{
				Name:     "ruff",
				ExitCode: 1,
				Diagnostics: []typeCheckDiagnostic{
					{
						Checker:  "ruff",
						File:     "pkg/a.py",
						Severity: "error",
						Code:     "F401",
						Message:  "unused import",
						Line:     2,
						Column:   1,
					},
					{
						Checker:  "ruff",
						File:     "pkg/b.py",
						Severity: "error",
						Code:     "F821",
						Message:  "undefined name",
						Line:     9,
						Column:   4,
					},
				},
			},
		},
		2,
		hookOutputFormatHuman,
	)
	for _, fragment := range []string{
		"pkg/a.py",
		"a.py:2:1 [error F401] unused import",
		"pkg/b.py",
		"b.py:9:4 [error F821] undefined name",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("formatted output missing %q:\n%s", fragment, output)
		}
	}
}

func TestFormatTypeCheckResultsTOON(t *testing.T) {
	output := formatTypeCheckResults(
		[]typeCheckResult{
			{
				Name:       "mypy",
				ExitCode:   1,
				DurationMS: 12,
				Diagnostics: []typeCheckDiagnostic{
					{
						Checker:  "mypy",
						File:     "pkg/app.py",
						Severity: "error",
						Code:     "no-any-return",
						Message:  "Returning Any from function declared to return bool",
						Line:     88,
						Column:   12,
					},
				},
			},
		},
		1,
		hookOutputFormatTOON,
	)
	for _, fragment := range []string{
		"format: toon",
		"status: FAIL",
		"checks{name,status,exit_code,duration_ms}:",
		"diagnostics[1]{tool,file,line,column,severity,code,message}:",
		"mypy,pkg/app.py,88,12,error,no-any-return,Returning Any from function declared to return bool",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("TOON output missing %q:\n%s", fragment, output)
		}
	}
}

func TestSelectedTypeCheckOutputFormatHonorsAgentEnvironment(t *testing.T) {
	t.Setenv(hookOutputFormatEnv, "")
	t.Setenv("CODEX_THREAD_ID", "thread")

	if got := selectedHookOutputFormat(); got != hookOutputFormatTOON {
		t.Fatalf("selectedHookOutputFormat() = %q, want toon", got)
	}
}

func TestSelectedTypeCheckOutputFormatAllowsHumanOverride(t *testing.T) {
	t.Setenv(hookOutputFormatEnv, hookOutputFormatHuman)
	t.Setenv("CODEX_THREAD_ID", "thread")

	if got := selectedHookOutputFormat(); got != hookOutputFormatHuman {
		t.Fatalf("selectedHookOutputFormat() = %q, want human", got)
	}
}

func TestResolveRuffCommandInjectsRepoConfig(t *testing.T) {
	tempDir := t.TempDir()
	bundleRoot := filepath.Join(tempDir, "pre-commit")
	consumerRoot := filepath.Join(tempDir, "repo")
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "pyproject.toml"),
		"[project]\nname = \"hooks\"\n",
	)
	mustWriteTestFile(t, filepath.Join(consumerRoot, "ruff.toml"), "line-length = 88\n")

	command := resolveTypeCheckerCommand(
		typeCheckerConfig{
			Name:            "ruff",
			Command:         []string{"ruff", "check", "--quiet", "--ignore-noqa"},
			PassFilesAsArgs: true,
			UseHookProject:  true,
			ConfigFlags:     []string{"--config"},
			RepoConfig:      "ruff.toml",
		},
		typeCheckSettings{
			BundleRoot:   bundleRoot,
			ConsumerRoot: consumerRoot,
			HooksProject: filepath.Join(bundleRoot, "hooks"),
		},
	)

	wantPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		filepath.Join(bundleRoot, "hooks"),
		"ruff",
		"check",
		"--quiet",
		"--ignore-noqa",
		"--config",
		filepath.Join(consumerRoot, "ruff.toml"),
	}
	if len(command) < len(wantPrefix) {
		t.Fatalf(
			"resolveTypeCheckerCommand() = %#v, want prefix %#v",
			command,
			wantPrefix,
		)
	}
	for i := range wantPrefix {
		if command[i] != wantPrefix[i] {
			t.Fatalf(
				"command[%d] = %q, want %q (%#v)",
				i,
				command[i],
				wantPrefix[i],
				command,
			)
		}
	}
}

func TestResolveTypeCheckerCommandPrefersConsumerWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	consumerRoot := filepath.Join(tempDir, "repo")
	hooksProject := filepath.Join(consumerRoot, "coding-ethos", "pre-commit", "hooks")
	mustWriteTestFile(
		t,
		filepath.Join(consumerRoot, "pyproject.toml"),
		strings.TrimSpace(`
[tool.uv.workspace]
members = [
    "lbox-platform/lib/python",
    "coding-ethos/pre-commit/hooks",
]
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(hooksProject, "pyproject.toml"),
		"[tool.mypy]\n",
	)

	command := resolveTypeCheckerCommand(
		typeCheckerConfig{
			Name:            "mypy",
			Command:         []string{"mypy"},
			PassFilesAsArgs: true,
			UseHookProject:  true,
			ConfigFlags:     []string{"--config-file"},
			RepoConfig:      "mypy.ini",
		},
		typeCheckSettings{
			ConsumerRoot: consumerRoot,
			HooksProject: hooksProject,
		},
	)

	wantPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		consumerRoot,
		"mypy",
	}
	if len(command) < len(wantPrefix) {
		t.Fatalf(
			"resolveTypeCheckerCommand() = %#v, want prefix %#v",
			command,
			wantPrefix,
		)
	}
	for i := range wantPrefix {
		if command[i] != wantPrefix[i] {
			t.Fatalf(
				"command[%d] = %q, want %q (%#v)",
				i,
				command[i],
				wantPrefix[i],
				command,
			)
		}
	}
}

func TestNormalizeTypeCheckFiles(t *testing.T) {
	tempDir := t.TempDir()
	pythonFile := filepath.Join(tempDir, "pkg", "module.py")
	dockerFile := filepath.Join(tempDir, "pkg", "docker", "script.py")
	venvFile := filepath.Join(tempDir, ".venv", "lib.py")
	whitelist := filepath.Join(tempDir, "vulture_whitelist.py")
	mustWriteTestFile(t, pythonFile, "value = 1\n")
	mustWriteTestFile(t, dockerFile, "value = 1\n")
	mustWriteTestFile(t, venvFile, "value = 1\n")
	mustWriteTestFile(t, whitelist, "value\n")

	files := normalizeTypeCheckFiles(
		[]string{pythonFile, dockerFile, venvFile, whitelist, pythonFile},
		[]string{"/docker/", "vulture_whitelist"},
	)
	if len(files) != 1 || files[0] != pythonFile {
		t.Fatalf("normalizeTypeCheckFiles() = %#v, want [%q]", files, pythonFile)
	}
}

func TestNormalizeTypeCheckFilesHonorsConfiguredExcludedPathFragments(t *testing.T) {
	tempDir := t.TempDir()
	pythonFile := filepath.Join(tempDir, "pkg", "module.py")
	generatedFile := filepath.Join(tempDir, "pkg", "generated", "script.py")
	mustWriteTestFile(t, pythonFile, "value = 1\n")
	mustWriteTestFile(t, generatedFile, "value = 1\n")

	files := normalizeTypeCheckFiles(
		[]string{pythonFile, generatedFile},
		[]string{"/generated/"},
	)
	if len(files) != 1 || files[0] != pythonFile {
		t.Fatalf("normalizeTypeCheckFiles() = %#v, want [%q]", files, pythonFile)
	}
}

func TestCheckTypeCheckersCommandReportsFailures(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  type_check:
    enabled: true
    checkers:
      - name: ok
        command:
          - /bin/sh
          - -lc
          - exit 0
        pass_files_as_args: false
        use_hook_project: false
      - name: fail
        command:
          - /bin/sh
          - -lc
          - printf 'type failure\n'; exit 1
        pass_files_as_args: false
        use_hook_project: false
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatHuman)

	pythonPath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(t, pythonPath, "value = 1\n")

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if got := checkTypeCheckersCommand(Config{}, []string{pythonPath}); got != 1 {
				t.Fatalf("checkTypeCheckersCommand() = %d, want 1", got)
			}
		})
		if !strings.Contains(
			stderr,
			"Python static analysis failed in one or more configured checkers",
		) {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if !strings.Contains(
			stderr,
			"Fix the reported checker output above and run the hook again.",
		) {
			t.Fatalf("missing remediation guidance in stderr: %q", stderr)
		}
	})
	if !strings.Contains(stdout, "PYTHON STATIC CHECKS (PARALLEL)") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "fail: FAIL") {
		t.Fatalf("missing failing checker report: %q", stdout)
	}
}
