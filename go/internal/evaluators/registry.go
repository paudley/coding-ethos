// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

var errEvaluatorNotRegistered = apperror.StaticError("evaluator is not registered")

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
	registry.Register("repo.pii_scrubber", EvaluatorFunc(EvaluatePIIScrubber))
	registry.Register("repo.license_header", EvaluatorFunc(EvaluateLicenseHeader))
}

func registerShellEvaluators(registry Registry) {
	registry.Register(
		"shell.malformed_command",
		EvaluatorFunc(EvaluateShellMalformedCommand),
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
}

func registerPythonEvaluators(registry Registry) {
	registry.Register(
		"python.conditional_imports",
		EvaluatorFunc(EvaluatePythonConditionalImports),
	)
	registry.Register(
		"python.functional_idioms",
		EvaluatorFunc(EvaluatePythonFunctionalIdioms),
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
	registry.Register(
		"python.uv_exclude_newer",
		EvaluatorFunc(EvaluatePythonUVExcludeNewer),
	)
}

func registerExternalEvaluators(registry Registry) {
	registry.Register(
		"generated_config.freshness",
		EvaluatorFunc(EvaluateGeneratedConfigFreshness),
	)
	registry.Register(
		"generated_gemini_prompts.freshness",
		EvaluatorFunc(EvaluateGeneratedGeminiPromptsFreshness),
	)
	registry.Register(
		"generated_agent_skills.freshness",
		EvaluatorFunc(EvaluateGeneratedAgentSkillsFreshness),
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
