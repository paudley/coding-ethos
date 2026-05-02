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

	forbiddenWhen := stringOptionFromEvaluator(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"when",
	)
	if !strings.Contains(forbiddenWhen, "coding-ethos-hooks/coding-ethos-git-hook") {
		t.Fatalf("default forbidden strings missing hook binary path: %s", forbiddenWhen)
	}
	if !strings.Contains(forbiddenWhen, "coding-ethos-hooks/bin/coding-ethos-policy") {
		t.Fatalf("default forbidden strings missing shared policy tool path: %s", forbiddenWhen)
	}
	if strings.Contains(forbiddenWhen, "coding-ethos-hooks/coding-ethos-legacy-hook") {
		t.Fatalf("default forbidden strings still include removed legacy hook path: %s", forbiddenWhen)
	}

	protectedPaths := optionStrings(
		t,
		bundle.Policies["filesystem.protected_path"].Evaluators[0],
		"protected_paths",
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
      when: shell_commands.exists(cmd, cmd.name in ["python", "python3"] && cmd.argv.exists(arg, arg.contains("subprocess")) && cmd.argv.exists(arg, arg.contains("git")))
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
	for _, option := range []string{
		"config_candidates",
		"protected_branches",
		"protected_paths",
		"source_roots",
	} {
		if _, ok := policyDef.Evaluators[0].Options[option]; !ok {
			t.Fatalf("expression evaluator missing %q: %#v", option, policyDef.Evaluators[0])
		}
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

func TestCompileBuildsGitChangeDirAsCELPolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
git:
  change_dir_flag:
    enabled: true
  destructive_worktree:
    enabled: true
  stash_blocked:
    enabled: true
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	policyDef := bundle.Policies["git.change_dir_flag"]
	if len(policyDef.Evaluators) != 1 {
		t.Fatalf("git change-dir evaluator count = %d", len(policyDef.Evaluators))
	}
	evaluator := policyDef.Evaluators[0]
	if evaluator.Kind != "cel" ||
		evaluator.Name != "cel.expression" ||
		evaluator.Options["when"] != `git_command.is_git && git_command.has_change_dir` {
		t.Fatalf("git change-dir evaluator mismatch: %#v", evaluator)
	}
	assertHookPolicyDispatched(
		t,
		bundle.Dispatch.Hooks["PreToolUse"]["Bash"],
		"git.change_dir_flag",
	)
	assertPolicyDispatched(t, bundle.Dispatch.Linter["staged"], "git.change_dir_flag")
}

func TestCompileBuildsSmallGitPoliciesAsCELPolicies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
git:
  destructive_worktree:
    enabled: true
  stash_blocked:
    enabled: true
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for _, policyID := range []string{
		"git.destructive_worktree",
		"git.stash_blocked",
	} {
		policyDef := bundle.Policies[policyID]
		if len(policyDef.Evaluators) != 1 ||
			policyDef.Evaluators[0].Kind != "cel" ||
			policyDef.Evaluators[0].Name != "cel.expression" ||
			!strings.Contains(policyDef.Evaluators[0].Options["when"].(string), "git_command.") {
			t.Fatalf("%s evaluator mismatch: %#v", policyID, policyDef.Evaluators)
		}
	}
}

func TestCompileExpressionPolicyUsesExplicitDispatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.edit_python_only
      scope: files
      severity: block
      mode: advise
      hook_events: [PreToolUse]
      tools: [Edit]
      lint_scopes: [files]
      command_patterns: [python]
      path_patterns: ["**/*.py"]
      principle_ids: [one-path-for-critical-operations]
      skill_id: lint-remediation
      when: paths.exists(path, path.ext == ".py")
      message: Python edits require focused policy review.
      advice: Load the lint-remediation skill and fix structurally.
