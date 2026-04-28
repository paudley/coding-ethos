// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCompileBuildsBundleFromYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)

	bundle, metadata, err := Compile(CompileOptions{
		Primary:     primaryPath,
		Config:      configPath,
		BundleID:    "test-bundle",
		GeneratedAt: "2026-04-24T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if bundle.BundleID != "test-bundle" {
		t.Fatalf("bundle id mismatch: got %q", bundle.BundleID)
	}

	if _, ok := bundle.Principles["no-conditional-imports"]; !ok {
		t.Fatalf("missing compiled principle")
	}

	if _, ok := bundle.Policies["python.conditional_imports"]; !ok {
		t.Fatalf("missing compiled conditional import policy")
	}

	if _, ok := bundle.Policies["pytest.gate"]; !ok {
		t.Fatalf("missing compiled pytest gate policy")
	}
	if _, ok := bundle.Policies["syntax.file_syntax"]; !ok {
		t.Fatalf("missing compiled syntax policy")
	}
	if _, ok := bundle.Policies["shell.best_practices"]; !ok {
		t.Fatalf("missing compiled shell best practices policy")
	}
	for _, policyID := range []string{
		"syntax.merge_conflict",
		"security.private_key",
		"filesystem.shebangs",
		"filesystem.large_files",
		"filesystem.line_limits",
	} {
		if _, ok := bundle.Policies[policyID]; !ok {
			t.Fatalf("missing compiled file guard policy %s", policyID)
		}
	}

	if bundle.Policies["git.hook_bypass"].DefenseLayers.Notify != "on_block" {
		t.Fatalf(
			"missing notify defense layer: %#v",
			bundle.Policies["git.hook_bypass"].DefenseLayers,
		)
	}

	if _, ok := bundle.Policies["python.structured_logging"]; ok {
		t.Fatalf("structured logging policy should be disabled by fixture config")
	}

	if metadata.BundleHash == "" {
		t.Fatalf("metadata missing bundle hash")
	}

	if metadata.SourceHashes[primaryPath] == "" ||
		metadata.SourceHashes[configPath] == "" {
		t.Fatalf("metadata missing source hashes: %#v", metadata.SourceHashes)
	}

	if len(bundle.EvidenceMaps) != 9 {
		t.Fatalf("evidence map count = %d, want 9", len(bundle.EvidenceMaps))
	}

	forbiddenStrings := optionStrings(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"strings",
	)
	if !slices.Contains(forbiddenStrings, "header must match") {
		t.Fatalf("default forbidden strings missing hook recon marker: %#v", forbiddenStrings)
	}
}

func TestCompileDispatchesExecutableSmokePoliciesOutsideStagedScope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for _, policyID := range bundle.Dispatch.Linter["staged"] {
		if policyID == "pytest.gate" || policyID == "generated_config.freshness" {
			t.Fatalf("smoke-only policy should not be in staged dispatch: %q", policyID)
		}
	}

	assertPolicyDispatched(t, bundle.Dispatch.Linter["smoke"], "pytest.gate")
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["smoke"],
		"generated_config.freshness",
	)
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["smoke"],
		"filesystem.required_ignores",
	)
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["cutover"],
		"filesystem.required_ignores",
	)
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["staged"],
		"filesystem.required_ignores",
	)
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "syntax.file_syntax")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "shell.best_practices")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "syntax.merge_conflict")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "security.private_key")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.shebangs")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.large_files")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.line_limits")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "syntax.file_syntax")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "shell.best_practices")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "syntax.merge_conflict")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "security.private_key")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.shebangs")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.large_files")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.line_limits")
}

func TestCompileHonorsRepoConfigOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)
	writeTestFile(t, repoConfigPath, `
python:
  structured_logging:
    enabled: true
`)

	bundle, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, ok := bundle.Policies["python.structured_logging"]; !ok {
		t.Fatalf("repo overlay should enable structured logging policy")
	}
}

func TestCompileAddsEvaluatorOptionsFromConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
git:
  staged_admin_files:
    basenames: [custom.lock]
    dirs: [ops]
filesystem:
  protected_path:
    paths: [/opt/blocked]
  protected_branch_write:
    branches: [release]
    exempt_path_prefixes: [docs/plans/]
  required_ignores:
    paths: [.runtime/]
  large_files:
    suffixes: [.txt]
    exclude_prefixes: [vendor/]
    max_kb: 7
  line_limits:
    python_hard: 12
    shell_hard: 8
