// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/internal/toolaliases"
)

func hookProbes() []hookProbe {
	probes := make([]hookProbe, 0, hookProbeCapacity)
	probes = append(probes, claudeHookProbes()...)
	probes = append(probes, codexHookProbes()...)
	probes = append(probes, geminiHookProbes()...)
	probes = append(probes, kimiHookProbes()...)

	return probes
}

// HookProbeSummaries returns provider/payload metadata for doctor probes.
func HookProbeSummaries() []HookProbeSummary {
	probes := hookProbes()

	summaries := make([]HookProbeSummary, 0, len(probes))
	for _, probe := range probes {
		summaries = append(summaries, HookProbeSummary{
			Provider: probe.provider,
			Payload:  probe.payload,
		})
	}

	return summaries
}

const hookProbeCapacity = 13

const (
	hookTamperProbeCommand = "rm /repo/.git/coding-ethos-hooks/coding-ethos-git-hook" +
		" && go build -o /repo/.git/coding-ethos-hooks/coding-ethos-git-hook ."
	pythonSubprocessGitProbeCommand = "python3 -c " +
		`"import subprocess; subprocess.run(['/usr/bin/git','status'])"`
)

func claudeHookProbes() []hookProbe {
	return []hookProbe{
		{
			provider: string(ProviderClaude),
			event:    eventPreToolUse,
			tool:     toolaliases.CanonicalShell,
			payload: `{
				"provider": "claude",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": "pwd && git status --short 2>&1"}
			}`,
			validate: validateClaudeRewriteProbe,
		},
		{
			provider: string(ProviderClaude),
			event:    eventPreToolUse,
			tool:     toolaliases.CanonicalShell,
			payload:  claudeBashProbePayload(hookTamperProbeCommand),
			validate: validateClaudeBlockProbe,
		},
	}
}

