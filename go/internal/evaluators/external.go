// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"bytes"
	stdlibcontext "context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errExternalCommandEmpty = errors.New("external evaluator command is empty")

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
	decisions, err := EvaluateExternalCommand(policyDef, context)
	if err != nil {
		return nil, fmt.Errorf("evaluate generated config freshness: %w", err)
	}

	return decisions, nil
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