generated_config:
  freshness:
    check_command: [coding-ethos, --repo, /tmp/repo, --check-tool-configs]
shell:
  best_practices:
    require_common_for_prefixes: [bin/]
  forbidden_strings:
    strings: [/blocked/settings.json]
syntax:
  file_syntax:
    extensions: [.json]
  merge_conflict:
    markers: [CONFLICT]
security:
  private_key:
    pattern: PRIVATE KEY
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	protectedPath := optionStrings(
		t,
		bundle.Policies["filesystem.protected_path"].Evaluators[0],
		"paths",
	)
	if protectedPath[0] != "/opt/blocked" {
		t.Fatalf("protected path options mismatch: %#v", protectedPath)
	}

	protectedBranch := optionStrings(
		t,
		bundle.Policies["filesystem.protected_branch_write"].Evaluators[0],
		"branches",
	)
	if protectedBranch[0] != "release" {
		t.Fatalf("protected branch options mismatch: %#v", protectedBranch)
	}

	requiredIgnores := optionStrings(
		t,
		bundle.Policies["filesystem.required_ignores"].Evaluators[0],
		"paths",
	)
	if requiredIgnores[0] != ".runtime/" {
		t.Fatalf("required ignore options mismatch: %#v", requiredIgnores)
	}

	adminFiles := optionStrings(
		t,
		bundle.Policies["git.staged_admin_files"].Evaluators[0],
		"basenames",
	)
	if adminFiles[0] != "custom.lock" {
		t.Fatalf("admin file options mismatch: %#v", adminFiles)
	}

	pytestCommand := optionStrings(
		t,
		bundle.Policies["pytest.gate"].Evaluators[0],
		"command",
	)
	if pytestCommand[0] != "uv" {
		t.Fatalf("pytest command options mismatch: %#v", pytestCommand)
	}

	generatedConfigCommand := optionStrings(
		t,
		bundle.Policies["generated_config.freshness"].Evaluators[0],
		"command",
	)
	if generatedConfigCommand[2] != "/tmp/repo" {
		t.Fatalf("generated config command options mismatch: %#v", generatedConfigCommand)
	}

	forbiddenStrings := optionStrings(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"strings",
	)
	if forbiddenStrings[0] != "/blocked/settings.json" {
		t.Fatalf("forbidden strings options mismatch: %#v", forbiddenStrings)
	}

	shellPrefixes := optionStrings(
		t,
		bundle.Policies["shell.best_practices"].Evaluators[0],
		"require_common_for_prefixes",
	)
	if shellPrefixes[0] != "bin/" {
		t.Fatalf("shell best-practices options mismatch: %#v", shellPrefixes)
	}

	syntaxExtensions := optionStrings(
		t,
		bundle.Policies["syntax.file_syntax"].Evaluators[0],
		"extensions",
	)
	if syntaxExtensions[0] != ".json" {
		t.Fatalf("syntax options mismatch: %#v", syntaxExtensions)
	}

	mergeMarkers := optionStrings(
		t,
		bundle.Policies["syntax.merge_conflict"].Evaluators[0],
		"markers",
	)
	if mergeMarkers[0] != "CONFLICT" {
		t.Fatalf("merge marker options mismatch: %#v", mergeMarkers)
	}

	privateKeyPattern := optionString(
		t,
		bundle.Policies["security.private_key"].Evaluators[0],
		"pattern",
	)
	if privateKeyPattern != "PRIVATE KEY" {
		t.Fatalf("private key pattern mismatch: %q", privateKeyPattern)
	}

	largeFileSuffixes := optionStrings(
		t,
		bundle.Policies["filesystem.large_files"].Evaluators[0],
		"suffixes",
	)
	if largeFileSuffixes[0] != ".txt" {
		t.Fatalf("large file suffix options mismatch: %#v", largeFileSuffixes)
	}

	lineLimit := optionInt(
		t,
		bundle.Policies["filesystem.line_limits"].Evaluators[0],
		"python_hard",
	)
	if lineLimit != 12 {
		t.Fatalf("line limit option mismatch: %d", lineLimit)
	}
}

