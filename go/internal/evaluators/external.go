// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"bytes"
	stdlibcontext "context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

var errExternalCommandEmpty = apperror.StaticError(
	"external evaluator command is empty",
)

var errGeneratedConfigEthosRootRequired = apperror.StaticError(
	"evaluate generated config freshness: ethos_root option is required",
)

var errGeneratedGeminiPromptsEthosRootRequired = apperror.StaticError(
	"evaluate generated Gemini prompt freshness: ethos_root option is required",
)

var errGeneratedAgentSkillsEthosRootRequired = apperror.StaticError(
	"evaluate generated agent skill freshness: ethos_root option is required",
)

const defaultExternalCommandTimeout = 10 * time.Minute

func EvaluateExternalCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := stringSliceOption(context.EvaluatorOptions, "command", nil)
	if len(command) == 0 {
		return nil, errExternalCommandEmpty
	}

	commandContext, cancel := stdlibcontext.WithTimeout(
		stdlibcontext.Background(),
		defaultExternalCommandTimeout,
	)
	defer cancel()

	// #nosec G204 - compiled policy controls the command.
	cmd := exec.CommandContext(commandContext, command[0], command[1:]...)
	if context.Cwd != "" {
		cmd.Dir = context.Cwd
	}

	var stdout bytes.Buffer

	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil, nil
	}

	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())
	tool := stringOption(
		context.EvaluatorOptions,
		"parser",
		diagnostics.InferTool(command),
	)

	decision := policy.NewDecision("block", policyDef)
	decision.Diagnostics = diagnostics.Parse(tool, stdoutText, stderrText)

	decision.Evidence = map[string]any{
		"command":   append([]string(nil), command...),
		"exit_code": externalExitCode(err),
		"stderr":    stderrText,
		"stdout":    stdoutText,
		"tool":      tool,
	}
	if context.Cwd != "" {
		decision.Evidence["cwd"] = context.Cwd
	}

	return []policy.Decision{decision}, nil
}

func externalExitCode(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}

	return -1
}

func EvaluateGeneratedConfigFreshness(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	ethosRoot := stringOption(context.EvaluatorOptions, "ethos_root", "")
	if ethosRoot == "" {
		return nil, errGeneratedConfigEthosRootRequired
	}

	repoRoot := context.Cwd
	if repoRoot == "" {
		repoRoot = stringOption(context.EvaluatorOptions, "repo", ".")
	}

	repoConfig := stringOption(context.EvaluatorOptions, "repo_config", "")

	mismatched, err := toolconfigs.Check(ethosRoot, repoRoot, repoConfig)
	if err != nil {
		return nil, fmt.Errorf(
			"evaluate generated config freshness: check generated tool configs: %w",
			err,
		)
	}

	if len(mismatched) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = map[string]any{
		"ethos_root":       ethosRoot,
		"repo":             repoRoot,
		"repo_config":      repoConfig,
		"mismatched_paths": append([]string(nil), mismatched...),
		"tool":             "generated-config",
	}
	decision.Diagnostics = generatedConfigFreshnessDiagnostics(
		policyDef,
		decision,
		repoRoot,
		mismatched,
	)

	return []policy.Decision{decision}, nil
}

func EvaluateGeneratedGeminiPromptsFreshness(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	options, err := generatedGeminiPromptsOptions(context)
	if err != nil {
		return nil, err
	}

	mismatched, err := geminiprompts.Check(options)
	if err != nil {
		return nil, fmt.Errorf(
			"evaluate generated Gemini prompt freshness: check prompt pack: %w",
			err,
		)
	}

	if len(mismatched) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = generatedGeminiPromptsEvidence(options, mismatched)
	decision.Diagnostics = generatedGeminiPromptsFreshnessDiagnostics(
		policyDef,
		decision,
		options.RepoRoot,
		mismatched,
	)

	return []policy.Decision{decision}, nil
}

func EvaluateGeneratedAgentSkillsFreshness(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	options, err := generatedAgentSkillsOptions(context)
	if err != nil {
		return nil, err
	}

	mismatched, err := agentskills.Check(options)
	if err != nil {
		return nil, fmt.Errorf(
			"evaluate generated agent skill freshness: check skill surfaces: %w",
			err,
		)
	}

	if len(mismatched) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = generatedAgentSkillsEvidence(options, mismatched)
	decision.Diagnostics = generatedAgentSkillsFreshnessDiagnostics(
		policyDef,
		decision,
		options.RepoRoot,
		mismatched,
	)

	return []policy.Decision{decision}, nil
}

func EvaluatePytestGate(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	decisions, err := EvaluateExternalCommand(policyDef, context)
	if err != nil {
		return nil, fmt.Errorf("evaluate pytest gate: %w", err)
	}

	return decisions, nil
}

