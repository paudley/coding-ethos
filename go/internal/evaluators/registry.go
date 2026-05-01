// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"errors"
	"fmt"
)

var errEvaluatorNotRegistered = errors.New("evaluator is not registered")

type Registry struct {
	evaluators map[string]Evaluator
}

func NewRegistry() Registry {
	return Registry{evaluators: map[string]Evaluator{}}
}

func DefaultRegistry() Registry {
	registry := NewRegistry()
	registerExpressionEvaluators(registry)
	registerGitEvaluators(registry)
	registerFilesystemEvaluators(registry)
	registerShellEvaluators(registry)
	registerSyntaxEvaluators(registry)
	registerPythonEvaluators(registry)
	registerExternalEvaluators(registry)

	return registry
}

func registerExpressionEvaluators(registry Registry) {
	registry.Register("cel.expression", EvaluatorFunc(EvaluateCELExpression))
}

func registerGitEvaluators(registry Registry) {
	registry.Register("git.hook_bypass", EvaluatorFunc(EvaluateGitHookBypass))
	registry.Register(
		"git.destructive_command",
		EvaluatorFunc(EvaluateGitDestructiveCommand),
	)
	registry.Register(
		"git.merge_strategy_shortcut",
		EvaluatorFunc(EvaluateGitMergeStrategyShortcut),
	)
	registry.Register(
		"git.force_push_protected_branch",
		EvaluatorFunc(EvaluateGitForcePushProtectedBranch),
	)
	registry.Register(
		"git.checkout_protected_branch",
		EvaluatorFunc(EvaluateGitCheckoutProtectedBranch),
	)
	registry.Register(
		"git.destructive_worktree",
		EvaluatorFunc(EvaluateGitDestructiveWorktree),
	)
	registry.Register("git.change_dir_flag", EvaluatorFunc(EvaluateGitChangeDirFlag))
	registry.Register("git.stash_blocked", EvaluatorFunc(EvaluateGitStashBlocked))
	registry.Register("git.commitlint", EvaluatorFunc(EvaluateGitCommitLint))
	registry.Register(
		"git.commit_attribution",
		EvaluatorFunc(EvaluateGitCommitAttribution),
	)
	registry.Register(
		"git.staged_admin_files",
		EvaluatorFunc(EvaluateGitStagedAdminFiles),
	)
	registry.Register(
		"git.commit_head_advanced",
		EvaluatorFunc(EvaluateGitCommitHeadAdvanced),
	)
}

func registerFilesystemEvaluators(registry Registry) {
	registry.Register(
		"filesystem.protected_path",
		EvaluatorFunc(EvaluateProtectedPath),
	)
	registry.Register(
		"filesystem.protected_branch_write",
		EvaluatorFunc(EvaluateProtectedBranchWrite),
	)
	registry.Register(
		"filesystem.required_ignores",
		EvaluatorFunc(EvaluateRequiredIgnores),
	)
	registry.Register("repo.required_ignores", EvaluatorFunc(EvaluateRequiredIgnores))
	registry.Register("repo.pii_scrubber", EvaluatorFunc(EvaluatePIIScrubber))
	registry.Register("repo.license_header", EvaluatorFunc(EvaluateLicenseHeader))
}

func registerShellEvaluators(registry Registry) {
	registry.Register(
		"shell.dangerous_command",
		EvaluatorFunc(EvaluateShellDangerousCommand),
	)
	registry.Register("shell.background_git", EvaluatorFunc(EvaluateShellBackgroundGit))
	registry.Register("shell.github_admin", EvaluatorFunc(EvaluateShellGitHubAdmin))
	registry.Register(
		"shell.forbidden_strings",
		EvaluatorFunc(EvaluateShellForbiddenStrings),
	)
	registry.Register(
		"shell.best_practices",
		EvaluatorFunc(EvaluateShellBestPractices),
	)
}

func registerSyntaxEvaluators(registry Registry) {
	registry.Register("syntax.file_syntax", EvaluatorFunc(EvaluateFileSyntax))
	registry.Register("syntax.merge_conflict", EvaluatorFunc(EvaluateFileMergeConflict))
	registry.Register("security.private_key", EvaluatorFunc(EvaluateFilePrivateKey))
	registry.Register("filesystem.shebangs", EvaluatorFunc(EvaluateFileShebang))
	registry.Register("filesystem.large_files", EvaluatorFunc(EvaluateFileLargeFile))
	registry.Register("filesystem.line_limits", EvaluatorFunc(EvaluateFileLineLimit))
}

func registerPythonEvaluators(registry Registry) {
	registry.Register(
		"python.conditional_imports",
		EvaluatorFunc(EvaluatePythonConditionalImports),
	)
	registry.Register(
		"python.optional_returns",
		EvaluatorFunc(EvaluatePythonOptionalReturns),
	)
	registry.Register(
		"python.catch_and_silence",
		EvaluatorFunc(EvaluatePythonCatchAndSilence),
	)
	registry.Register(
		"python.structured_logging",
		EvaluatorFunc(EvaluatePythonStructuredLogging),
	)
	registry.Register(
		"python.direct_imports",
		EvaluatorFunc(EvaluatePythonDirectImports),
	)
	registry.Register("python.bare_except", EvaluatorFunc(EvaluatePythonBareExcept))
	registry.Register(
		"python.unexplained_type_ignore",
		EvaluatorFunc(EvaluatePythonUnexplainedTypeIgnore),
	)
	registry.Register(
		"python.pyproject_ignores",
		EvaluatorFunc(EvaluatePythonPyprojectIgnores),
	)
}

func registerExternalEvaluators(registry Registry) {
	registry.Register(
		"generated_config.freshness",
		EvaluatorFunc(EvaluateGeneratedConfigFreshness),
	)
	registry.Register("pytest.gate", EvaluatorFunc(EvaluatePytestGate))
}

func (registry Registry) Register(name string, evaluator Evaluator) {
	registry.evaluators[name] = evaluator
}

func (registry Registry) Lookup(name string) (Evaluator, bool) {
	evaluator, ok := registry.evaluators[name]

	return evaluator, ok
}

func (registry Registry) Require(name string) (Evaluator, error) {
	evaluator, ok := registry.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", errEvaluatorNotRegistered, name)
	}

	return evaluator, nil
}
