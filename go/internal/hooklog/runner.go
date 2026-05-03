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
)

var errCommandRequired = errors.New("command is required")

type Options struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	GitPath    string
	Root       string
	BundleRoot string
	Command    []string
	Now        func() time.Time
}

func Run(options Options) error {
	if len(options.Command) == 0 {
		return errCommandRequired
	}
	if strings.TrimSpace(options.Root) == "" {
		return fmt.Errorf("root is required")
	}
	if strings.TrimSpace(options.BundleRoot) == "" {
		return fmt.Errorf("bundle root is required")
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

	for _, requiredIgnore := range []string{
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	} {
		if err := requireIgnored(options, requiredIgnore); err != nil {
			return err
		}
	}

	startedAt := options.Now().UTC()
	runID := fmt.Sprintf(
		"%s-%d-%d",
		startedAt.Format("20060102T150405Z"),
		os.Getppid(),
		os.Getpid(),
	)
	runDir := filepath.Join(options.Root, ".coding-ethos", "hook-runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create hook log directory: %w", err)
	}

	stdoutLog, err := os.Create(filepath.Join(runDir, "stdout.log"))
	if err != nil {
		return fmt.Errorf("create stdout log: %w", err)
	}
	defer stdoutLog.Close()

	stderrLog, err := os.Create(filepath.Join(runDir, "stderr.log"))
	if err != nil {
		return fmt.Errorf("create stderr log: %w", err)
	}
	defer stderrLog.Close()

	metadataPath := filepath.Join(runDir, "metadata.env")
	if err := writeStartedMetadata(metadataPath, hookRunMetadata{
		RunID:      runID,
		StartedAt:  startedAt,
		RepoRoot:   options.Root,
		BundleRoot: options.BundleRoot,
		Command:    options.Command,
	}); err != nil {
		return err
	}

	cmd := exec.Command(options.Command[0], options.Command[1:]...)
	cmd.Env = append(os.Environ(),
		"CODE_ETHOS_HOOK_LOGGING_ACTIVE=1",
		"CODE_ETHOS_HOOK_RUN_DIR="+runDir,
	)
	cmd.Stdin = options.Stdin
	cmd.Stdout = io.MultiWriter(options.Stdout, stdoutLog)
	cmd.Stderr = io.MultiWriter(options.Stderr, stderrLog)

	err = cmd.Run()
	status := exitCode(err)
	if metadataErr := appendFinishedMetadata(metadataPath, options.Now().UTC(), status); metadataErr != nil {
		return metadataErr
	}
	if err != nil {
		return commandError{err: err, code: status}
	}

	return nil
}

func requireIgnored(options Options, path string) error {
	cmd := exec.Command(options.GitPath, "-C", options.Root, "check-ignore", "--quiet", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"FATAL: %s is not ignored; add .coding-ethos/ to the repo .gitignore before hook logs are written",
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
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close metadata log: %w", closeErr)
		}
	}()

	if _, err := fmt.Fprintf(file, "run_id=%s\n", shellQuote(metadata.RunID)); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "started_at_utc=%s\n", shellQuote(metadata.StartedAt.Format("20060102T150405Z"))); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "repo_root=%s\n", shellQuote(metadata.RepoRoot)); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "bundle_root=%s\n", shellQuote(metadata.BundleRoot)); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "command=%s\n", quoteCommand(metadata.Command)); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

func appendFinishedMetadata(path string, finishedAt time.Time, status int) (err error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open metadata log: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close metadata log: %w", closeErr)
		}
	}()

	if _, err := fmt.Fprintf(file, "finished_at_utc=%s\n", shellQuote(finishedAt.Format("20060102T150405Z"))); err != nil {
		return fmt.Errorf("write finished metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "exit_code=%s\n", shellQuote(strconv.Itoa(status))); err != nil {
		return fmt.Errorf("write finished metadata: %w", err)
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
