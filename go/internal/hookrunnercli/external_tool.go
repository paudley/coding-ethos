// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	execabs "golang.org/x/sys/execabs"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
)

var (
	errExternalToolCommandEmpty = apperror.StaticError("external tool command is empty")
	errExternalToolTimedOut     = apperror.StaticError("external tool timed out")
)

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

	cmd := execabs.CommandContext(ctx, request.Command[0], request.Command[1:]...)
	cmd.Dir = request.Dir
	cmd.Env = externalToolEnv(request.Env)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := debuglog.ProcessEnter(request.Command, request.Dir)
	err := cmd.Run()
	debuglog.ProcessExit(
		startedAt,
		request.Command,
		request.Dir,
		commandExitCode(err),
		err,
	)
	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())

	result := externalToolResult{
		Stdout:     stdoutText,
		Stderr:     stderrText,
		Combined:   externalToolCombinedOutput(stdoutText, stderrText),
		ExitCode:   0,
		DurationMS: float64(time.Since(start).Milliseconds()),
		TimedOut:   errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	if result.TimedOut {
		result.ExitCode = 1
		result.RunnerFailure = fmt.Errorf(
			"%s: %w after %d seconds",
			request.Name,
			errExternalToolTimedOut,
			timeout,
		)

		return result
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

func externalToolCombinedOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func externalToolEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, item := range os.Environ() {
		if externalToolEnvBlocked(item) {
			continue
		}

		name, value, found := strings.Cut(item, "=")
		if found && name == "PATH" {
			env = append(env, name+"="+externalToolPathWithoutGitShim(value))

			continue
		}

		env = append(env, item)
	}

	env = append(env, "GIT_OPTIONAL_LOCKS=0")

	return append(env, extra...)
}

func externalToolPathWithoutGitShim(pathValue string) string {
	kept := []string{}

	for _, entry := range filepath.SplitList(pathValue) {
		if entry == "" || externalToolPathEntryHasCodingEthosGitShim(entry) {
			continue
		}

		kept = append(kept, entry)
	}

	return strings.Join(kept, string(os.PathListSeparator))
}

func externalToolPathEntryHasCodingEthosGitShim(entry string) bool {
	payload, err := os.ReadFile(filepath.Join(entry, "git"))
	if err != nil {
		return false
	}

	text := string(payload)

	return strings.Contains(text, "coding-ethos-run") &&
		strings.Contains(text, "policy-git")
}

func externalToolEnvBlocked(item string) bool {
	name, _, found := strings.Cut(item, "=")
	if !found {
		return false
	}

	if strings.HasPrefix(name, "CODE_ETHOS_") ||
		strings.HasPrefix(name, "CODING_ETHOS_") {
		return true
	}

	if name == consumerRootEnv ||
		name == precommitRootEnv ||
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

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 1
}
