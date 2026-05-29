// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooklog

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/repoignore"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	hookLogDirMode    = 0o755
	loggedStreamCount = 2
)

const (
	hookLogRunDirPath     = ".coding-ethos/hook-runs/"
	hookLogMemoryTestPath = ".coding-ethos/memories/MEMORY.md"
)

var (
	errCommandRequired               = apperror.StaticError("command is required")
	errCodingEthosCommandUnsupported = apperror.StaticError(
		"hook-log must not execute coding-ethos commands; " +
			"call the Go implementation directly",
	)
	errHookLogRuntimeOutputNotIgnored = apperror.StaticError(
		"hook runtime output ignore required",
	)
	errHookLogMemoryIgnored = apperror.StaticError(
		"repo memory path must remain trackable",
	)

	// runLoggedAction replaces process-global os.Stdout/os.Stderr while an
	// in-process command runs, so concurrent captures must serialize.
	//nolint:gochecknoglobals // process-global streams require one shared lock.
	loggedActionStreamMu sync.Mutex
)

type Options struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Now        func() time.Time
	Action     func() int
	GitPath    string
	Root       string
	BundleRoot string
	Command    []string
	Debug      bool
}

type loggedActionStreams struct {
	stdoutReader *os.File
	stdoutWriter *os.File
	stderrReader *os.File
	stderrWriter *os.File
}

type hookLogEnvSnapshot struct {
	LoggingActive string
	RunDir        string
	Debug         string
	HadLogging    bool
	HadRunDir     bool
	HadDebug      bool
}

func Run(options Options) error {
	_, err := runWithStatus(options)

	return err
}

func RunInProcess(options Options, action func() int) (int, error) {
	options.Action = action

	return runWithStatus(options)
}

func runWithStatus(options Options) (int, error) {
	options, err := normalizedOptions(options)
	if err != nil {
		return 1, err
	}

	err = requireHookLogIgnores(options)
	if err != nil {
		return 1, err
	}

	startedAt := options.Now().UTC()
	runID := hookRunID(startedAt)

	runDir, err := createHookRunDir(options.Root, runID)
	if err != nil {
		return 1, err
	}

	logs, err := createHookRunLogs(runDir)
	if err != nil {
		return 1, err
	}
	defer logs.close()
	defer debuglog.Reset()

	debugWriter := io.MultiWriter(options.Stderr, logs.stderr)

	debugEnabled, err := configureRunDebugLog(options, runDir, debugWriter)
	if err != nil {
		return 1, err
	}

	metadataPath := filepath.Join(runDir, "metadata.env")

	err = writeStartedMetadata(metadataPath, hookRunMetadata{
		RunID:      runID,
		StartedAt:  startedAt,
		RepoRoot:   options.Root,
		BundleRoot: options.BundleRoot,
		Command:    options.Command,
		Debug:      debugEnabled,
	})
	if err != nil {
		return 1, err
	}

	debuglog.Debug(
		"hook.run.enter",
		zap.String("run_id", runID),
		zap.String("repo_root", options.Root),
		zap.String("bundle_root", options.BundleRoot),
		zap.Strings("command", options.Command),
	)

	status, err := runLoggedPayload(options, logs, runDir)

	finishedAt := options.Now().UTC()
	debuglog.Debug(
		"hook.run.exit",
		zap.String("run_id", runID),
		zap.Int("exit_code", status),
		zap.Duration("elapsed", finishedAt.Sub(startedAt)),
		zap.Error(err),
	)

	syncErr := debuglog.Sync()
	if syncErr != nil {
		_, _ = fmt.Fprintf(options.Stderr, "WARN: sync debug log: %v\n", syncErr)
	}

	metadataErr := appendFinishedMetadata(metadataPath, finishedAt, status)
	if metadataErr != nil {
		return 1, metadataErr
	}

	maintenanceErr := finishHookMaintenance(options, runDir)

	return completedHookStatus(status, err, maintenanceErr)
}

func configureRunDebugLog(
	options Options,
	runDir string,
	debugWriter io.Writer,
) (bool, error) {
	debugEnabled := options.Debug || debuglog.EnabledFromEnv()

	err := debuglog.Configure(debugEnabled, runDir, debugWriter)
	if err != nil {
		return false, fmt.Errorf("configure debug logging: %w", err)
	}

	return debugEnabled, nil
}

func completedHookStatus(status int, runErr, maintenanceErr error) (int, error) {
	if runErr != nil {
		return status, commandError{err: runErr, code: status}
	}

	if maintenanceErr != nil {
		return 1, maintenanceErr
	}

	return status, nil
}

