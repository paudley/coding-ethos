// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/lint"
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
	conditionalImportSkill, ok := bundle.Skills["conditional-imports"]
	if !ok {
		t.Fatalf("missing compiled conditional import skill: %#v", bundle.Skills)
	}
	if conditionalImportSkill.Source.Path != "skills.conditional-imports" ||
		!slices.Contains(conditionalImportSkill.PrincipleIDs, "no-conditional-imports") ||
		!strings.Contains(conditionalImportSkill.ShortHint, "Protocol") {
		t.Fatalf("compiled conditional import skill mismatch: %#v", conditionalImportSkill)
	}
	if bundle.Advice.Reminders.AmbientFrequencyPercent != 25 ||
		len(bundle.Advice.Reminders.Items) == 0 {
		t.Fatalf("missing compiled reminder advice: %#v", bundle.Advice.Reminders)
	}
	conditionalImportReminder := reminderByPrincipleID(
		bundle.Advice.Reminders.Items,
		"no-conditional-imports",
	)
	if conditionalImportReminder == nil {
		t.Fatalf("missing conditional import reminder: %#v", bundle.Advice.Reminders.Items)
	}
	if !strings.Contains(conditionalImportReminder.Axiom, "Conditional imports are banned") ||
		!strings.Contains(conditionalImportReminder.Action, "module-scope imports") {
		t.Fatalf("conditional import reminder missing expected guidance: %#v", conditionalImportReminder)
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
		"repo.required_ignores",
		"repo.pii_scrubber",
		"repo.license_header",
		"git.commitlint",
		"git.commit_attribution",
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

	if len(bundle.EvidenceMaps) != 28 {
		t.Fatalf("evidence map count = %d, want 28", len(bundle.EvidenceMaps))
	}
	conditionalImportEvidence := evidenceMapByPolicyID(
		bundle.EvidenceMaps,
		"python.conditional_imports",
	)
	if conditionalImportEvidence == nil {
		t.Fatalf("missing conditional import evidence map")
	}
	conditionalImportAdvice := strings.Join(
		append(
			[]string{
				conditionalImportEvidence.Meaning,
				conditionalImportEvidence.Advice.Summary,
			},
			conditionalImportEvidence.Advice.Steps...,
		),
		"\n",
	)
	for _, want := range []string{"SOLID", "Protocol", "startup validation"} {
		if !strings.Contains(conditionalImportAdvice, want) {
			t.Fatalf("conditional import evidence missing %q: %#v", want, conditionalImportEvidence)
		}
	}
	importCycleEvidence := evidenceMapByPolicyID(
		bundle.EvidenceMaps,
		"python.import_cycles",
	)
	if importCycleEvidence == nil {
		t.Fatalf("missing import cycle evidence map")
	}
	importCycleAdvice := strings.Join(
		append(
			[]string{
				importCycleEvidence.Meaning,
				importCycleEvidence.Advice.Summary,
			},
			importCycleEvidence.Advice.Steps...,
		),
		"\n",
	)
	for _, want := range []string{"Protocol", "neutral module", "concrete dependency"} {
		if !strings.Contains(importCycleAdvice, want) {
			t.Fatalf("import cycle evidence missing %q: %#v", want, importCycleEvidence)
		}
	}
	suppressionEvidence := evidenceMapByPolicyID(
		bundle.EvidenceMaps,
		"python.comment_suppressions",
	)
	if suppressionEvidence == nil {
		t.Fatalf("missing suppression evidence map")
	}
	if !strings.Contains(suppressionEvidence.Advice.Summary, "suppression") {
		t.Fatalf("suppression evidence advice mismatch: %#v", suppressionEvidence)
	}
	docEvidence := evidenceMapByPolicyID(bundle.EvidenceMaps, "docs.public_contract")
	if docEvidence == nil {
		t.Fatalf("missing docstring evidence map")
	}
	if !strings.Contains(docEvidence.Advice.Summary, "contract") {
		t.Fatalf("docstring evidence advice mismatch: %#v", docEvidence)
	}
	optionalTypeEvidence := evidenceMapByPolicyID(
		bundle.EvidenceMaps,
		"python.optional_required_types",
	)
	if optionalTypeEvidence == nil {
		t.Fatalf("missing optional type evidence map")
	}
	if !strings.Contains(optionalTypeEvidence.Advice.Summary, "required") {
		t.Fatalf("optional type advice mismatch: %#v", optionalTypeEvidence)
	}
	unknownTypeEvidence := evidenceMapByPolicyID(bundle.EvidenceMaps, "python.unknown_types")
	if unknownTypeEvidence == nil {
		t.Fatalf("missing unknown type evidence map")
	}
	if !strings.Contains(unknownTypeEvidence.Advice.Summary, "Any") {
		t.Fatalf("unknown type advice mismatch: %#v", unknownTypeEvidence)
	}
	interfaceEvidence := evidenceMapByPolicyID(
		bundle.EvidenceMaps,
		"python.interface_contracts",
	)
	if interfaceEvidence == nil {
		t.Fatalf("missing interface evidence map")
	}
	if !strings.Contains(interfaceEvidence.Advice.Summary, "interface") {
		t.Fatalf("interface advice mismatch: %#v", interfaceEvidence)
	}

	forbiddenStrings := optionStrings(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"strings",
	)
	if !slices.Contains(forbiddenStrings, "header must match") {
		t.Fatalf("default forbidden strings missing hook recon marker: %#v", forbiddenStrings)
	}
	if !slices.Contains(forbiddenStrings, "coding-ethos-hooks/coding-ethos-git-hook") {
		t.Fatalf("default forbidden strings missing hook binary path: %#v", forbiddenStrings)
	}
	if !slices.Contains(forbiddenStrings, "coding-ethos-hooks/bin/coding-ethos-policy") {
		t.Fatalf("default forbidden strings missing shared policy tool path: %#v", forbiddenStrings)
	}
	if slices.Contains(forbiddenStrings, "coding-ethos-hooks/coding-ethos-legacy-hook") {
		t.Fatalf("default forbidden strings still include removed legacy hook path: %#v", forbiddenStrings)
	}

	protectedPaths := optionStrings(
		t,
		bundle.Policies["filesystem.protected_path"].Evaluators[0],
		"paths",
	)
	if !slices.Contains(protectedPaths, "coding-ethos-hooks/coding-ethos-git-hook") {
		t.Fatalf("default protected paths missing hook cache: %#v", protectedPaths)
	}
	if !slices.Contains(protectedPaths, "coding-ethos-hooks/bin/coding-ethos-git") {
		t.Fatalf("default protected paths missing shared git wrapper: %#v", protectedPaths)
	}
	if slices.Contains(protectedPaths, "coding-ethos-hooks/coding-ethos-legacy-hook") {
		t.Fatalf("default protected paths still include removed legacy hook path: %#v", protectedPaths)
	}
}

func TestCompileExpressionPolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.no_subprocess_git
      scope: command
      severity: block
      principle_ids:
        - one-path-for-critical-operations
      skill_id: safe-git-workflow
      when: command.contains("subprocess") && command.contains("git")
      message: Git subprocesses are forbidden.
      advice: Use the protected Git wrapper.
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	policyDef, ok := bundle.Policies["custom.no_subprocess_git"]
	if !ok {
		t.Fatalf("missing expression policy: %#v", bundle.Policies)
	}
	if policyDef.Evaluators[0].Kind != "cel" ||
		policyDef.Evaluators[0].Name != "cel.expression" ||
		policyDef.Evaluators[0].Options["when"] == "" ||
		policyDef.Evaluators[0].Options["skill_id"] != "safe-git-workflow" {
		t.Fatalf("expression evaluator mismatch: %#v", policyDef.Evaluators[0])
	}
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "custom.no_subprocess_git")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "custom.no_subprocess_git")
	assertHookPolicyDispatched(
		t,
		bundle.Dispatch.Hooks["PreToolUse"]["Bash"],
		"custom.no_subprocess_git",
	)

	result, err := lint.Run(bundle, lint.Options{
		Scope:   lint.ScopeFiles,
		Command: "python -c 'import subprocess; subprocess.run([\"git\"])'",
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}
	if !result.Blocked() || result.Diagnostics[0].SkillID != "safe-git-workflow" {
		t.Fatalf("expression lint result = %#v", result)
	}
}

