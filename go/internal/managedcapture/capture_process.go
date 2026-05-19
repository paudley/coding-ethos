// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"os"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

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