func finishHookMaintenance(options Options, runDir string) error {
	err := refreshCodeIntelAfterRun(options, runDir)
	if err != nil {
		return err
	}

	err = autoPruneHookRuns(options.Root)
	if err != nil {
		_, _ = fmt.Fprintf(options.Stderr, "WARN: auto-prune hook runs: %v\n", err)
		debuglog.Debug("hook.auto_prune.warn", zap.Error(err))
	}

	return nil
}

func refreshCodeIntelAfterRun(options Options, runDir string) error {
	if shouldForceCodeIntelRefresh(options.Command) {
		_, err := codeintel.RefreshRepository(
			context.Background(),
			options.Root,
			[]string{"."},
		)
		if err != nil {
			return fmt.Errorf("refresh code-intel after hook run: %w", err)
		}

		return nil
	}

	tracePath := filepath.Join(runDir, "event.json")

	_, err := os.Stat(tracePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("stat hook trace for code-intel ingest: %w", err)
	}

	err = codeintel.IngestHookTraceFile(context.Background(), options.Root, tracePath)
	if err != nil {
		return fmt.Errorf("ingest hook trace into code-intel: %w", err)
	}

	return nil
}

func autoPruneHookRuns(root string) error {
	err := outputsurface.AutoPruneSurface(context.Background(), root, "hook_runs", false)
	if err != nil {
		return fmt.Errorf("auto-prune hook runs: %w", err)
	}

	return nil
}

func shouldForceCodeIntelRefresh(command []string) bool {
	return commandContains(command, "parent-install") ||
		commandContains(command, "parent-check") ||
		commandContains(command, "parent-lint") ||
		commandContains(command, "policy-lint") ||
		commandContainsSequence(command, "git-hook", "pre-commit") ||
		commandContainsSequence(command, "git-hook", "pre-push") ||
		commandContainsSequence(command, "make", "pre-commit") ||
		commandContainsSequence(command, "make", "pre-commit-all") ||
		commandContainsSequence(command, "make", "check") ||
		commandContainsSequence(command, "make", "lint")
}

func commandContains(command []string, value string) bool {
	for _, part := range command {
		if filepath.Base(part) == value || part == value {
			return true
		}
	}

	return false
}

func commandContainsSequence(command []string, first, second string) bool {
	for index := 0; index+1 < len(command); index++ {
		if filepath.Base(command[index]) == first && command[index+1] == second {
			return true
		}
	}

	return false
}

func runLoggedPayload(
	options Options,
	logs hookRunLogs,
	runDir string,
) (int, error) {
	if options.Action != nil {
		return runLoggedAction(options, logs, runDir)
	}

	cmd := safeexec.Command(options.Command[0], options.Command[1:]...)

	cmd.Env = append(os.Environ(),
		"CODE_ETHOS_HOOK_LOGGING_ACTIVE=1",
		"CODE_ETHOS_HOOK_RUN_DIR="+runDir,
	)
	if options.Debug {
		cmd.Env = append(cmd.Env, debuglog.EnvName+"=1")
	}

	cmd.Stdin = options.Stdin
	cmd.Stdout = io.MultiWriter(options.Stdout, logs.stdout)
	cmd.Stderr = io.MultiWriter(options.Stderr, logs.stderr)

	logMakeProcess("make.process.enter", options.Command, runDir, options.Root, -1, 0)

	startedAt := debuglog.ProcessEnter(
		options.Command,
		options.Root,
		zap.String("run_dir", runDir),
	)
	err := cmd.Run()
	debuglog.ProcessExit(
		startedAt,
		options.Command,
		options.Root,
		exitCode(err),
		err,
		zap.String("run_dir", runDir),
	)
	logMakeProcess(
		"make.process.exit",
		options.Command,
		runDir,
		options.Root,
		exitCode(err),
		0,
	)

	return exitCode(err), err
}

func runLoggedAction(options Options, logs hookRunLogs, runDir string) (int, error) {
	loggedActionStreamMu.Lock()
	defer loggedActionStreamMu.Unlock()

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	originalEnv := captureHookLogEnv()

	streams, waitGroup, err := installLoggedActionStreams(
		options,
		logs,
	)
	if err != nil {
		return 1, err
	}

	restored := false

	defer func() {
		if restored {
			return
		}

		_, restoreErr := restoreLoggedStreams(
			originalStdout,
			originalStderr,
			streams.stdoutWriter,
			streams.stderrWriter,
			waitGroup,
			1,
			nil,
		)
		if restoreErr != nil {
			_, _ = fmt.Fprintf(
				originalStderr,
				"WARN: restore hook log streams: %v\n",
				restoreErr,
			)
		}
	}()

	err = setLoggedActionEnv(runDir)
	if err != nil {
		restored = true

		return restoreLoggedStreams(
			originalStdout,
			originalStderr,
			streams.stdoutWriter,
			streams.stderrWriter,
			waitGroup,
			1,
			err,
		)
	}

	status := runActionAndRestoreHookLogEnv(
		options.Action,
		options.Command,
		runDir,
		options.Root,
		originalEnv,
		options.Debug,
	)

	restored = true

	return restoreLoggedStreams(
		originalStdout,
		originalStderr,
		streams.stdoutWriter,
		streams.stderrWriter,
		waitGroup,
		status,
		nil,
	)
}