`)

	bundle, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	policyDef := bundle.Policies["custom.edit_python_only"]
	if !slices.Equal(policyDef.AppliesTo.Tools, []string{"Edit"}) {
		t.Fatalf("applies_to tools = %#v", policyDef.AppliesTo.Tools)
	}
	assertPolicyDispatched(t, bundle.Dispatch.Linter["files"], "custom.edit_python_only")
	assertPolicyNotDispatched(t, bundle.Dispatch.Linter["staged"], "custom.edit_python_only")
	assertHookPolicyNotDispatched(
		t,
		bundle.Dispatch.Hooks["PreToolUse"]["Bash"],
		"custom.edit_python_only",
	)
	entry := assertHookPolicyDispatched(
		t,
		bundle.Dispatch.Hooks["PreToolUse"]["Edit"],
		"custom.edit_python_only",
	)
	if entry.Mode != "advise" ||
		!slices.Equal(entry.CommandPatterns, []string{"python"}) ||
		!slices.Equal(entry.PathPatterns, []string{"**/*.py"}) {
		t.Fatalf("hook dispatch entry = %#v", entry)
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
    - id: git.commitlint
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

func TestCompileRejectsExpressionOverrideOfBuiltinPolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: git.commitlint
      override: true
      override_reason: Attempted replacement of built-in policy.
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("git")
      message: Builtin replacement must fail.
      advice: Choose a unique custom policy ID.
`)

	_, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot override protected policy") {
		t.Fatalf("compile error = %v, want built-in override rejection", err)
	}
}

func TestCompileRejectsInvalidExpressionDispatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		expression string
		want       string
	}{
		{
			name: "bad hook event",
			expression: `
    - id: custom.bad_event
      hook_events: [BeforeAnything]
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("git")
      message: Invalid dispatch.
      advice: Fix dispatch.
`,
			want: "invalid hook event",
		},
		{
			name: "bad lint scope",
			expression: `
    - id: custom.bad_lint_scope
      lint_scopes: [everything]
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("git")
      message: Invalid dispatch.
      advice: Fix dispatch.
`,
			want: "invalid lint scope",
		},
		{
			name: "bad mode",
			expression: `
    - id: custom.bad_mode
      mode: warnish
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("git")
      message: Invalid dispatch.
      advice: Fix dispatch.
`,
			want: "invalid mode",
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

func TestCompileAppendsRepoExpressionPolicies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.base_expression
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("base")
      message: Base expression.
      advice: Keep the base expression.
`)
	writeTestFile(t, repoConfigPath, `
policy:
  expressions:
    - id: custom.repo_expression
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("repo")
      message: Repo expression.
      advice: Keep the repo expression.
`)

	bundle, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	basePolicy := bundle.Policies["custom.base_expression"]
	repoPolicy := bundle.Policies["custom.repo_expression"]
	if basePolicy.Source.File != "config.yaml" {
		t.Fatalf("base expression source = %#v", basePolicy.Source)
	}
	if repoPolicy.Source.File != "repo_config.yaml" {
		t.Fatalf("repo expression source = %#v", repoPolicy.Source)
	}
}

func TestCompileRejectsExpressionPolicyShadowing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.shared_expression
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("base")
      message: Base expression.
      advice: Keep the base expression.
`)
	writeTestFile(t, repoConfigPath, `
policy:
  expressions:
    - id: custom.shared_expression
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("repo")
      message: Repo expression.
      advice: Keep the repo expression.
`)

	_, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with an existing policy") {
		t.Fatalf("compile error = %v, want duplicate expression rejection", err)
	}
}

func TestCompileAllowsExplicitExpressionOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.overrideable_expression
      allow_override: true
      allow_severity_weaken: true
      principle_ids: [one-path-for-critical-operations]
      severity: block
      when: command.contains("base")
      message: Base expression.
      advice: Keep the base expression.
`)
	writeTestFile(t, repoConfigPath, `
policy:
  expressions:
    - id: custom.overrideable_expression
      override: true
      override_reason: Repo policy narrows this custom expression.
      principle_ids: [one-path-for-critical-operations]
      severity: advise
      when: command.contains("repo")
      message: Repo expression.
      advice: Keep the repo expression.
