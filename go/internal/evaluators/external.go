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
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

var errExternalCommandEmpty = apperror.StaticError(
	"external evaluator command is empty",
)

var errGeneratedConfigEthosRootRequired = apperror.StaticError(
	"evaluate generated config freshness: ethos_root option is required",
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