func TestCompileRejectsExpressionPolicyIDCollisions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: git.hook_bypass
      scope: command
      severity: block
      principle_ids:
        - one-path-for-critical-operations
      when: command.contains("git")
      message: Builtin replacement must fail.
      advice: Choose a unique custom policy ID.
`)

	_, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with an existing policy") {
		t.Fatalf("compile error = %v, want policy ID collision error", err)
	}
}

func TestCompileRejectsInvalidExpressionPolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.invalid
      scope: command
      principle_ids:
        - one-path-for-critical-operations
      when: command + 1
      message: Invalid expression.
      advice: Fix the expression.
`)

	_, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err == nil || !strings.Contains(err.Error(), "compile CEL policy") {
		t.Fatalf("compile error = %v, want CEL compile failure", err)
	}
}

func TestCompileRejectsInvalidExpressionPolicyContracts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		expression string
		want       string
	}{
		{
			name: "unsafe host function",
			expression: `
    - id: custom.unsafe
      principle_ids: [one-path-for-critical-operations]
      when: now() > timestamp("2026-01-01T00:00:00Z")
      message: Invalid expression.
      advice: Fix the expression.
`,
			want: "compile CEL policy",
		},
		{
			name: "unknown variable",
			expression: `
    - id: custom.unknown
      principle_ids: [one-path-for-critical-operations]
      when: env.HOME != ""
      message: Invalid expression.
      advice: Fix the expression.
`,
			want: "compile CEL policy",
		},
		{
			name: "type error",
			expression: `
    - id: custom.type_error
      principle_ids: [one-path-for-critical-operations]
      when: command + 1
      message: Invalid expression.
      advice: Fix the expression.
`,
			want: "compile CEL policy",
		},
		{
			name: "non boolean when",
			expression: `
    - id: custom.non_bool
      principle_ids: [one-path-for-critical-operations]
      when: command
      message: Invalid expression.
      advice: Fix the expression.
`,
			want: "when expression must return bool",
		},
		{
			name: "missing ethos mapping",
			expression: `
    - id: custom.missing_ethos
      when: command.contains("git")
      message: Invalid expression.
      advice: Fix the expression.
`,
			want: "principle_ids is required",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			primaryPath := filepath.Join(dir, "coding_ethos.yml")
			configPath := filepath.Join(dir, "config.yaml")

			writeTestFile(t, primaryPath, testEthosYAML)
			writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
`+testCase.expression)

			_, _, err := Compile(CompileOptions{
				Primary: primaryPath,
				Config:  configPath,
			})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("compile error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestCompileRejectsInvalidExpressionOverrideMerge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)
	writeTestFile(t, repoConfigPath, `
policy:
  expressions:
    id: custom.invalid_overlay
`)

	_, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err == nil || !strings.Contains(err.Error(), "policy.expressions must be a list") {
		t.Fatalf("compile error = %v, want invalid expression overlay error", err)
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
		"repo.required_ignores",
	)
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["cutover"],
		"repo.required_ignores",
	)
	assertPolicyDispatched(
		t,
		bundle.Dispatch.Linter["staged"],
		"repo.required_ignores",
	)
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "syntax.file_syntax")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "shell.best_practices")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "syntax.merge_conflict")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "security.private_key")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.shebangs")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.large_files")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "filesystem.line_limits")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "repo.pii_scrubber")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "repo.license_header")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "shell.forbidden_strings")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "syntax.file_syntax")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "shell.best_practices")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "syntax.merge_conflict")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "security.private_key")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.shebangs")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.large_files")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "filesystem.line_limits")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "repo.pii_scrubber")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "repo.license_header")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["commit-msg"], "git.commitlint")
	assertPolicyDispatched(t, bundle.Dispatch.Linter["commit-msg"], "git.commit_attribution")
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

func TestCompileDoesNotInheritLicensePolicyIntoConsumerRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
filesystem:
  license_header:
    enabled: true
`)
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

	if _, ok := bundle.Policies["repo.license_header"]; ok {
		t.Fatalf("consumer repo should not inherit bundle license policy")
	}
}

func TestCompileAddsRepoSpecificLicensePolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)
	writeTestFile(t, repoConfigPath, `
repo:
  license:
    spdx_identifier: MIT
    copyright: 2026 Example Inc.
    text: |
      MIT License

      Copyright (c) <year> <copyright holders>
`)

	bundle, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	policyDef, ok := bundle.Policies["repo.license_header"]
	if !ok {
		t.Fatalf("missing repo-specific license policy")
	}
	if policyDef.Source.File != "repo_config.yaml" || policyDef.Source.Path != "repo.license" {
		t.Fatalf("source mismatch: %#v", policyDef.Source)
	}

	options := policyDef.Evaluators[0].Options
	required := optionStrings(t, policyDef.Evaluators[0], "required")
	if !slices.Contains(required, "SPDX-License-Identifier: MIT") {
		t.Fatalf("missing SPDX header requirement: %#v", required)
	}
	if !slices.Contains(required, "SPDX-FileCopyrightText: 2026 Example Inc.") {
		t.Fatalf("missing copyright header requirement: %#v", required)
	}
	expected, ok := options["expected_license_text"].(string)
	if !ok || !strings.Contains(expected, "Copyright (c) 2026 Example Inc.") {
		t.Fatalf("expected license text mismatch: %#v", options["expected_license_text"])
	}
}