`)

	bundle, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	policyDef := bundle.Policies["custom.overrideable_expression"]
	if policyDef.Source.File != "repo_config.yaml" ||
		policyDef.DefaultSeverity != "advise" ||
		policyDef.Evaluators[0].Options["override"] != true {
		t.Fatalf("override policy mismatch: %#v", policyDef)
	}
}

func TestCompileRejectsExpressionSeverityWeakening(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.strict_expression
      allow_override: true
      principle_ids: [one-path-for-critical-operations]
      severity: block
      when: command.contains("base")
      message: Base expression.
      advice: Keep the base expression.
`)
	writeTestFile(t, repoConfigPath, `
policy:
  expressions:
    - id: custom.strict_expression
      override: true
      override_reason: Repo policy attempts to weaken this expression.
      principle_ids: [one-path-for-critical-operations]
      severity: advise
      when: command.contains("repo")
      message: Repo expression.
      advice: Keep the repo expression.
`)

	_, _, err := Compile(CompileOptions{
		Primary:    primaryPath,
		Config:     configPath,
		RepoConfig: repoConfigPath,
	})
	if err == nil || !strings.Contains(err.Error(), "weakens severity") {
		t.Fatalf("compile error = %v, want severity weakening rejection", err)
	}
}

func TestCompileRejectsDisabledProtectedExpression(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")

	writeTestFile(t, primaryPath, testEthosYAML)
	writeTestFile(t, configPath, testConfigYAML+`
policy:
  expressions:
    - id: custom.disabled_expression
      enabled: false
      principle_ids: [one-path-for-critical-operations]
      when: command.contains("disabled")
      message: Disabled expression.
      advice: Keep the disabled expression.
`)

	_, _, err := Compile(CompileOptions{
		Primary: primaryPath,
		Config:  configPath,
	})
	if err == nil || !strings.Contains(err.Error(), "protected and cannot be disabled") {
		t.Fatalf("compile error = %v, want protected-disable rejection", err)
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
		"protected_paths",
	)
	if protectedPath[0] != "/opt/blocked" {
		t.Fatalf("protected path options mismatch: %#v", protectedPath)
	}

	protectedBranch := optionStrings(
		t,
		bundle.Policies["filesystem.protected_branch_write"].Evaluators[0],
		"protected_branches",
	)
	if protectedBranch[0] != "release" {
		t.Fatalf("protected branch options mismatch: %#v", protectedBranch)
	}

	requiredIgnores := optionStrings(
		t,
		bundle.Policies["repo.required_ignores"].Evaluators[0],
		"required_ignore_paths",
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

	forbiddenWhen := stringOptionFromEvaluator(
		t,
		bundle.Policies["shell.forbidden_strings"].Evaluators[0],
		"when",
	)
	if !strings.Contains(forbiddenWhen, "coding-ethos-hooks") ||
		!strings.Contains(forbiddenWhen, "referenced_files.exists") {
		t.Fatalf("forbidden strings CEL expression mismatch: %s", forbiddenWhen)
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

	largeFilePolicy := bundle.Policies["filesystem.large_files"]
	if largeFilePolicy.Source.Path != "principles[security-by-design].policy.expressions[0]" ||
		largeFilePolicy.Evaluators[0].Kind != "cel" {
		t.Fatalf("large-file policy should be principle-owned CEL: %#v", largeFilePolicy)
	}

	lineLimitPolicy := bundle.Policies["filesystem.line_limits"]
	if lineLimitPolicy.Source.Path != "principles[solid-is-law].policy.expressions[0]" ||
		lineLimitPolicy.Evaluators[0].Kind != "cel" {
		t.Fatalf("line-limit policy should be principle-owned CEL: %#v", lineLimitPolicy)
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
  malformed_command:
    enabled: false
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
		"repo.pii_scrubber",
		"repo.license_header",
		"shell.malformed_command",
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

func stringOptionFromEvaluator(t *testing.T, evaluator Evaluator, key string) string {
	t.Helper()

	value, ok := evaluator.Options[key].(string)
	if !ok {
		t.Fatalf("option %q is not string: %#v", key, evaluator.Options[key])
	}

	return value
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
) HookDispatchEntry {
	t.Helper()

	for _, entry := range entries {
		if entry.PolicyID == expected {
			return entry
		}
	}

	t.Fatalf("hook dispatch missing %q: %#v", expected, entries)
	return HookDispatchEntry{}
}

func assertPolicyNotDispatched(t *testing.T, policyIDs []string, expected string) {
	t.Helper()

	if slices.Contains(policyIDs, expected) {
		t.Fatalf("dispatch unexpectedly includes %q: %#v", expected, policyIDs)
	}
}

func assertHookPolicyNotDispatched(
	t *testing.T,
	entries []HookDispatchEntry,
	expected string,
) {
	t.Helper()

	for _, entry := range entries {
		if entry.PolicyID == expected {
			t.Fatalf("hook dispatch unexpectedly includes %q: %#v", expected, entries)
		}
	}
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
    policy:
      expressions:
        - id: filesystem.line_limits
          scope: file
          severity: block
          mode: block
          message: Large source files must not keep growing.
          advice: Split large files into focused modules before committing.
          when: "file_changes.exists(file, file.ext == '.py' && file.line_count > 1000)"
  - id: security-by-design
    order: 24
    title: Security by Design
    summary: Design for safe defaults.
    directive: Prevent secrets and unsafe defaults.
    policy:
      expressions:
        - id: filesystem.large_files
          scope: file
          severity: block
          mode: block
          message: Oversized newly added files are forbidden.
          advice: Remove oversized generated or binary content from the commit.
          when: "file_changes.exists(file, file.is_added && file.size_bytes > 512000)"
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
  - id: no-rationalized-shortcuts
    order: 21
    title: No Rationalized Shortcuts
    summary: Do not bypass safety checks.
    directive: Preserve work and use canonical safety paths.
    policy:
      expressions:
        - id: filesystem.protected_path
          scope: file
          severity: block
          mode: block
          tools: [Bash, Write, Edit, MultiEdit]
          message: Protected coding-ethos hook paths must not be modified.
          advice: Do not delete, rebuild, replace, chmod, or write managed hook binaries.
          when: "any_contains(repo.protected_paths, command_fact.lower) || paths.exists(path, is_protected_path(path.file, repo.protected_paths))"
        - id: shell.forbidden_strings
          scope: command
          severity: block
          mode: block
          tools: [Bash, Write, Edit, MultiEdit]
          lint_scopes: [files, staged]
          message: Commands must not inspect protected hook-system internals.
          advice: Use documented hook surfaces.
          when: "((event.tool == 'Bash' && any_contains(['.claude/settings.json', 'header must match', 'coding-ethos-hooks/coding-ethos-git-hook', 'coding-ethos-hooks/bin/coding-ethos-policy'], command_fact.lower)) || (list_contains(['Write', 'Edit', 'MultiEdit'], event.tool) && !paths.exists(path, any_glob_match(['**/.claude/**', '**/.codex/**', '**/.gemini/**'], path.file)) && any_contains(['header must match', 'coding-ethos-hooks/coding-ethos-git-hook'], content.lower)) || referenced_files.exists(file, file.is_regular && !file.in_agent_workspace && any_contains(['header must match', 'coding-ethos-hooks/coding-ethos-git-hook'], file.lower)))"
        - id: git.hook_bypass
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Hook bypass is forbidden.
          advice: Run the configured gate and fix the underlying failure.
          when: "git_command.is_git && git_command.subcommand == 'commit' && list_contains(git_command.flags, '--no-verify')"
        - id: git.protected_submodule_update
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Protected submodules cannot be initialized or checked out to a recorded SHA.
          advice: Use git submodule update --remote for upgrades.
          when: "git_command.is_git && git_command.subcommand == 'submodule' && git_command.args.size() > 0 && git_command.args[0] == 'update' && !list_contains(git_command.flags, '--remote')"
        - id: git.change_dir_flag
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: git -C changes repository context invisibly.
          advice: Run git commands from the intended repository root.
          when: "git_command.is_git && git_command.has_change_dir"
        - id: git.stash_blocked
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: git stash hides working state.
          advice: Keep changes visible.
          when: "git_command.is_git && git_command.subcommand == 'stash'"
        - id: git.destructive_worktree
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Destructive git worktree operations are forbidden.
          advice: Inspect worktree state before changing worktrees.
          when: "git_command.is_git && git_command.subcommand == 'worktree' && git_command.args.exists(arg, arg == 'prune')"
        - id: git.destructive_command
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Destructive git commands are forbidden.
          advice: Preserve work and resolve state explicitly.
          when: "git_command.is_git && (git_command.has_hard_reset || git_command.has_clean_force_delete || git_command.has_theirs_ours_checkout || git_command.has_restore_pathspec)"
        - id: git.merge_strategy_shortcut
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Blanket merge strategies are forbidden.
          advice: Resolve conflicts explicitly.
          when: "git_command.is_git && git_command.subcommand == 'merge' && git_command.has_merge_strategy_shortcut"
        - id: git.force_push_protected_branch
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Force push to protected branches is forbidden.
          advice: Use the normal review path.
          when: "git_command.is_git && git_command.subcommand == 'push' && git_command.has_force_push_protected"
        - id: git.checkout_protected_branch
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Switching to protected branches is forbidden.
          advice: Inspect history without switching.
          when: "git_command.is_git && (git_command.subcommand == 'checkout' || git_command.subcommand == 'switch') && git_command.has_checkout_protected_branch"
        - id: filesystem.protected_branch_write
          scope: file
          severity: block
          mode: block
          tools: [Bash, Write, Edit, MultiEdit]
          lint_scopes: [staged]
          message: Protected branch writes are forbidden.
          advice: Create or use a worktree before modifying files.
          when: "git.on_protected_branch && paths.exists(path, path.file != 'docs/plans/next.md')"
        - id: shell.inline_env
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Inline command environment variables are forbidden.
          advice: Route configuration through validated bootstrap files.
          when: "command_fact.has_inline_env || shell_commands.exists(command, command.has_inline_env)"
        - id: shell.path_override
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: PATH override in shell commands is forbidden.
          advice: Use managed toolchain paths.
          when: "shell_commands.exists(command, command.uses_path_override)"
        - id: shell.dangerous_command
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: Dangerous shell commands are forbidden.
          advice: Use reviewed commands.
          when: "shell_commands.exists(command, command.name == 'rm' && list_contains(command.argv, '-rf'))"
        - id: shell.background_git
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: git commit and git push must not run in the background or under timeout.
          advice: Run git commit or git push in the foreground.
          when: "shell_commands.exists(command, (command.is_git_mutation && command.background) || command.wraps_git_mutation)"
        - id: shell.github_admin
          scope: command
          severity: block
          mode: block
          tools: [Bash]
          lint_scopes: [staged]
          message: GitHub admin CLI operations are forbidden in agent hooks.
          advice: Use the reviewed administrative path instead of gh --admin.
          when: "shell_commands.exists(command, command.name == 'gh' && list_contains(command.argv, '--admin'))"
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
    policy:
      expressions:
        - id: repo.required_ignores
          scope: repo
          severity: block
          mode: block
          tools: [Bash]
          hook_events: []
          lint_scopes: [staged, smoke, full, cutover]
          message: Repository runtime output paths must be ignored.
          advice: Add coding-ethos runtime paths to .gitignore.
          when: "repo.required_ignores.exists(ignore, ignore.check_failed || !ignore.ignored)"
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
