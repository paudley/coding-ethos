// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:paralleltest,tparallel,lll,varnamelen // Uses process-global fixtures.
package hookrunnercli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	diag "blackcat.ca/coding-ethos/go/diagnostics"
)

func stringSliceContains(values []string, want string) bool {
	return slices.Contains(values, want)
}

func TestResolveTypeCheckerCommandInjectsRepoConfig(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestDefaultTypeCheckersCarryParserNames(t *testing.T) {
	t.Parallel()

	checkers := defaultTypeCheckers()

	for _, checker := range checkers {
		if checker.Parser == "" {
			t.Fatalf("checker %q missing parser: %#v", checker.Name, checker)
		}
	}
}

func TestConfiguredTypeCheckersExcludeDisabledPylintByDefault(t *testing.T) {
	t.Parallel()

	checkers := configuredTypeCheckers(typeCheckSettings{
		Checkers: defaultTypeCheckers(),
	})

	names := make(map[string]bool, len(checkers))
	for _, checker := range checkers {
		names[checker.Name] = true
	}

	if names["pylint"] {
		t.Fatalf("configuredTypeCheckers() enabled pylint by default: %#v", checkers)
	}

	for _, name := range []string{"ruff", "mypy", "pyright"} {
		if !names[name] {
			t.Fatalf(
				"configuredTypeCheckers() missing enabled checker %q: %#v",
				name,
				checkers,
			)
		}
	}
}

func TestParseTypeCheckDiagnostics(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		checker string
		output  string
		want    diag.Diagnostic
	}{
		{
			name:    "ruff",
			checker: "ruff",
			output:  `[{"filename":"pkg/app.py","code":"F401","message":"unused import","location":{"row":3,"column":5}}]`,
			want: diag.Diagnostic{
				Tool:     "ruff",
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
			want: diag.Diagnostic{
				Tool:     "pyright",
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
			want: diag.Diagnostic{
				Tool:     "mypy",
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
			want: diag.Diagnostic{
				Tool:     "pylint",
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
			got := diag.Parse(tc.checker, tc.output, "")
			if len(got) != 1 {
				t.Fatalf("diagnostics.Parse() = %#v, want one diagnostic", got)
			}

			if !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("diagnostics.Parse()[0] = %#v, want %#v", got[0], tc.want)
			}
		})
	}
}

func TestFormatTypeCheckResultsGroupsDiagnosticsByFile(t *testing.T) {
	t.Parallel()

	output := formatTypeCheckResults(
		[]typeCheckResult{
			{
				Name:     "ruff",
				ExitCode: 1,
				Diagnostics: []diag.Diagnostic{
					{
						Tool:     "ruff",
						File:     "pkg/a.py",
						Severity: "error",
						Code:     "F401",
						Message:  "unused import",
						Line:     2,
						Column:   1,
					},
					{
						Tool:     "ruff",
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
	t.Parallel()

	output := formatTypeCheckResults(
		[]typeCheckResult{
			{
				Name:       "mypy",
				ExitCode:   1,
				DurationMS: 12,
				Diagnostics: []diag.Diagnostic{
					{
						Tool:     "mypy",
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
		"diagnostics[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice}:",
		"mypy,pkg/app.py,88,12,error,no-any-return,,,Returning Any from function declared to return bool,",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("TOON output missing %q:\n%s", fragment, output)
		}
	}
}

func TestFormatTypeCheckResultsJSONIncludesSummaryAndDiagnostics(t *testing.T) {
	t.Parallel()

	output := formatTypeCheckResults(
		[]typeCheckResult{
			{
				Name:       "pyright",
				ExitCode:   1,
				DurationMS: 7,
				Diagnostics: []diag.Diagnostic{{
					Tool:     "pyright",
					File:     "pkg/app.py",
					Severity: "error",
					Code:     "reportGeneralTypeIssues",
					Message:  "type mismatch",
					Line:     5,
					Column:   2,
				}},
			},
			{Name: "mypy", ExitCode: 0, DurationMS: 3},
		},
		1,
		hookOutputFormatJSON,
	)
	for _, fragment := range []string{
		`"format": "json"`,
		`"status": "FAIL"`,
		`"failed": 1`,
		`"name": "pyright"`,
		`"message": "type mismatch"`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("JSON output missing %q:\n%s", fragment, output)
		}
	}
}

func TestTypeCheckRawOutputsAreNotRendered(t *testing.T) {
	t.Parallel()

	results := []typeCheckResult{{
		Name:     "custom-checker",
		ExitCode: 1,
		Output:   "raw stdout that was not parsed",
	}}
	if got := typeCheckRawOutputsForResults(results); len(got) != 0 {
		t.Fatalf("raw outputs rendered despite parser failure: %#v", got)
	}
}

func TestFormatTypeCheckResultsTOONIncludesSkillAdvice(t *testing.T) {
	t.Parallel()

	output := formatTypeCheckResultsWithSkills(
		[]typeCheckResult{
			{
				Name:     "ruff",
				ExitCode: 1,
				Diagnostics: []diag.Diagnostic{
					{
						Tool:         "ruff",
						File:         "pkg/app.py",
						Severity:     "error",
						Code:         "PLC" + "0415",
						PolicyID:     "python.conditional_imports",
						SkillID:      "conditional-imports",
						PrincipleIDs: []string{"no-conditional-imports"},
						Message:      "import outside top-level",
						Line:         12,
					},
				},
			},
		},
		1,
		hookOutputFormatTOON,
		map[string]hookSkill{
			"conditional-imports": {
				ID:           "conditional-imports",
				ShortHint:    "Conditional imports are banned; use protocols to break cycles.",
				PrincipleIDs: []string{"no-conditional-imports"},
			},
		},
	)

	for _, fragment := range []string{
		"advice[1]{principle_id,skill_id,message,next}:",
		"no-conditional-imports,conditional-imports,Conditional imports are banned; use protocols to break cycles.,Load the conditional-imports skill for the remediation playbook.",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("TOON output missing %q:\n%s", fragment, output)
		}
	}
}

func TestEnrichTypeCheckDiagnosticsMapsPolicyEvidence(t *testing.T) {
	t.Parallel()

	diagnostics := diag.Enrich(
		[]diag.Diagnostic{
			{
				Tool: "ruff",
				Code: "PLC" + "0415",
			},
			{
				Tool: "ruff",
				Code: "F401",
			},
		},
		[]diag.EvidenceMap{
			{
				Source:       "ruff",
				Codes:        []string{"PLC" + "0415"},
				PolicyID:     "python.conditional_imports",
				SkillID:      "conditional-imports",
				PrincipleIDs: []string{"no-conditional-imports"},
				Confidence:   "high",
				Meaning:      "import away from module scope",
				Advice: diag.EvidenceAdvice{
					Summary: "Move required imports to module scope.",
					Steps:   []string{"Import at module scope."},
					Rerun:   []string{"make pre-commit"},
				},
			},
		},
	)

	if diagnostics[0].PolicyID != "python.conditional_imports" {
		t.Fatalf("mapped diagnostic policy = %q", diagnostics[0].PolicyID)
	}

	if diagnostics[0].Advice != "Move required imports to module scope." {
		t.Fatalf("mapped diagnostic advice = %q", diagnostics[0].Advice)
	}

	if diagnostics[0].SkillID != "conditional-imports" {
		t.Fatalf("mapped diagnostic skill = %q", diagnostics[0].SkillID)
	}

	if len(diagnostics[0].PrincipleIDs) != 1 ||
		diagnostics[0].PrincipleIDs[0] != "no-conditional-imports" {
		t.Fatalf("mapped diagnostic principles = %#v", diagnostics[0].PrincipleIDs)
	}

	if diagnostics[1].PolicyID != "" {
		t.Fatalf("unmapped diagnostic policy = %q, want empty", diagnostics[1].PolicyID)
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestStagedTypeCheckFilesBypassesCodingEthosGitShim(t *testing.T) {
	tempDir := t.TempDir()
	repo := filepath.Join(tempDir, "repo")
	pythonFile := filepath.Join(repo, "pkg", "module.py")
	mustWriteTestFile(t, pythonFile, "value = 1\n")
	t.Chdir(repo)

	realGit := filepath.Join(tempDir, "real", "git")
	mustWriteExecutable(
		t,
		realGit,
		"#!/bin/sh\n"+
			"if [ \"$1\" = --version ]; then printf 'git version 2.0.0\\n'; exit 0; fi\n"+
			"printf 'pkg/module.py\\n'\n",
	)

	shimGit := filepath.Join(tempDir, "shim", "git")
	mustWriteExecutable(
		t,
		shimGit,
		"#!/bin/sh\n"+
			"printf 'recursive shim should not run\\n' >&2\n"+
			"exit 96\n",
	)

	t.Setenv(consumerRootEnv, repo)
	t.Setenv("CODING_ETHOS_REAL_GIT", realGit)
	t.Setenv(
		"PATH",
		filepath.Dir(shimGit)+string(os.PathListSeparator)+filepath.Dir(realGit),
	)

	files, err := stagedTypeCheckFiles(typeCheckSettings{})
	if err != nil {
		t.Fatalf("stagedTypeCheckFiles() error = %v", err)
	}

	if len(files) != 1 || files[0] != pythonFile {
		t.Fatalf("stagedTypeCheckFiles() = %#v, want [%q]", files, pythonFile)
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
