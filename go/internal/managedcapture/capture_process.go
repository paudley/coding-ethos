// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"errors"
	"os"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

type capturedProcessIO struct {
	stdoutReader *os.File
	stdoutWriter *os.File
	stderrReader *os.File
	stderrWriter *os.File
	files        []*os.File
}

func openCapturedProcessIO(plan sandbox.Plan) (capturedProcessIO, processResult, bool) {
	stdoutReader, stdoutWriter, stdoutErr := os.Pipe()
	if stdoutErr != nil {
		return capturedProcessIO{}, processResult{
			err:      stdoutErr,
			exitCode: capturedCommandNotFoundCode,
		}, false
	}

	stderrReader, stderrWriter, stderrErr := os.Pipe()
	if stderrErr != nil {
		closeErr := errors.Join(stdoutReader.Close(), stdoutWriter.Close())

		return capturedProcessIO{}, processResult{
			err:      errors.Join(stderrErr, closeErr),
			exitCode: capturedCommandNotFoundCode,
		}, false
	}

	return capturedProcessIO{
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		stderrReader: stderrReader,
		stderrWriter: stderrWriter,
		files: capturedProcessFiles(
			stdoutWriter,
			stderrWriter,
			plan.ExtraFiles,
		),
	}, processResult{}, true
}

func (processIO capturedProcessIO) closeReaders() {
	_ = processIO.stdoutReader.Close()
	_ = processIO.stderrReader.Close()
}

func (processIO capturedProcessIO) closeWriters() {
	_ = processIO.stdoutWriter.Close()
	_ = processIO.stderrWriter.Close()
}

func assignCapturedProcessToCgroup(
	ctx context.Context,
	process *os.Process,
	cgroup *sandbox.Cgroup,
	startedAt time.Time,
	argv []string,
	request captureRequest,
	cacheEnv sandboxCacheEnvironment,
	copyDone <-chan error,
	buffers *captureBuffers,
) (processResult, bool) {
	assignErr := cgroup.AssignProcess(process)
	if assignErr == nil {
		return processResult{}, false
	}

	debugCapturedProcessExit(
		startedAt,
		argv,
		request,
		capturedExitCode(assignErr),
		assignErr,
	)

	return failedCgroupAssignmentResult(
		ctx,
		process,
		copyDone,
		cacheEnv,
		buffers,
		assignErr,
	), true
}

func failedCapturedProcessStart(
	startedAt time.Time,
	argv []string,
	request captureRequest,
	copyDone <-chan error,
	cacheEnv sandboxCacheEnvironment,
	startErr error,
) processResult {
	debugCapturedProcessExit(
		startedAt,
		argv,
		request,
		capturedExitCode(startErr),
		startErr,
	)

	return failedProcessStartResult(copyDone, cacheEnv, startErr)
}

func startCapturedOSProcess(
	request captureRequest,
	plan sandbox.Plan,
	files []*os.File,
	argv []string,
	cacheEnv sandboxCacheEnvironment,
	cgroup *sandbox.Cgroup,
	evidence sandbox.Evidence,
) (*os.Process, time.Time, error) {
	startedAt := debuglog.ProcessEnter(
		argv,
		request.Cwd,
		zap.String("tool", request.Tool),
		zap.Bool("sandboxed", true),
		zap.String("sandbox_backend", evidence.Backend),
	)

	process, startErr := os.StartProcess(
		plan.Executable,
		argv,
		&os.ProcAttr{
			Dir:   request.Cwd,
			Env:   capturedProcessEnv(os.Environ(), cacheEnv),
			Files: files,
			Sys:   capturedProcessSysProcAttr(cgroup, evidence),
		},
	)
	if startErr != nil {
		return process, startedAt, capturedProcessStartError{cause: startErr}
	}

	return process, startedAt, nil
}

type capturedProcessStartError struct {
	cause error
}

func (err capturedProcessStartError) Error() string {
	return err.cause.Error()
}

func (err capturedProcessStartError) Unwrap() error {
	return err.cause
}

func debugCapturedProcessExit(
	startedAt time.Time,
	argv []string,
	request captureRequest,
	exitCode int,
	err error,
) {
	debuglog.ProcessExit(
		startedAt,
		argv,
		request.Cwd,
		exitCode,
		err,
		zap.String("tool", request.Tool),
		zap.Bool("sandboxed", true),
	)
}
