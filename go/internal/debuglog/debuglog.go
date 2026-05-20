// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package debuglog provides per-hook structured debug logging.
package debuglog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	Flag                  = "--coding-ethos-debug"
	EnvName               = "CODE_ETHOS_HOOK_DEBUG"
	RunDirEnv             = "CODE_ETHOS_HOOK_RUN_DIR"
	debugLog              = "debug.log"
	debugValue            = "1"
	fileMode              = 0o600
	timeKey               = "ts"
	messageKey            = "event"
	levelKey              = "level"
	callerKey             = "caller"
	baseProcessFieldCount = 5
)

//nolint:gochecknoglobals // hook debug logging is process-scoped state.
var (
	loggerMu sync.Mutex

	logger = zap.NewNop()
	closer io.Closer
)

func EnabledFromEnv() bool {
	return os.Getenv(EnvName) == debugValue
}

func Configure(enabled bool, runDir string, stderr io.Writer) error {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	resetLocked()

	if !enabled {
		logger = zap.NewNop()

		return nil
	}

	writers := []zapcore.WriteSyncer{}

	if runDir != "" {
		file, err := os.OpenFile(
			filepath.Join(runDir, debugLog),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY,
			fileMode,
		)
		if err != nil {
			return fmt.Errorf("open debug log: %w", err)
		}

		closer = file
		writers = append(writers, zapcore.AddSync(file))
	}

	if stderr != nil {
		writers = append(writers, zapcore.AddSync(stderr))
	}

	if len(writers) == 0 {
		logger = zap.NewNop()

		return nil
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = timeKey
	encoderConfig.MessageKey = messageKey
	encoderConfig.LevelKey = levelKey
	encoderConfig.CallerKey = callerKey
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writers...),
		zap.NewAtomicLevelAt(zap.DebugLevel),
	)

	logger = zap.New(core, zap.AddCaller())

	return nil
}

func Reset() {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	resetLocked()

	logger = zap.NewNop()
}

func Debug(event string, fields ...zap.Field) {
	loggerMu.Lock()
	current := logger
	loggerMu.Unlock()

	current.Debug(event, fields...)
}

func ProcessEnter(argv []string, cwd string, fields ...zap.Field) time.Time {
	startedAt := time.Now()

	Debug("process.exec.enter", processFields(argv, cwd, startedAt, fields...)...)

	return startedAt
}

func ProcessExit(
	startedAt time.Time,
	argv []string,
	cwd string,
	exitCode int,
	err error,
	fields ...zap.Field,
) {
	processFields := append(
		processFields(argv, cwd, startedAt, fields...),
		zap.Int("exit_code", exitCode),
		zap.Duration("elapsed", time.Since(startedAt)),
		zap.Error(err),
	)

	Debug("process.exec.exit", processFields...)
}

func EstimateTokens(text string) int {
	return agentproxy.ApproximateTokenizer{}.Count(text)
}

func Sync() error {
	loggerMu.Lock()
	current := logger
	loggerMu.Unlock()

	err := current.Sync()
	if err != nil {
		return fmt.Errorf("sync debug logger: %w", err)
	}

	return nil
}

func resetLocked() {
	if closer != nil {
		_ = closer.Close()
		closer = nil
	}
}

func processFields(
	argv []string,
	cwd string,
	startedAt time.Time,
	fields ...zap.Field,
) []zap.Field {
	processFields := make([]zap.Field, 0, baseProcessFieldCount+len(fields))
	processFields = append(
		processFields,
		zap.Time("started_at", startedAt.UTC()),
		zap.String("cwd", cwd),
		zap.Strings("argv", argv),
		zap.Int("argc", len(argv)),
		zap.Int("argv_token_estimate", EstimateTokens(strings.Join(argv, " "))),
	)

	return append(processFields, fields...)
}
