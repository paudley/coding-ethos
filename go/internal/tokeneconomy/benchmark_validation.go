// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:govet,lll,mnd,noinlineerr,wsl_v5 // Receipt fields and ordered validation aid audit review.
package tokeneconomy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const validationReceiptName = "validation-receipt.json"

type benchmarkValidatorResult struct {
	Arguments    []string `json:"arguments"`
	Executable   string   `json:"executable"`
	StdoutSHA256 string   `json:"stdout_sha256"`
	StderrSHA256 string   `json:"stderr_sha256"`
	Error        string   `json:"error,omitempty"`
	ExitCode     int      `json:"exit_code"`
	TimedOut     bool     `json:"timed_out"`
	Passed       bool     `json:"passed"`
}

type benchmarkValidationReceipt struct {
	TaskID          string                     `json:"task_id"`
	BaselineCommit  string                     `json:"baseline_commit"`
	CompletedAtUTC  string                     `json:"completed_at_utc"`
	ChangedPaths    []string                   `json:"changed_paths"`
	OutOfScopePaths []string                   `json:"out_of_scope_paths"`
	Validators      []benchmarkValidatorResult `json:"validators"`
	AgentSucceeded  bool                       `json:"agent_succeeded"`
	Accepted        bool                       `json:"accepted"`
}

func validateBenchmarkWorkspace(
	ctx context.Context,
	task BenchmarkTask,
	workspace preparedWorkspace,
	runRoot string,
	agentSucceeded bool,
) benchmarkValidationReceipt {
	receipt := benchmarkValidationReceipt{
		TaskID:          task.TaskID,
		BaselineCommit:  workspace.BaselineCommit,
		CompletedAtUTC:  time.Now().UTC().Format(time.RFC3339Nano),
		ChangedPaths:    []string{},
		OutOfScopePaths: []string{},
		Validators:      []benchmarkValidatorResult{},
		AgentSucceeded:  agentSucceeded,
	}

	changed, err := benchmarkChangedPaths(ctx, workspace)
	if err != nil {
		receipt.Validators = append(receipt.Validators, benchmarkValidatorResult{
			Error:    "inspect changed paths: " + err.Error(),
			ExitCode: -1,
		})

		return receipt
	}
	receipt.ChangedPaths = changed
	for _, path := range changed {
		if !benchmarkPathAllowed(path, task.AllowedPaths) {
			receipt.OutOfScopePaths = append(receipt.OutOfScopePaths, path)
		}
	}

	validationTimeout, err := time.ParseDuration(task.ValidationTimeout)
	if err != nil {
		receipt.Validators = append(receipt.Validators, benchmarkValidatorResult{
			Error:    "parse validation timeout: " + err.Error(),
			ExitCode: -1,
		})

		return receipt
	}

	validationContext, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	allPassed := true
	for index, validator := range task.Validators {
		result := runBenchmarkValidator(
			validationContext,
			workspace.Path,
			runRoot,
			index,
			validator,
		)
		receipt.Validators = append(receipt.Validators, result)
		allPassed = allPassed && result.Passed
	}

	receipt.Accepted = agentSucceeded && allPassed && len(receipt.OutOfScopePaths) == 0

	return receipt
}

func benchmarkChangedPaths(
	ctx context.Context,
	workspace preparedWorkspace,
) ([]string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("resolve git: %w", err)
	}

	tracked, err := benchmarkGitBytes(
		ctx,
		git,
		workspace.Path,
		"diff",
		"--name-only",
		"-z",
		workspace.BaselineCommit,
		"--",
	)
	if err != nil {
		return nil, err
	}
	untracked, err := benchmarkGitBytes(
		ctx,
		git,
		workspace.Path,
		"ls-files",
		"--others",
		"-z",
	)
	if err != nil {
		return nil, err
	}

	unique := map[string]struct{}{}
	for _, path := range append(splitNullPaths(tracked), splitNullPaths(untracked)...) {
		cleaned := filepath.ToSlash(filepath.Clean(path))
		if cleaned != "." {
			unique[cleaned] = struct{}{}
		}
	}

	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	return paths, nil
}

