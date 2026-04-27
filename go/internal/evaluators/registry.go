// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
)

type Registry struct {
	evaluators map[string]Evaluator
}

func NewRegistry() Registry {
	return Registry{evaluators: map[string]Evaluator{}}
}

func DefaultRegistry() Registry {
	registry := NewRegistry()
	registry.Register("git.hook_bypass", EvaluatorFunc(EvaluateGitHookBypass))
	registry.Register("git.destructive_command", EvaluatorFunc(EvaluateGitDestructiveCommand))
	registry.Register("git.merge_strategy_shortcut", EvaluatorFunc(EvaluateGitMergeStrategyShortcut))
	registry.Register("git.force_push_protected_branch", EvaluatorFunc(EvaluateGitForcePushProtectedBranch))
	registry.Register("git.checkout_protected_branch", EvaluatorFunc(EvaluateGitCheckoutProtectedBranch))
	registry.Register("git.destructive_worktree", EvaluatorFunc(EvaluateGitDestructiveWorktree))
	registry.Register("git.change_dir_flag", EvaluatorFunc(EvaluateGitChangeDirFlag))
	registry.Register("git.stash_blocked", EvaluatorFunc(EvaluateGitStashBlocked))
	registry.Register("git.staged_admin_files", EvaluatorFunc(EvaluateGitStagedAdminFiles))
	registry.Register("git.commit_head_advanced", EvaluatorFunc(EvaluateGitCommitHeadAdvanced))
	registry.Register("shell.dangerous_command", EvaluatorFunc(EvaluateShellDangerousCommand))
	registry.Register("shell.background_git", EvaluatorFunc(EvaluateShellBackgroundGit))
	registry.Register("python.conditional_imports", EvaluatorFunc(EvaluatePythonConditionalImports))
	registry.Register("python.optional_returns", EvaluatorFunc(EvaluatePythonOptionalReturns))
	registry.Register("python.catch_and_silence", EvaluatorFunc(EvaluatePythonCatchAndSilence))
	registry.Register("python.structured_logging", EvaluatorFunc(EvaluatePythonStructuredLogging))
	registry.Register("python.direct_imports", EvaluatorFunc(EvaluatePythonDirectImports))
	return registry
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
		return nil, fmt.Errorf("no evaluator registered for %q", name)
	}
	return evaluator, nil
}
