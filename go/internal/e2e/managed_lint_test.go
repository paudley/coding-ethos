// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

const windowsGOOS = "windows"

func TestManagedRuffCaptureRunsRealToolAgainstReferenceRepo(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	clean := repo.CodingEthosRun(t, "policy-tool", "ruff", "check", "pkg/clean.py")
	clean.RequireExit(t, 0)

	assertCleanRuffOutput(t, clean.Combined)

	if strings.TrimSpace(clean.Combined) != "" {
		t.Fatalf("clean output should be silent:\n%s", clean.Combined)
	}

	cleanTrace := repo.SingleTrace(t)
	assertCleanRuffTrace(t, cleanTrace)

	repo.ResetTraces(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())
	finding := repo.CodingEthosRun(
		t,
		"policy-tool",
		"ruff",
		"check",
		"pkg/unused_import.py",
	)
	finding.RequireExit(t, 1)
	assertRuffFindingOutput(t, finding)

	findingTrace := repo.SingleTrace(t)
	assertRuffFindingTrace(t, findingTrace)
}

func preparedManagedLintRepo(t *testing.T) e2e.Repo {
	t.Helper()

	if testing.Short() {
		t.Skip("real managed tool e2e is skipped in short mode")
	}

	if runtime.GOOS == windowsGOOS {
		t.Skip("real managed tool e2e uses POSIX paths")
	}

	sourceRoot := repoRootFromWorkingDirectory(t)
	e2e.RequireRuntime(t, sourceRoot)
	runtimeRoot := e2e.MutableBinEthosRoot(t, sourceRoot)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	repo.EthosRoot = runtimeRoot

	sync := repo.CodingEthosRun(
		t,
		"policy",
		"sync-tool-configs",
		"--ethos-root",
		runtimeRoot,
		"--repo",
		repo.Root,
	)
	sync.RequireExit(t, 0)

	syncGemini := repo.CodingEthosRun(
		t,
		"policy",
		"sync-gemini-prompts",
		"--ethos-root",
		runtimeRoot,
		"--repo",
		repo.Root,
		"--primary",
		filepath.Join(runtimeRoot, "coding_ethos.yml"),
		"--repo-ethos",
		filepath.Join(runtimeRoot, "repo_ethos.yml"),
	)
	syncGemini.RequireExit(t, 0)

	return repo
}

func assertCleanRuffOutput(t *testing.T, output string) {
	t.Helper()

	for _, unwanted := range []string{
		"tool.output_visible",
		"ruff emitted output while passing",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("clean output contained %q:\n%s", unwanted, output)
		}
	}
}

func assertCleanRuffTrace(t *testing.T, cleanTrace string) {
	t.Helper()

	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"tool": "ruff"`,
		`"parser": "ruff"`,
		`"parse_status": "empty"`,
		`"exit_code": 0`,
		`"--output-format=json"`,
	} {
		if !strings.Contains(cleanTrace, want) {
			t.Fatalf("clean trace missing %q:\n%s", want, cleanTrace)
		}
	}

	if strings.Contains(cleanTrace, `"policy_id": "tool.output_visible"`) {
		t.Fatalf(
			"clean trace should not contain output-visible policy:\n%s",
			cleanTrace,
		)
	}
}

func assertRuffFindingOutput(t *testing.T, finding e2e.CommandResult) {
	t.Helper()

	for _, want := range []string{
		"tool: ruff",
		"FAIL",
		"trace_id:",
		"pkg/unused_import.py",
		"imported but unused",
	} {
		finding.RequireContains(t, want)
	}
}

func assertRuffFindingTrace(t *testing.T, findingTrace string) {
	t.Helper()

	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"file": "pkg/unused_import.py"`,
		`"code": "F401"`,
		`"message": "`,
	} {
		if !strings.Contains(findingTrace, want) {
			t.Fatalf("finding trace missing %q:\n%s", want, findingTrace)
		}
	}
}

func TestManagedRuffCaptureProducesRealSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--sarif",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"check",
		"pkg/unused_import.py",
	)
	result.RequireExit(t, 1)
	assertRuffSARIFOutput(t, result)
}

func TestManagedRuffCaptureProducesJSONDiagnostics(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"check",
		"pkg/unused_import.py",
	)
	result.RequireExit(t, 1)

	for _, want := range []string{
		`"tool": "ruff"`,
		`"trace_id": "`,
		`"code": "F401"`,
		`"file": "pkg/unused_import.py"`,
		`"parse_status": "parsed"`,
	} {
		result.RequireContains(t, want)
	}
}

func TestManagedTSCCaptureRunsRealToolAgainstTypeScriptProject(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "tsconfig.json", typescriptConfig())
	repo.Touch(t, "src/index.ts", "const answer: number = 'forty-two';\n")

	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"tsc",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"tool": "tsc"`,
		`"code": "TS2322"`,
		`"file": "src/index.ts"`,
		`"policy_id": "typescript.static_analysis"`,
		`"parse_status": "parsed"`,
	} {
		result.RequireContains(t, want)
	}
}

func TestManagedTSCCaptureProducesRealSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "tsconfig.json", typescriptConfig())
	repo.Touch(t, "src/index.ts", "const answer: number = 'forty-two';\n")

	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--sarif",
		"--managed-capture-tool",
		"tsc",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"ruleId": "typescript.static_analysis"`,
		`"uri": "src/index.ts"`,
		`"source_tool": "tsc"`,
		`"code": "TS2322"`,
	} {
		result.RequireContains(t, want)
	}
}

func TestManagedKubeLinterCaptureRunsRealToolAgainstManifest(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "deploy/pod.yaml", unsafeKubernetesPod())

	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"kube-linter",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"lint",
		"--do-not-auto-add-defaults",
		"--include",
		"privileged-container",
		"deploy/pod.yaml",
	)
	result.RequireExit(t, 1)

	for _, want := range []string{
		`"tool": "kube-linter"`,
		`"code": "privileged-container"`,
		`"file": "deploy/pod.yaml"`,
		`"policy_id": "kubernetes.manifest_security"`,
		`"parse_status": "parsed"`,
	} {
		result.RequireContains(t, want)
	}
}