func TestCompileHonorsConfiguredEvidenceMaps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  evidence_maps:
    - source: mypy
      codes: [no-any-return]
      policy_id: python.optional_returns
      principle_ids: [no-optional-types-for-required-dependencies]
      confidence: medium
      meaning: Return type leaks Any.
      advice:
        summary: Replace Any with a precise required type.
        steps: [Tighten the annotation.]
        rerun: [make pre-commit]
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if len(bundle.EvidenceMaps) != 10 {
		t.Fatalf("evidence map count = %d, want 10", len(bundle.EvidenceMaps))
	}

	evidenceMap := bundle.EvidenceMaps[0]
	if evidenceMap.Source != "mypy" || evidenceMap.Codes[0] != "no-any-return" {
		t.Fatalf("evidence map mismatch: %#v", evidenceMap)
	}

	if evidenceMap.Advice.Summary != "Replace Any with a precise required type." {
		t.Fatalf("evidence advice mismatch: %#v", evidenceMap.Advice)
	}
}

func TestCompileHonorsPolicyEnabledFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
git:
  hook_bypass:
    enabled: false
filesystem:
  protected_path:
    enabled: false
  required_ignores:
    enabled: false
shell:
  dangerous_command:
    enabled: false
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for _, policyID := range []string{
		"git.hook_bypass",
		"filesystem.protected_path",
		"filesystem.required_ignores",
		"shell.dangerous_command",
	} {
		if _, ok := bundle.Policies[policyID]; ok {
			t.Fatalf("policy should be disabled: %s", policyID)
		}
	}
}

func optionStrings(t *testing.T, evaluator Evaluator, key string) []string {
	t.Helper()

	items, ok := evaluator.Options[key].([]string)
	if !ok {
		t.Fatalf("option %q is not []string: %#v", key, evaluator.Options[key])
	}

	return items
}

func optionString(t *testing.T, evaluator Evaluator, key string) string {
	t.Helper()

	item, ok := evaluator.Options[key].(string)
	if !ok {
		t.Fatalf("option %q is not string: %#v", key, evaluator.Options[key])
	}

	return item
}

func optionInt(t *testing.T, evaluator Evaluator, key string) int {
	t.Helper()

	item, ok := evaluator.Options[key].(int)
	if !ok {
		t.Fatalf("option %q is not int: %#v", key, evaluator.Options[key])
	}

	return item
}

func TestCompileRejectsMissingPrinciples(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, "version: 2\n")
	writeTestFile(t, configPath, testConfigYAML)

	_, _, err := Compile(CompileOptions{Primary: primaryPath, Config: configPath})
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func assertPolicyDispatched(t *testing.T, policyIDs []string, expected string) {
	t.Helper()

	if slices.Contains(policyIDs, expected) {
		return
	}

	t.Fatalf("dispatch missing %q: %#v", expected, policyIDs)
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const testEthosYAML = `
version: 2
principles:
  - id: solid-is-law
    order: 1
    title: SOLID is Law
    summary: Keep design simple.
    directive: Enforce SOLID and simplicity.
  - id: security-by-design
    order: 24
    title: Security by Design
    summary: Design for safe defaults.
    directive: Prevent secrets and unsafe defaults.
  - id: no-conditional-imports
    order: 3
    title: No Conditional Imports
    summary: Required imports are hard dependencies.
    directive: Treat required imports as hard dependencies and fail immediately.
    tags: [dependency, startup]
  - id: one-path-for-critical-operations
    order: 19
    title: One Path for Critical Operations
    summary: Critical operations use canonical gates.
    directive: Keep one explicit, validated path for critical operations.
  - id: linting-as-code-quality-enforcement
    order: 14
    title: Linting as Code Quality Enforcement
    summary: Hooks are mandatory quality gates.
    directive: Resolve lint findings structurally.
  - id: testing-as-specification
    order: 22
    title: Testing as Specification
    summary: Tests are executable behavioral contracts.
    directive: Treat tests as executable behavioral contracts.
  - id: radical-visibility
    order: 11
    title: Radical Visibility
    summary: Log important decisions.
    directive: Log important decisions with context.
  - id: validation-at-the-gate
    order: 8
    title: Validation at the Gate
    summary: Validate inputs before use.
    directive: Validate configuration and syntax before relying on files.
`

const testConfigYAML = `
version: 1
python:
  conditional_imports:
    enabled: true
  optional_returns:
    enabled: false
  catch_and_silence:
    enabled: false
  structured_logging:
    enabled: false
  direct_imports:
    enabled: false
  pytest_gate:
    enabled: true
    test_command: [uv, run, pytest]
`