func captureHookLogEnv() hookLogEnvSnapshot {
	originalLoggingActive, hadLoggingActive := os.LookupEnv(
		"CODE_ETHOS_HOOK_LOGGING_ACTIVE",
	)
	originalRunDir, hadRunDir := os.LookupEnv("CODE_ETHOS_HOOK_RUN_DIR")
	originalDebug, hadDebug := os.LookupEnv(debuglog.EnvName)

	return hookLogEnvSnapshot{
		LoggingActive: originalLoggingActive,
		RunDir:        originalRunDir,
		Debug:         originalDebug,
		HadLogging:    hadLoggingActive,
		HadRunDir:     hadRunDir,
		HadDebug:      hadDebug,
	}
}

func installLoggedActionStreams(
	options Options,
	logs hookRunLogs,
) (loggedActionStreams, *sync.WaitGroup, error) {
	streams, err := openLoggedActionStreams()
	if err != nil {
		return loggedActionStreams{}, nil, err
	}

	waitGroup := &sync.WaitGroup{}
	waitGroup.Add(loggedStreamCount)

	go copyLoggedStream(
		waitGroup,
		streams.stdoutReader,
		io.MultiWriter(options.Stdout, logs.stdout),
	)
	go copyLoggedStream(
		waitGroup,
		streams.stderrReader,
		io.MultiWriter(options.Stderr, logs.stderr),
	)

	os.Stdout = streams.stdoutWriter
	os.Stderr = streams.stderrWriter

	return streams, waitGroup, nil
}

func setLoggedActionEnv(runDir string) error {
	err := os.Setenv("CODE_ETHOS_HOOK_LOGGING_ACTIVE", "1")
	if err != nil {
		return fmt.Errorf("set hook logging env: %w", err)
	}

	err = os.Setenv("CODE_ETHOS_HOOK_RUN_DIR", runDir)
	if err != nil {
		return fmt.Errorf("set hook run dir env: %w", err)
	}

	return nil
}

func openLoggedActionStreams() (loggedActionStreams, error) {
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return loggedActionStreams{}, fmt.Errorf("create stdout pipe: %w", err)
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()

		return loggedActionStreams{}, fmt.Errorf("create stderr pipe: %w", err)
	}

	return loggedActionStreams{
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		stderrReader: stderrReader,
		stderrWriter: stderrWriter,
	}, nil
}

func runActionAndRestoreHookLogEnv(
	action func() int,
	command []string,
	runDir string,
	root string,
	originalEnv hookLogEnvSnapshot,
	debug bool,
) int {
	defer restoreHookLogEnv(originalEnv)

	if debug {
		_ = os.Setenv(debuglog.EnvName, "1")
	}

	logMakeProcess("make.process.enter", command, runDir, root, -1, os.Getpid())

	startedAt := debuglog.ProcessEnter(
		command,
		root,
		zap.String("run_dir", runDir),
		zap.Bool("in_process", true),
	)
	status := action()
	debuglog.ProcessExit(
		startedAt,
		command,
		root,
		status,
		nil,
		zap.String("run_dir", runDir),
		zap.Bool("in_process", true),
	)
	logMakeProcess("make.process.exit", command, runDir, root, status, os.Getpid())

	return status
}

func restoreHookLogEnv(original hookLogEnvSnapshot) {
	restoreEnv(
		"CODE_ETHOS_HOOK_LOGGING_ACTIVE",
		original.LoggingActive,
		original.HadLogging,
	)
	restoreEnv("CODE_ETHOS_HOOK_RUN_DIR", original.RunDir, original.HadRunDir)
	restoreEnv(debuglog.EnvName, original.Debug, original.HadDebug)
}

func restoreEnv(name, value string, hadValue bool) {
	if hadValue {
		_ = os.Setenv(name, value)

		return
	}

	_ = os.Unsetenv(name)
}

