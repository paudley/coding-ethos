// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooklog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const hookLogDirMode = 0o755

const hookLogIgnoreRequiredMessage = "FATAL: %s is not ignored; add " +
	".coding-ethos/ to the repo .gitignore before hook logs are written"

var errCommandRequired = apperror.StaticError("command is required")

type Options struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Now        func() time.Time
	GitPath    string
	Root       string
	BundleRoot string
	Command    []string
}

func Run(options Options) error {
	options, err := normalizedOptions(options)
	if err != nil {
		return err
	}

	err = requireHookLogIgnores(options)
	if err != nil {
		return err
	}

	startedAt := options.Now().UTC()
	runID := hookRunID(startedAt)

	runDir, err := createHookRunDir(options.Root, runID)
	if err != nil {
		return err
	}

	logs, err := createHookRunLogs(runDir)
	if err != nil {
		return err
	}
	defer logs.close()

	metadataPath := filepath.Join(runDir, "metadata.env")

	err = writeStartedMetadata(metadataPath, hookRunMetadata{
		RunID:      runID,
		StartedAt:  startedAt,
		RepoRoot:   options.Root,
		BundleRoot: options.BundleRoot,
		Command:    options.Command,
	})
	if err != nil {
		return err
	}

	cmd := safeexec.Command(options.Command[0], options.Command[1:]...)

	cmd.Env = append(os.Environ(),
		"CODE_ETHOS_HOOK_LOGGING_ACTIVE=1",
		"CODE_ETHOS_HOOK_RUN_DIR="+runDir,
	)
	cmd.Stdin = options.Stdin
	cmd.Stdout = io.MultiWriter(options.Stdout, logs.stdout)
	cmd.Stderr = io.MultiWriter(options.Stderr, logs.stderr)

	err = cmd.Run()

	status := exitCode(err)

	metadataErr := appendFinishedMetadata(metadataPath, options.Now().UTC(), status)
	if metadataErr != nil {
		return metadataErr
	}

	if err != nil {
		return commandError{err: err, code: status}
	}

	return nil
}

func normalizedOptions(options Options) (Options, error) {
	if len(options.Command) == 0 {
		return Options{}, errCommandRequired
	}

	if strings.TrimSpace(options.Root) == "" {
		return Options{}, apperror.StaticError("root is required")
	}

	if strings.TrimSpace(options.BundleRoot) == "" {
		return Options{}, apperror.StaticError("bundle root is required")
	}

	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}

	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}

	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}

	if options.GitPath == "" {
		options.GitPath = "/usr/bin/git"
	}

	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}

	return options, nil
}

func requireHookLogIgnores(options Options) error {
	for _, requiredIgnore := range []string{
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	} {
		err := requireIgnored(options, requiredIgnore)
		if err != nil {
			return err
		}
	}

	return nil
}

func hookRunID(startedAt time.Time) string {
	return fmt.Sprintf(
		"%s-%d-%d",
		startedAt.Format("20060102T150405Z"),
		os.Getppid(),
		os.Getpid(),
	)
}

func createHookRunDir(root, runID string) (string, error) {
	runDir := filepath.Join(root, ".coding-ethos", "hook-runs", runID)

	err := os.MkdirAll(runDir, hookLogDirMode)
	if err != nil {
		return "", fmt.Errorf("create hook log directory: %w", err)
	}

	return runDir, nil
}

type hookRunLogs struct {
	stdout *os.File
	stderr *os.File
}

func createHookRunLogs(runDir string) (hookRunLogs, error) {
	stdoutLog, err := os.Create(filepath.Join(runDir, "stdout.log"))
	if err != nil {
		return hookRunLogs{}, fmt.Errorf("create stdout log: %w", err)
	}

	stderrLog, err := os.Create(filepath.Join(runDir, "stderr.log"))
	if err != nil {
		_ = stdoutLog.Close()

		return hookRunLogs{}, fmt.Errorf("create stderr log: %w", err)
	}

	return hookRunLogs{stdout: stdoutLog, stderr: stderrLog}, nil
}

func (logs hookRunLogs) close() {
	_ = logs.stdout.Close()
	_ = logs.stderr.Close()
}

func requireIgnored(options Options, path string) error {
	cmd := safeexec.Command(
		options.GitPath,
		"-C",
		options.Root,
		"check-ignore",
		"--quiet",
		path,
	)

	err := cmd.Run()
	if err != nil {
		return apperror.Wrapf(
			apperror.StaticError(
				hookLogIgnoreRequiredMessage,
			),
			hookLogIgnoreRequiredMessage,
			path,
		)
	}

	return nil
}

type hookRunMetadata struct {
	StartedAt  time.Time
	RunID      string
	RepoRoot   string
	BundleRoot string
	Command    []string
}

func writeStartedMetadata(path string, metadata hookRunMetadata) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata log: %w", err)
	}

	defer func() {
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close metadata log: %w", closeErr)
		}
	}()

	_, inlineErrA := fmt.Fprintf(file, "run_id=%s\n", shellQuote(metadata.RunID))
	if inlineErrA != nil {
		return fmt.Errorf("write metadata: %w", inlineErrA)
	}

	_, inlineErrB := fmt.Fprintf(
		file,
		"started_at_utc=%s\n",
		shellQuote(metadata.StartedAt.Format("20060102T150405Z")),
	)
	if inlineErrB != nil {
		return fmt.Errorf("write metadata: %w", inlineErrB)
	}

	_, inlineErrC := fmt.Fprintf(
		file,
		"repo_root=%s\n",
		shellQuote(metadata.RepoRoot),
	)
	if inlineErrC != nil {
		return fmt.Errorf("write metadata: %w", inlineErrC)
	}

	_, inlineErrD := fmt.Fprintf(
		file,
		"bundle_root=%s\n",
		shellQuote(metadata.BundleRoot),
	)
	if inlineErrD != nil {
		return fmt.Errorf("write metadata: %w", inlineErrD)
	}

	_, inlineErrE := fmt.Fprintf(
		file,
		"command=%s\n",
		quoteCommand(metadata.Command),
	)
	if inlineErrE != nil {
		return fmt.Errorf("write metadata: %w", inlineErrE)
	}

	return nil
}

func appendFinishedMetadata(path string, finishedAt time.Time, status int) (err error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open metadata log: %w", err)
	}

	defer func() {
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close metadata log: %w", closeErr)
		}
	}()

	_, inlineErrF := fmt.Fprintf(
		file,
		"finished_at_utc=%s\n",
		shellQuote(finishedAt.Format("20060102T150405Z")),
	)
	if inlineErrF != nil {
		return fmt.Errorf("write finished metadata: %w", inlineErrF)
	}

	_, inlineErrG := fmt.Fprintf(
		file,
		"exit_code=%s\n",
		shellQuote(strconv.Itoa(status)),
	)
	if inlineErrG != nil {
		return fmt.Errorf("write finished metadata: %w", inlineErrG)
	}

	return nil
}

func quoteCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}

	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

type commandError struct {
	err  error
	code int
}

func (err commandError) Error() string {
	return err.err.Error()
}

func (err commandError) ExitCode() int {
	return err.code
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 1
}