func TestCompiledRepoLicensePolicyRunsAgainstSampleConsumer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")
	consumerRoot := filepath.Join(dir, "consumer")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML)
	writeTestFile(t, repoConfigPath, sampleLicenseRepoConfigYAML)
	if err := os.MkdirAll(consumerRoot, 0o755); err != nil {
		t.Fatalf("mkdir consumer: %v", err)
	}

	bundle, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	writeTestFile(t, filepath.Join(consumerRoot, "LICENSE"), "wrong\n")
	writeTestFile(t, filepath.Join(consumerRoot, "app.go"), sampleLicensedGoSource)

	result, err := lint.Run(bundle, lint.Options{
		Scope: lint.ScopeFiles,
		Cwd:   consumerRoot,
		Files: []string{"app.go"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}
	assertBlockedDiagnostic(t, result.Diagnostics, "license_file")

	writeTestFile(t, filepath.Join(consumerRoot, "LICENSE"), sampleExpectedLicenseText)
	writeTestFile(
		t,
		filepath.Join(consumerRoot, "app.go"),
		"// SPDX-License-Identifier: Apache-2.0\n\npackage main\n",
	)

	result, err = lint.Run(bundle, lint.Options{
		Scope: lint.ScopeFiles,
		Cwd:   consumerRoot,
		Files: []string{"app.go"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}
	assertBlockedDiagnostic(t, result.Diagnostics, "license_header")

	writeTestFile(t, filepath.Join(consumerRoot, "app.go"), sampleLicensedGoSource)
	writeTestFile(t, filepath.Join(consumerRoot, "config.yaml"), "name: app\n")

	result, err = lint.Run(bundle, lint.Options{
		Scope: lint.ScopeFiles,
		Cwd:   consumerRoot,
		Files: []string{"app.go", "config.yaml"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}
	if result.Status != "resolved" {
		t.Fatalf("sample consumer should pass, got %#v", result)
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
    exempt_paths: [config.yaml]
    file_strings: [BADCODE]
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
	forbiddenStringExempts := optionStrings(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"exempt_paths",
	)
	if forbiddenStringExempts[0] != "config.yaml" {
		t.Fatalf("forbidden string exempt options mismatch: %#v", forbiddenStringExempts)
	}
	forbiddenFileStrings := optionStrings(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"file_strings",
	)
	if forbiddenFileStrings[0] != "BADCODE" {
		t.Fatalf("forbidden file string options mismatch: %#v", forbiddenFileStrings)
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
      skill_id: managed-toolchain
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

	if len(bundle.EvidenceMaps) != 29 {
		t.Fatalf("evidence map count = %d, want 29", len(bundle.EvidenceMaps))
	}

	evidenceMap := bundle.EvidenceMaps[0]
	if evidenceMap.Source != "mypy" || evidenceMap.Codes[0] != "no-any-return" {
		t.Fatalf("evidence map mismatch: %#v", evidenceMap)
	}
	if evidenceMap.SkillID != "managed-toolchain" {
		t.Fatalf("evidence skill id mismatch: %#v", evidenceMap)
	}

	if evidenceMap.Advice.Summary != "Replace Any with a precise required type." {
		t.Fatalf("evidence advice mismatch: %#v", evidenceMap.Advice)
	}
}

func TestCompileDerivesReminderAdviceFromEthosPrinciples(t *testing.T) {
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

	reminders := bundle.Advice.Reminders
	if reminders.AmbientFrequencyPercent != 25 || len(reminders.Items) < 9 {
		t.Fatalf("reminder config mismatch: %#v", reminders)
	}
	reminder := reminderByPrincipleID(
		reminders.Items,
		"evidence-based-engineering-and-decision-quality",
	)
	if reminder == nil || reminder.Axiom != "Evidence requires verification." {
		t.Fatalf("missing ethos-derived reminder: %#v", reminders.Items)
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
  pii_scrubber:
    enabled: false
  license_header:
    enabled: false
shell:
  dangerous_command:
    enabled: false
go:
  commitlint:
    enabled: false
  commit_attribution:
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
		"repo.required_ignores",
		"repo.pii_scrubber",
		"repo.license_header",
		"shell.dangerous_command",
		"git.commitlint",
		"git.commit_attribution",
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

func evidenceMapByPolicyID(
	maps []diagnostics.EvidenceMap,
	policyID string,
) *diagnostics.EvidenceMap {
	for index := range maps {
		if maps[index].PolicyID == policyID {
			return &maps[index]
		}
	}

	return nil
}

func reminderByPrincipleID(
	items []EthosReminder,
	principleID string,
) *EthosReminder {
	for index := range items {
		if items[index].PrincipleID == principleID {
			return &items[index]
		}
	}

	return nil
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

func assertHookPolicyDispatched(
	t *testing.T,
	entries []HookDispatchEntry,
	expected string,
) {
	t.Helper()

	for _, entry := range entries {
		if entry.PolicyID == expected {
			return
		}
	}

	t.Fatalf("hook dispatch missing %q: %#v", expected, entries)
}

func assertBlockedDiagnostic(
	t *testing.T,
	diagnostics []diagnostics.Diagnostic,
	tool string,
) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Tool == tool {
			return
		}
	}

	t.Fatalf("missing diagnostic tool %q: %#v", tool, diagnostics)
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
    axioms:
      - axiom: Conditional imports are banned.
        action: Use module-scope imports and Protocol boundaries.
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
  - id: evidence-based-engineering-and-decision-quality
    order: 26
    title: Evidence-Based Engineering and Decision Quality
    summary: Evidence outranks assumptions.
    directive: Understand, plan, execute, and validate with evidence.
    axioms:
      - axiom: Evidence requires verification.
        action: Run the relevant check before claiming completion.
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
skills:
  - id: conditional-imports
    title: Conditional Imports
    description: Replace conditional imports with explicit dependencies or protocol boundaries.
    principle_ids: [no-conditional-imports]
    trigger_terms: [ruff local import, import cycle]
    short_hint: Use Protocol boundaries when module-scope imports expose cycles.
    focus: Remove hidden dependency paths.
    remediation_steps:
      - Move required imports to module scope.
      - Use a neutral Protocol when concrete modules would otherwise cycle.
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

const sampleLicenseRepoConfigYAML = `
repo:
  license:
    spdx_identifier: MIT
    copyright: 2026 Example Inc.
    text: |
      MIT License

      Copyright (c) <year> <copyright holders>
`

const sampleExpectedLicenseText = `MIT License

Copyright (c) 2026 Example Inc.
`

const sampleLicensedGoSource = `// SPDX-FileCopyrightText: 2026 Example Inc.
// SPDX-License-Identifier: MIT

package main
`