func TestManagedKubeLinterCaptureProducesRealSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "deploy/pod.yaml", unsafeKubernetesPod())

	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--sarif",
		"--managed-capture-tool",
		"kube-linter",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"lint",
		"--do-not-auto-add-defaults",
		"--include",
		"privileged-container",
		"deploy/pod.yaml",
	)
	result.RequireExit(t, 1)

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"ruleId": "kubernetes.manifest_security"`,
		`"uri": "deploy/pod.yaml"`,
		`"source_tool": "kube-linter"`,
		`"code": "privileged-container"`,
	} {
		result.RequireContains(t, want)
	}
}

func TestGeneratedConfigDriftProducesTraceAndSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	bundlePath := generatedConfigDriftBundle(t, repo)
	repo.ResetTraces(t)
	repo.Touch(t, "ruff.toml", "# intentionally stale generated config\n")
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--bundle",
		bundlePath,
		"--sarif",
		"--scope",
		"smoke",
		"--cwd",
		repo.Root,
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"ruleId": "generated_config.freshness"`,
		`"uri": "ruff.toml"`,
		`"code": "generated-config-drift"`,
		`"source_tool": "generated-config"`,
	} {
		result.RequireContains(t, want)
	}

	trace := repo.SingleTrace(t)
	for _, want := range []string{
		`"scope": "smoke"`,
		`"policy_id": "generated_config.freshness"`,
		`"tool": "generated-config"`,
		`"file": "ruff.toml"`,
		`"code": "generated-config-drift"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("generated config trace missing %q:\n%s", want, trace)
		}
	}
}

func TestGeneratedGeminiPromptDriftProducesTraceAndSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	bundlePath := generatedConfigDriftBundle(t, repo)
	repo.ResetTraces(t)
	repo.Touch(
		t,
		".coding-ethos/gemini/prompt-pack.json",
		"{\"intentionally\":\"stale\"}\n",
	)
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--bundle",
		bundlePath,
		"--sarif",
		"--scope",
		"smoke",
		"--cwd",
		repo.Root,
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"ruleId": "generated_gemini_prompts.freshness"`,
		`"uri": ".coding-ethos/gemini/prompt-pack.json"`,
		`"code": "generated-gemini-prompt-pack-drift"`,
		`"source_tool": "generated-gemini-prompts"`,
	} {
		result.RequireContains(t, want)
	}

	trace := repo.SingleTrace(t)
	for _, want := range []string{
		`"scope": "smoke"`,
		`"policy_id": "generated_gemini_prompts.freshness"`,
		`"tool": "generated-gemini-prompts"`,
		`"file": ".coding-ethos/gemini/prompt-pack.json"`,
		`"code": "generated-gemini-prompt-pack-drift"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("generated Gemini prompt trace missing %q:\n%s", want, trace)
		}
	}
}

func generatedConfigDriftBundle(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	repoConfigPath := filepath.Join(t.TempDir(), "repo_config.yaml")

	err := os.WriteFile(
		repoConfigPath,
		[]byte("python:\n  pytest_gate:\n    enabled: false\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write generated-config e2e repo config: %v", err)
	}

	outDir := filepath.Join(t.TempDir(), "policy")
	compile := repo.CodingEthosRun(
		t,
		"policy",
		"compile",
		"--out-dir",
		outDir,
		"--primary",
		filepath.Join(repo.EthosRoot, "coding_ethos.yml"),
		"--repo-ethos",
		filepath.Join(repo.EthosRoot, "repo_ethos.yml"),
		"--config",
		filepath.Join(repo.EthosRoot, "config.yaml"),
		"--repo-config",
		repoConfigPath,
	)
	compile.RequireExit(t, 0)

	return filepath.Join(outDir, "policy-bundle.json")
}

func TestManagedRuffCaptureKeepsRealToolFailureEvidence(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"check",
		"--definitely-not-a-ruff-flag",
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"tool": "ruff"`,
		`"policy_id": "tool.ruff"`,
		`"parse_status": "tool_config_error"`,
		`"category": "configuration_error"`,
		`"--definitely-not-a-ruff-flag"`,
	} {
		result.RequireContains(t, want)
	}
}

func assertRuffSARIFOutput(t *testing.T, result e2e.CommandResult) {
	t.Helper()

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"tool": {`,
		`"ruleId": "`,
		`"uri": "pkg/unused_import.py"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
	} {
		result.RequireContains(t, want)
	}
}

func unusedImportPython() string {
	return `# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: AGPL-3.0-only

"""Temporary e2e fixture containing a real Ruff F401 finding."""

import json


def answer() -> int:
    """Return a stable value."""
    return 42
`
}

func typescriptConfig() string {
	return `{
  "compilerOptions": {
    "strict": true,
    "target": "ES2022"
  },
  "include": ["src/**/*.ts"]
}
`
}

func unsafeKubernetesPod() string {
	return "apiVersion: v1\n" +
		"kind: Pod\n" +
		"metadata:\n" +
		"  name: unsafe-pod\n" +
		"spec:\n" +
		"  containers:\n" +
		"    - name: app\n" +
		"      image: nginx:latest\n" +
		"      securityContext:\n" +
		"        privileged: true\n"
}

func repoRootFromWorkingDirectory(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for current := workingDirectory; ; current = filepath.Dir(current) {
		_, inlineErrAutoA := os.Stat(filepath.Join(current, "coding_ethos.yml"))
		if inlineErrAutoA == nil {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("could not find coding-ethos root from %s", workingDirectory)
		}
	}
}
