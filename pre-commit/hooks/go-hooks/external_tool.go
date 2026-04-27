// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:gosec // Runs validated hook commands.
package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var errExternalToolCommandEmpty = errors.New("external tool command is empty")

type externalToolRequest struct {
	Name           string
	Dir            string
	Command        []string
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