func benchmarkGitBytes(
	ctx context.Context,
	git string,
	root string,
	arguments ...string,
) ([]byte, error) {
	command := safeexec.CommandContext(
		ctx,
		git,
		append([]string{"-C", root}, arguments...)...,
	)
	command.Env = benchmarkGitEnvironment()
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("query benchmark Git paths: %w", err)
	}

	return output, nil
}

func splitNullPaths(payload []byte) []string {
	parts := strings.Split(string(payload), "\x00")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

func benchmarkPathAllowed(path string, allowed []string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, candidate := range allowed {
		candidate = filepath.ToSlash(filepath.Clean(strings.TrimSpace(candidate)))
		if path == candidate || strings.HasPrefix(path, candidate+"/") {
			return true
		}
	}

	return false
}

func runBenchmarkValidator(
	ctx context.Context,
	workspace string,
	runRoot string,
	index int,
	validator []string,
) benchmarkValidatorResult {
	result := benchmarkValidatorResult{
		ExitCode: -1,
	}
	if len(validator) == 0 {
		result.Error = "validator command is empty"

		return result
	}
	result.Arguments = slices.Clone(validator[1:])

	executable, err := resolveBenchmarkExecutable(validator[0])
	if err != nil {
		result.Error = err.Error()

		return result
	}
	result.Executable = executable

	stdoutPath := filepath.Join(runRoot, fmt.Sprintf("validator-%02d.stdout.log", index+1))
	stderrPath := filepath.Join(runRoot, fmt.Sprintf("validator-%02d.stderr.log", index+1))
	stdout, stderr, err := createPrivateLogPair(stdoutPath, stderrPath)
	if err != nil {
		result.Error = err.Error()

		return result
	}

	command := safeexec.CommandContext(ctx, executable, validator[1:]...)
	command.Dir = workspace
	command.Env = benchmarkGitEnvironment()
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	closeErr := errors.Join(stdout.Close(), stderr.Close())

	result.ExitCode = benchmarkProcessExitCode(runErr)
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	stdoutDigest, stdoutHashErr := ledgerFileSHA256(stdoutPath)
	stderrDigest, stderrHashErr := ledgerFileSHA256(stderrPath)
	result.StdoutSHA256 = stdoutDigest
	result.StderrSHA256 = stderrDigest
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if closeErr != nil {
		result.Error = strings.TrimSpace(
			result.Error + "; close validator logs: " + closeErr.Error(),
		)
	}
	if hashErr := errors.Join(stdoutHashErr, stderrHashErr); hashErr != nil {
		result.Error = strings.TrimSpace(
			result.Error + "; hash validator logs: " + hashErr.Error(),
		)
	}
	result.Passed = runErr == nil && closeErr == nil && stdoutHashErr == nil &&
		stderrHashErr == nil && !result.TimedOut

	return result
}

func resolveBenchmarkExecutable(name string) (string, error) {
	executable, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve validator %q: %w", name, err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve validator symlink %q: %w", name, err)
	}

	return executable, nil
}

func createPrivateLogPair(stdoutPath, stderrPath string) (*os.File, *os.File, error) {
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("create validator stdout log: %w", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = stdout.Close()

		return nil, nil, fmt.Errorf("create validator stderr log: %w", err)
	}

	return stdout, stderr, nil
}

func writeBenchmarkValidationReceipt(
	runRoot string,
	receipt benchmarkValidationReceipt,
) (string, error) {
	payload, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode benchmark validation receipt: %w", err)
	}
	payload = append(payload, '\n')

	path := filepath.Join(runRoot, validationReceiptName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create benchmark validation receipt: %w", err)
	}
	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return "", fmt.Errorf("write benchmark validation receipt: %w", err)
	}

	digest := sha256.Sum256(payload)

	return hex.EncodeToString(digest[:]), nil
}