func restoreLoggedStreams(
	originalStdout *os.File,
	originalStderr *os.File,
	stdoutWriter *os.File,
	stderrWriter *os.File,
	waitGroup *sync.WaitGroup,
	status int,
	err error,
) (int, error) {
	os.Stdout = originalStdout
	os.Stderr = originalStderr

	stdoutErr := stdoutWriter.Close()
	stderrErr := stderrWriter.Close()

	waitGroup.Wait()

	if err != nil {
		return status, err
	}

	if stdoutErr != nil {
		return 1, fmt.Errorf("close stdout hook log pipe: %w", stdoutErr)
	}

	if stderrErr != nil {
		return 1, fmt.Errorf("close stderr hook log pipe: %w", stderrErr)
	}

	return status, nil
}

func copyLoggedStream(
	waitGroup *sync.WaitGroup,
	reader *os.File,
	writer io.Writer,
) {
	defer waitGroup.Done()
	defer reader.Close()

	_, err := io.Copy(writer, reader)
	if err != nil {
		feedback.Emit(
			os.Stderr,
			feedback.Text{Text: "warning: failed to copy hook log stream: " + err.Error()},
			feedback.FormatTOON,
		)
	}
}

func normalizedOptions(options Options) (Options, error) {
	if len(options.Command) == 0 {
		return Options{}, errCommandRequired
	}

	if options.Action == nil &&
		strings.HasPrefix(filepath.Base(options.Command[0]), "coding-ethos-") {
		return Options{}, errCodingEthosCommandUnsupported
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
	_, err := repoignore.RepairGitignore(options.Root)
	if err != nil {
		return fmt.Errorf("repair hook log runtime ignores: %w", err)
	}

	if gitPathIgnored(options, hookLogMemoryTestPath) {
		return apperror.Wrapf(errHookLogMemoryIgnored, "%s", hookLogMemoryIgnoredFeedback())
	}

	return requireIgnored(options, hookLogRunDirPath)
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
	if gitPathIgnored(options, path) {
		return nil
	}

	if repoignore.ContainsPath(options.Root, path) {
		return nil
	}

	return apperror.Wrapf(
		errHookLogRuntimeOutputNotIgnored,
		"%s",
		hookLogIgnoreRequiredFeedback(path),
	)
}

func hookLogIgnoreRequiredFeedback(path string) string {
	return feedback.MustRender(feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("status", "fatal"),
			feedback.S("severity", "fatal"),
			feedback.S("invariant", "hook runtime output must be ignored"),
			feedback.S("path", path),
			feedback.S("observed", "not_ignored"),
			feedback.S("expected", "ignored"),
			feedback.S(
				"repair",
				"add generated .coding-ethos runtime subpaths to the repo "+
					".gitignore before hook logs are written",
			),
		},
	}, feedback.FormatTOON)
}

func hookLogMemoryIgnoredFeedback() string {
	return feedback.MustRender(feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("status", "fatal"),
			feedback.S("severity", "fatal"),
			feedback.S("invariant", "repo memories must remain trackable"),
			feedback.S("path", hookLogMemoryTestPath),
			feedback.S("observed", "ignored"),
			feedback.S("expected", "not_ignored"),
			feedback.S(
				"repair",
				"remove broad .coding-ethos memory ignores so repo memories "+
					"remain trackable",
			),
		},
	}, feedback.FormatTOON)
}

func gitPathIgnored(options Options, path string) bool {
	cmd := safeexec.Command(
		options.GitPath,
		"-C",
		options.Root,
		"check-ignore",
		"--no-index",
		"--quiet",
		path,
	)
	cmd.Env = realgit.CleanGitLocalEnv(os.Environ())

	err := cmd.Run()

	return err == nil
}

type hookRunMetadata struct {
	StartedAt  time.Time
	RunID      string
	RepoRoot   string
	BundleRoot string
	Command    []string
	Debug      bool
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

	_, inlineErrF := fmt.Fprintf(
		file,
		"debug=%s\n",
		shellQuote(strconv.FormatBool(metadata.Debug)),
	)
	if inlineErrF != nil {
		return fmt.Errorf("write metadata: %w", inlineErrF)
	}

	return nil
}

func logMakeProcess(
	event string,
	command []string,
	runDir string,
	cwd string,
	exitCode int,
	pid int,
) {
	if !commandIncludesMake(command) {
		return
	}

	fields := []zap.Field{
		zap.String("run_dir", runDir),
		zap.String("cwd", cwd),
		zap.Strings("argv", command),
		zap.Int("exit_code", exitCode),
		zap.Int("pid", pid),
		zap.Int("ppid", os.Getppid()),
	}
	debuglog.Debug(event, fields...)
}

func commandIncludesMake(command []string) bool {
	for _, part := range command {
		if filepath.Base(part) == "make" {
			return true
		}
	}

	return false
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
	return processstatus.ExitCode(err, 1)
}
