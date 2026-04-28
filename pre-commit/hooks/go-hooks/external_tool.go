// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:gosec // Runs validated hook commands.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

var errExternalToolCommandEmpty = errors.New("external tool command is empty")

type externalToolRequest struct {
	Name           string
	Dir            string
	Command        []string
	Env            []string
	TimeoutSeconds int
}

type externalToolResult struct {
	RunnerFailure error
	Stdout        string
	Stderr        string
	Combined      string
	ExitCode      int
	DurationMS    float64
	TimedOut      bool
}

func runExternalTool(request externalToolRequest) externalToolResult {
	start := time.Now()

	if len(request.Command) == 0 {
		return externalToolResult{
			ExitCode: 1,
			RunnerFailure: fmt.Errorf(
				"%s: %w",
				request.Name,
				errExternalToolCommandEmpty,
			),
		}
	}

	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = loadHookSettings().ToolTimeoutSeconds
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	cmd := exec.CommandContext(ctx, request.Command[0], request.Command[1:]...)
	cmd.Dir = request.Dir
	cmd.Env = externalToolEnv(request.Env)

	output, err := cmd.CombinedOutput()

	result := externalToolResult{
		Combined:   strings.TrimSpace(string(output)),
		ExitCode:   0,
		DurationMS: float64(time.Since(start).Milliseconds()),
		TimedOut:   errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	if err == nil {
		return result
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()

		return result
	}

	result.ExitCode = 1
	result.RunnerFailure = err

	return result
}

func externalToolEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, item := range os.Environ() {
		if externalToolEnvBlocked(item) {
			continue
		}

		env = append(env, item)
	}

	return append(env, extra...)
}

func externalToolEnvBlocked(item string) bool {
	name, _, found := strings.Cut(item, "=")
	if !found {
		return false
	}

	if name == consumerRootEnv ||
		name == hookGroupChildEnv ||
		name == hookGroupResultPathEnv {
		return true
	}

	return slices.Contains(gitHookLocalEnvNames(), name) ||
		strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
		strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

func gitHookLocalEnvNames() []string {
	return []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE",
	}
}
