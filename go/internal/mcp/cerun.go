// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
)

const (
	defaultCerunTimeoutSeconds = 120
	maxCerunCapturedBytes      = 64 * 1024
	cerunTimeoutExitCode       = 124
)

var errCerunRuntimeUnavailable = apperror.StaticError(
	"repo-local cerun runtime is not configured",
)

func (server Server) checkCerun(args json.RawMessage) (any, error) {
	input, err := parseCerunInput(args)
	if err != nil {
		return nil, err
	}

	rewrite := cerunRewrite(input)

	result, err := server.inspectCerun(input, rewrite)
	if err != nil {
		return nil, err
	}

	response := policyCheckResponse("cerun", result)
	response["command_sha256"] = sha256Text(input.Command)
	response["recommended_command"] = cerunDisplayCommand(
		server.resolvedCerunPathOrEmpty(),
		input,
		true,
		rewrite,
	)
	response["agent_remediation"] = agentmsg.FromDecisions(result.Decisions, "Bash")
	response["rewrite"] = rewrite

	return response, nil
}

func (server Server) runCerun(args json.RawMessage) (any, error) {
	input, err := parseCerunInput(args)
	if err != nil {
		return nil, err
	}

	cerunPath, err := server.resolveCerunPath()
	if err != nil {
		return nil, err
	}

	rewrite := cerunRewrite(input)
	argv := cerunArgs(input, false, rewrite)

	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultCerunTimeoutSeconds
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(timeoutSeconds)*time.Second,
	)
	defer cancel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	command := safeexec.CommandContext(ctx, cerunPath, argv...)
	command.Dir = server.cerunCwd(input)
	command.Stdout = cappedBufferWriter{
		buffer: &stdout,
		limit:  maxCerunCapturedBytes + 1,
	}
	command.Stderr = cappedBufferWriter{
		buffer: &stderr,
		limit:  maxCerunCapturedBytes + 1,
	}
	command.Env = os.Environ()

	runErr := command.Run()
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)

	exitCode := processstatus.ExitCode(runErr, 0)
	if timedOut {
		exitCode = cerunTimeoutExitCode
	} else if runErr != nil && exitCode == 0 {
		exitCode = 127

		if stderr.Len() == 0 {
			stderr.WriteString(runErr.Error())
		}
	}

	stdoutText, stdoutTruncated := truncateCerunOutput(stdout.String())
	stderrText, stderrTruncated := truncateCerunOutput(stderr.String())

	return map[string]any{
		"kind":                 "cerun_run",
		"cerun_path":           cerunPath,
		"command_sha256":       sha256Text(input.Command),
		"cwd":                  command.Dir,
		"display_command":      cerunDisplayCommand(cerunPath, input, false, rewrite),
		"exit_code":            exitCode,
		"timed_out":            timedOut,
		"timeout_seconds":      timeoutSeconds,
		"stdout":               stdoutText,
		"stderr":               stderrText,
		"stdout_truncated":     stdoutTruncated,
		"stderr_truncated":     stderrTruncated,
		"recommended_followup": cerunFollowup(input, exitCode),
	}, nil
}

func parseCerunInput(args json.RawMessage) (cerunInput, error) {
	var input cerunInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return cerunInput{}, fmt.Errorf("parse cerun arguments: %w", err)
	}

	if strings.TrimSpace(input.Command) == "" {
		return cerunInput{}, apperror.StaticError("command is required")
	}

	return input, nil
}

type cappedBufferWriter struct {
	buffer *bytes.Buffer
	limit  int
}

func (writer cappedBufferWriter) Write(data []byte) (int, error) {
	remaining := writer.limit - writer.buffer.Len()
	if remaining > 0 {
		_, _ = writer.buffer.Write(data[:min(len(data), remaining)])
	}

	return len(data), nil
}