func codexHookProbes() []hookProbe {
	return []hookProbe{
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "git status --short"}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "functions.exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "functions.exec_command",
				"input": {"cmd": "git switch main"}
			}`,
			validate: validateCodexGitPolicyBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "/usr/bin/git status --short"}
			}`,
			validate: validateCodexWrapperRefusalProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "bash -c 'git status --short'"}
			}`,
			validate: validateCodexWrapperRefusalProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "exec_command",
			payload:  codexExecProbePayload(pythonSubprocessGitProbeCommand),
			validate: validateCodexWrapperRefusalProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    eventPreToolUse,
			tool:     "exec_command",
			payload:  codexExecProbePayload(hookTamperProbeCommand),
			validate: validateCodexPolicyBlockProbe,
		},
	}
}

func geminiHookProbes() []hookProbe {
	return []hookProbe{
		{
			provider: string(ProviderGemini),
			event:    eventBeforeTool,
			tool:     "run_shell_command",
			payload: `{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "run_shell_command",
				"toolInput": {"command": "git status --short"}
			}`,
			validate: validateGeminiRewriteProbe,
		},
		{
			provider: string(ProviderGemini),
			event:    eventBeforeTool,
			tool:     "run_shell_command",
			payload:  geminiShellProbePayload(hookTamperProbeCommand),
			validate: validateGeminiDenyProbe,
		},
		{
			provider: string(ProviderGemini),
			event:    eventBeforeTool,
			tool:     "write_file",
			payload: `{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "write_file",
				"toolInput": {
					"file_path": "/repo/.git/coding-ethos-hooks/coding-ethos-git-hook",
					"content": "binary"
				}
			}`,
			validate: validateGeminiDenyProbe,
		},
	}
}

func kimiHookProbes() []hookProbe {
	return []hookProbe{
		{
			provider: string(ProviderKimi),
			event:    eventPreToolUse,
			tool:     toolaliases.CanonicalShell,
			payload: fmt.Sprintf(`{
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": %q}
			}`, hookTamperProbeCommand),
			validate: validateKimiDenyProbe,
		},
		{
			provider: string(ProviderKimi),
			event:    eventStop,
			payload: `{
				"hook_event_name": "Stop"
			}`,
			validate: validateKimiStopContinuationProbe,
		},
	}
}

func claudeBashProbePayload(command string) string {
	return fmt.Sprintf(`{
				"provider": "claude",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": %q}
			}`, command)
}

func codexExecProbePayload(command string) string {
	return fmt.Sprintf(`{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": %q}
			}`, command)
}

func geminiShellProbePayload(command string) string {
	return fmt.Sprintf(`{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "run_shell_command",
				"toolInput": {"command": %q}
			}`, command)
}

func runHookProbe(
	root string,
	hookCommand string,
	probe hookProbe,
	probeTimeout time.Duration,
) (hookProbeResult, error) {
	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	args, err := hookProbeArgs(root, hookCommand)
	if err != nil {
		return hookProbeResult{}, err
	}

	if probe.provider == string(ProviderKimi) {
		args = append(args, "--provider", string(ProviderKimi))
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	command := safeexec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = root
	command.Env = withoutHookProbeProcessState(os.Environ())
	command.Stdin = strings.NewReader(probe.payload)
	command.Stdout = &stdout
	command.Stderr = &stderr

	runErr := command.Run()
	exitCode := 0

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return hookProbeResult{}, fmt.Errorf("run hook probe: %w", runErr)
		}
	}

	if ctx.Err() != nil {
		return hookProbeResult{}, fmt.Errorf("run hook probe: %w", ctx.Err())
	}

	result := hookProbeResult{
		exitCode: exitCode,
		stdout:   stdout.String(),
		stderr:   stderr.String(),
	}

	if result.stdout != "" {
		payload, decodeErr := decodeHookProbePayload(result.stdout)
		if decodeErr != nil {
			return result, decodeErr
		}

		result.payload = payload
	}

	return result, nil
}

func withoutHookProbeProcessState(environ []string) []string {
	const hookLoggingActiveEnv = "CODE_ETHOS_HOOK_LOGGING_ACTIVE"

	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if key == execguard.EnvStack || key == hookLoggingActiveEnv {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

func hookProbeArgs(root, hookCommand string) ([]string, error) {
	command, err := staticSingleCommand(hookCommand, errUnsupportedHookCommand)
	if err != nil {
		return nil, err
	}

	argv := append([]string(nil), command.Argv...)

	if len(argv) < minimumHookArgs {
		return nil, fmt.Errorf("%w: %s", errUnsupportedHookCommand, hookCommand)
	}

	executableIndex := 0

	if filepath.Base(argv[0]) == "env" {
		var found bool

		executableIndex, found = externalHookExecutableIndex(argv)
		if !found {
			return nil, fmt.Errorf("%w: %s", errUnsupportedHookCommand, hookCommand)
		}
	}

	if executableIndex+1 >= len(argv) {
		return nil, fmt.Errorf("%w: %s", errUnsupportedHookCommand, hookCommand)
	}

	runnerPath := argv[executableIndex]
	subcommand := argv[executableIndex+1]

	isCodingEthosHook := filepath.Base(runnerPath) == "coding-ethos-run" &&
		subcommand == "agent-hook"

	isExternalSupervisorHook := filepath.IsAbs(runnerPath) && subcommand == "hook"
	if !isCodingEthosHook && !isExternalSupervisorHook {
		return nil, fmt.Errorf("%w: %s", errUnsupportedHookCommand, hookCommand)
	}

	if !filepath.IsAbs(runnerPath) {
		if executableIndex != 0 {
			return nil, fmt.Errorf("%w: %s", errUnsupportedHookCommand, hookCommand)
		}

		runnerPath = filepath.Join(root, runnerPath)
	}

	argv[executableIndex] = runnerPath

	return argv, nil
}

func staticSingleCommand(
	commandText string,
	unsupported error,
) (shellparse.Command, error) {
	commands, err := shellparse.Commands(commandText)
	if err != nil {
		return shellparse.Command{}, fmt.Errorf(
			"%w: parse %q: %w",
			unsupported,
			commandText,
			err,
		)
	}

	if len(commands) != 1 {
		return shellparse.Command{}, fmt.Errorf("%w: %s", unsupported, commandText)
	}

	command := commands[0]
	if unsafeStaticCommand(command) {
		return shellparse.Command{}, fmt.Errorf("%w: %s", unsupported, commandText)
	}

	return command, nil
}

func unsafeStaticCommand(command shellparse.Command) bool {
	return len(command.Argv) == 0 ||
		len(command.Assignments) != 0 ||
		len(command.Redirects) != 0 ||
		command.Background ||
		command.HasCommandSubstitution ||
		command.HasDynamicExpansion ||
		command.HasHeredoc ||
		command.HasProcessSubstitution ||
		command.HasSubshell ||
		command.IsFunctionDeclaration
}

func externalHookExecutableIndex(argv []string) (int, bool) {
	index := 1
	for index < len(argv) && externalEnvironmentAssignment(argv[index]) {
		index++
	}

	return index, index > 1 && index < len(argv)
}

func externalEnvironmentAssignment(value string) bool {
	name, _, found := strings.Cut(value, "=")

	return found && externalEnvironmentName.MatchString(name)
}

func decodeHookProbePayload(output string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(output))
	payload := map[string]any{}

	err := decoder.Decode(&payload)
	if err != nil {
		return nil, fmt.Errorf("decode hook probe JSON: %w", err)
	}

	return payload, nil
}

func validateClaudeRewriteProbe(result hookProbeResult) error {
	err := validateRewriteProbe(result, "Claude")
	if err != nil {
		return err
	}

	command, found := nestedString(
		result.payload,
		"hookSpecificOutput",
		"updatedInput",
		"command",
	)
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("missing Claude updatedInput command in %s"),
			"missing Claude updatedInput command in %s",
			result.stdout,
		)
	}

	if !strings.Contains(command, "2>&1") {
		return apperror.Wrapf(
			apperror.StaticError("claude rewrite lost redirection: %s"),
			"claude rewrite lost redirection: %s",
			command,
		)
	}

	return nil
}

// ValidateClaudeRewritePayload validates Claude doctor rewrite output.
func ValidateClaudeRewritePayload(stdout string, payload map[string]any) error {
	return validateClaudeRewriteProbe(hookProbeResult{
		exitCode: 0,
		stdout:   stdout,
		payload:  payload,
	})
}

func validateCodexRewriteProbe(result hookProbeResult) error {
	hookOutput, found := result.payload["hookSpecificOutput"].(map[string]any)
	if found {
		if _, hasUpdatedInput := hookOutput["updatedInput"]; hasUpdatedInput {
			return apperror.Wrapf(
				apperror.StaticError(
					"codex rewrite emitted unsupported updatedInput in %s",
				),
				"codex rewrite emitted unsupported updatedInput in %s",
				result.stdout,
			)
		}
	}

	return validateCodexBlockProbe(result)
}

// ValidateCodexRewritePayload validates Codex doctor rewrite output.
func ValidateCodexRewritePayload(stdout string, payload map[string]any) error {
	return validateCodexRewriteProbe(hookProbeResult{
		exitCode: 0,
		stdout:   stdout,
		payload:  payload,
	})
}

func validateGeminiRewriteProbe(result hookProbeResult) error {
	return validateRewriteProbe(result, "Gemini")
}

// ValidateGeminiRewritePayload validates Gemini doctor rewrite output.
func ValidateGeminiRewritePayload(stdout string, payload map[string]any) error {
	return validateGeminiRewriteProbe(hookProbeResult{
		exitCode: 0,
		stdout:   stdout,
		payload:  payload,
	})
}

func validateRewriteProbe(result hookProbeResult, provider string) error {
	if result.exitCode != 0 {
		return apperror.Wrapf(
			apperror.StaticError("%s git rewrite probe should allow, got exit %d"),
			"%s git rewrite probe should allow, got exit %d",
			provider,
			result.exitCode,
		)
	}

	command, found := nestedString(
		result.payload,
		"hookSpecificOutput",
		"updatedInput",
		"command",
	)
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("missing %s updatedInput command in %s"),
			"missing %s updatedInput command in %s",
			provider,
			result.stdout,
		)
	}

	if !strings.Contains(command, "agent-shell --") ||
		!strings.Contains(command, "git status --short") {
		return apperror.Wrapf(
			apperror.StaticError("%s rewrite lost git wrapper or redirection: %s"),
			"%s rewrite lost git wrapper or redirection: %s",
			provider,
			command,
		)
	}

	return nil
}

// validateCodexBlockProbe checks the managed rewrite-remediation block
// shape: the wrapper policy must block and carry a concrete cerun resubmit
// command.
func validateCodexBlockProbe(result hookProbeResult) error {
	return validateCodexBlockReason(result, "git.wrapper_required", "cerun --")
}

// validateCodexGitPolicyBlockProbe checks that a git policy blocked the
// command. The winning policy id is configuration-dependent: semantic git
// policies such as git.checkout_protected_branch legitimately preempt the
// wrapper remediation for protected-branch targets.
func validateCodexGitPolicyBlockProbe(result hookProbeResult) error {
	return validateCodexBlockReason(result, "git.")
}

// validateCodexWrapperRefusalProbe checks the circumvention-refusal block
// shape: the wrapper policy refuses the command without offering a cerun
// resubmit template.
func validateCodexWrapperRefusalProbe(result hookProbeResult) error {
	return validateCodexBlockReason(result, "git.wrapper_required")
}

// validateCodexPolicyBlockProbe checks that enforcement hard-blocked the
// command with an actionable reason, regardless of which policy fired.
func validateCodexPolicyBlockProbe(result hookProbeResult) error {
	return validateCodexBlockReason(result)
}

func validateCodexBlockReason(
	result hookProbeResult,
	reasonMarkers ...string,
) error {
	if result.exitCode == 0 {
		return apperror.StaticError("codex raw git probe should block")
	}

	actual, found := result.payload["decision"].(string)
	if !found || actual != "block" {
		return apperror.Wrapf(
			apperror.StaticError("decision = %q, want block; stdout=%s"),
			"decision = %q, want block; stdout=%s",
			actual,
			result.stdout,
		)
	}

	reason, found := result.payload["reason"].(string)
	if !found || strings.TrimSpace(reason) == "" {
		return apperror.Wrapf(
			apperror.StaticError("missing reason in %s"),
			"missing reason in %s",
			result.stdout,
		)
	}

	for _, marker := range reasonMarkers {
		if !strings.Contains(reason, marker) {
			return apperror.Wrapf(
				apperror.StaticError("codex block reason lost marker %q: %s"),
				"codex block reason lost marker %q: %s",
				marker,
				reason,
			)
		}
	}

	permissionReason, found := nestedString(
		result.payload,
		"hookSpecificOutput",
		"permissionDecisionReason",
	)
	if !found || strings.TrimSpace(permissionReason) == "" {
		return apperror.Wrapf(
			apperror.StaticError("missing permissionDecisionReason in %s"),
			"missing permissionDecisionReason in %s",
			result.stdout,
		)
	}

	return nil
}

func validateClaudeBlockProbe(result hookProbeResult) error {
	return validateDecisionProbe(result, "block")
}

func validateGeminiDenyProbe(result hookProbeResult) error {
	if result.exitCode == 0 {
		return apperror.StaticError("gemini probe should deny")
	}

	return validateDecisionProbe(result, "deny")
}

func validateKimiDenyProbe(result hookProbeResult) error {
	if result.exitCode != kimiBlockExitCode {
		return apperror.Wrapf(
			apperror.StaticError("Kimi deny probe exit = %d, want 2"),
			"Kimi deny probe exit = %d, want 2",
			result.exitCode,
		)
	}

	if strings.TrimSpace(result.stderr) == "" {
		return apperror.StaticError("Kimi deny probe must emit a stderr reason")
	}

	return validateKimiStructuredDeny(result)
}

func validateKimiStopContinuationProbe(result hookProbeResult) error {
	if result.exitCode != 0 {
		return apperror.Wrapf(
			apperror.StaticError("Kimi Stop continuation exit = %d, want 0"),
			"Kimi Stop continuation exit = %d, want 0",
			result.exitCode,
		)
	}

	return validateKimiStructuredDeny(result)
}

func validateKimiStructuredDeny(result hookProbeResult) error {
	decision, found := nestedString(
		result.payload,
		"hookSpecificOutput",
		"permissionDecision",
	)
	if !found || decision != "deny" {
		return apperror.Wrapf(
			apperror.StaticError(
				"Kimi hook permissionDecision = %q, want deny; stdout=%s",
			),
			"Kimi hook permissionDecision = %q, want deny; stdout=%s",
			decision,
			result.stdout,
		)
	}

	reason, found := nestedString(
		result.payload,
		"hookSpecificOutput",
		"permissionDecisionReason",
	)
	if !found || strings.TrimSpace(reason) == "" {
		return apperror.Wrapf(
			apperror.StaticError("missing Kimi permissionDecisionReason in %s"),
			"missing Kimi permissionDecisionReason in %s",
			result.stdout,
		)
	}

	return nil
}

func validateDecisionProbe(result hookProbeResult, decision string) error {
	actual, found := result.payload["decision"].(string)
	if !found || actual != decision {
		return apperror.Wrapf(
			apperror.StaticError("decision = %q, want %q; stdout=%s"),
			"decision = %q, want %q; stdout=%s",
			actual,
			decision,
			result.stdout,
		)
	}

	message, found := result.payload["systemMessage"].(string)
	if !found || strings.TrimSpace(message) == "" {
		return apperror.Wrapf(
			apperror.StaticError("missing systemMessage in %s"),
			"missing systemMessage in %s",
			result.stdout,
		)
	}

	return nil
}

func nestedString(payload map[string]any, keys ...string) (string, bool) {
	current := any(payload)
	for _, key := range keys {
		object, found := current.(map[string]any)
		if !found {
			return "", false
		}

		current, found = object[key]
		if !found {
			return "", false
		}
	}

	value, found := current.(string)

	return value, found
}