func generatedConfigFreshnessDiagnostics(
	policyDef policy.Policy,
	decision policy.Decision,
	repoRoot string,
	paths []string,
) []diagnostics.Diagnostic {
	if len(paths) == 0 {
		return nil
	}

	items := make([]diagnostics.Diagnostic, 0, len(paths))
	for _, path := range paths {
		items = append(items, diagnostics.Diagnostic{
			Tool:         "generated-config",
			File:         externalRepoRelativePath(repoRoot, path),
			Severity:     decision.Severity,
			Code:         "generated-config-drift",
			PolicyID:     decision.PolicyID,
			Message:      "Generated tool config is out of sync.",
			Advice:       policyDef.Suggestion,
			PrincipleIDs: append([]string(nil), decision.PrincipleIDs...),
			Metadata: map[string]any{
				"generated_config_path": path,
			},
		})
	}

	return diagnostics.Dedupe(items)
}

func generatedGeminiPromptsOptions(
	context Context,
) (geminiprompts.Options, error) {
	ethosRoot := stringOption(context.EvaluatorOptions, "ethos_root", "")
	if ethosRoot == "" {
		return geminiprompts.Options{}, errGeneratedGeminiPromptsEthosRootRequired
	}

	repoRoot := context.Cwd
	if repoRoot == "" {
		repoRoot = stringOption(context.EvaluatorOptions, "repo", ".")
	}

	return geminiprompts.Options{
		EthosRoot:  ethosRoot,
		RepoRoot:   repoRoot,
		Primary:    stringOption(context.EvaluatorOptions, "primary", ""),
		RepoEthos:  stringOption(context.EvaluatorOptions, "repo_ethos", ""),
		RepoConfig: stringOption(context.EvaluatorOptions, "repo_config", ""),
	}, nil
}

func generatedGeminiPromptsEvidence(
	options geminiprompts.Options,
	mismatched []string,
) map[string]any {
	return map[string]any{
		"ethos_root":       options.EthosRoot,
		"repo":             options.RepoRoot,
		"primary":          options.Primary,
		"repo_ethos":       options.RepoEthos,
		"repo_config":      options.RepoConfig,
		"mismatched_paths": append([]string(nil), mismatched...),
		"tool":             "generated-gemini-prompts",
	}
}

func generatedAgentSkillsOptions(
	context Context,
) (agentskills.Options, error) {
	ethosRoot := stringOption(context.EvaluatorOptions, "ethos_root", "")
	if ethosRoot == "" {
		return agentskills.Options{}, errGeneratedAgentSkillsEthosRootRequired
	}

	repoRoot := context.Cwd
	if repoRoot == "" {
		repoRoot = stringOption(context.EvaluatorOptions, "repo", ".")
	}

	return agentskills.Options{
		EthosRoot: ethosRoot,
		RepoRoot:  repoRoot,
		Primary:   stringOption(context.EvaluatorOptions, "primary", ""),
		RepoEthos: stringOption(context.EvaluatorOptions, "repo_ethos", ""),
	}, nil
}

func generatedAgentSkillsEvidence(
	options agentskills.Options,
	mismatched []string,
) map[string]any {
	return map[string]any{
		"ethos_root":       options.EthosRoot,
		"repo":             options.RepoRoot,
		"primary":          options.Primary,
		"repo_ethos":       options.RepoEthos,
		"mismatched_paths": append([]string(nil), mismatched...),
		"tool":             "generated-agent-skills",
	}
}

func generatedGeminiPromptsFreshnessDiagnostics(
	policyDef policy.Policy,
	decision policy.Decision,
	repoRoot string,
	paths []string,
) []diagnostics.Diagnostic {
	if len(paths) == 0 {
		return nil
	}

	items := make([]diagnostics.Diagnostic, 0, len(paths))
	for _, path := range paths {
		items = append(items, diagnostics.Diagnostic{
			Tool:         "generated-gemini-prompts",
			File:         externalRepoRelativePath(repoRoot, path),
			Severity:     decision.Severity,
			Code:         "generated-gemini-prompt-pack-drift",
			PolicyID:     decision.PolicyID,
			Message:      "Generated Gemini prompt pack is out of sync.",
			Advice:       policyDef.Suggestion,
			PrincipleIDs: append([]string(nil), decision.PrincipleIDs...),
			Metadata: map[string]any{
				"generated_gemini_prompt_pack_path": path,
			},
		})
	}

	return diagnostics.Dedupe(items)
}

func generatedAgentSkillsFreshnessDiagnostics(
	policyDef policy.Policy,
	decision policy.Decision,
	repoRoot string,
	paths []string,
) []diagnostics.Diagnostic {
	if len(paths) == 0 {
		return nil
	}

	items := make([]diagnostics.Diagnostic, 0, len(paths))
	for _, path := range paths {
		items = append(items, diagnostics.Diagnostic{
			Tool:         "generated-agent-skills",
			File:         externalRepoRelativePath(repoRoot, path),
			Severity:     decision.Severity,
			Code:         "generated-agent-skill-drift",
			PolicyID:     decision.PolicyID,
			Message:      "Generated agent skill surface is out of sync.",
			Advice:       policyDef.Suggestion,
			PrincipleIDs: append([]string(nil), decision.PrincipleIDs...),
			Metadata: map[string]any{
				"generated_agent_skill_path": path,
			},
		})
	}

	return diagnostics.Dedupe(items)
}

func externalRepoRelativePath(cwd, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path))
	}

	if cwd != "" {
		relative, err := filepath.Rel(cwd, path)
		if err == nil && relative != "." &&
			relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}

	return filepath.ToSlash(filepath.Clean(path))
}