func (server Server) inspectCerun(
	input cerunInput,
	rewrite bool,
) (hooks.Result, error) {
	result, err := hooks.Run(server.bundle, hooks.Options{Event: hooks.Event{
		ProviderHint:  providerOrDefault(input.Provider),
		HookEventName: "PreToolUse",
		Source:        providerOrDefault(input.Provider),
		ToolName:      "Bash",
		Cwd:           server.cerunCwd(input),
		ToolInput: map[string]any{
			"command":             input.Command,
			"agent_shell_rewrite": rewrite,
			"strategic_intent":    strings.TrimSpace(input.Intent),
		},
	}})
	if err != nil {
		return hooks.Result{}, fmt.Errorf("inspect cerun command: %w", err)
	}

	return result, nil
}

func (server Server) resolvedCerunPathOrEmpty() string {
	path, err := server.resolveCerunPath()
	if err != nil {
		return ""
	}

	return path
}

func cerunRewrite(input cerunInput) bool {
	if input.Rewrite == nil {
		return true
	}

	return *input.Rewrite
}

func (server Server) cerunCwd(input cerunInput) string {
	return firstNonEmpty(
		input.Cwd,
		server.runtime.InvocationCwd,
		server.runtime.ConsumerRoot,
		server.runtime.EthosRoot,
		".",
	)
}

func (server Server) resolveCerunPath() (string, error) {
	for _, candidate := range server.cerunPathCandidates() {
		if executableExists(candidate) {
			return filepath.ToSlash(filepath.Clean(candidate)), nil
		}
	}

	return "", errCerunRuntimeUnavailable
}

func (server Server) cerunPathCandidates() []string {
	candidates := []string{}
	appendCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}

		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err == nil {
				path = abs
			}
		}

		path = filepath.Clean(path)
		if !containsString(candidates, path) {
			candidates = append(candidates, path)
		}
	}

	appendCandidate(server.runtime.CerunPath)

	if strings.TrimSpace(server.runtime.EthosRoot) != "" {
		appendCandidate(filepath.Join(server.runtime.EthosRoot, "bin", "cerun"))
	}

	if strings.TrimSpace(server.runtime.ConsumerRoot) != "" {
		appendCandidate(filepath.Join(server.runtime.ConsumerRoot, "bin", "cerun"))
		appendCandidate(
			filepath.Join(
				server.runtime.ConsumerRoot,
				"coding-ethos",
				"bin",
				"cerun",
			),
		)
	}

	return candidates
}

func executableExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return false
	}

	return info.Mode().Perm()&0o111 != 0
}

func containsString(values []string, candidate string) bool {
	return slices.Contains(values, candidate)
}

func cerunArgs(input cerunInput, check, rewrite bool) []string {
	args := []string{}
	if check {
		args = append(args, "--check")
	}

	if rewrite {
		args = append(args, "--rewrite")
	} else {
		args = append(args, "--no-rewrite")
	}

	if strings.TrimSpace(input.Intent) != "" {
		args = append(args, "--intent", strings.TrimSpace(input.Intent))
	}

	return append(args, "--", input.Command)
}

func cerunDisplayCommand(
	cerunPath string,
	input cerunInput,
	check bool,
	rewrite bool,
) string {
	command := strings.TrimSpace(cerunPath)
	if command == "" {
		command = "cerun"
	}

	return shellquote.Command(
		append([]string{command}, cerunArgs(input, check, rewrite)...)...)
}

func truncateCerunOutput(value string) (string, bool) {
	if len(value) <= maxCerunCapturedBytes {
		return value, false
	}

	limit := maxCerunCapturedBytes
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}

	return value[:limit], true
}

func cerunFollowup(input cerunInput, exitCode int) map[string]any {
	if exitCode == 0 {
		return nil
	}

	return map[string]any{
		"tool": "remediation_explain",
		"arguments": map[string]any{
			"tool":          "cerun",
			"command":       input.Command,
			"failed_action": "cerun_run",
			"message": "cerun execution failed; inspect stderr and policy " +
				"output before retrying.",
		},
	}
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}
