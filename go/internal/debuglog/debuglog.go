// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package debuglog provides per-hook structured debug logging.
package debuglog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	Flag       = "--coding-ethos-debug"
	EnvName    = "CODE_ETHOS_HOOK_DEBUG"
	RunDirEnv  = "CODE_ETHOS_HOOK_RUN_DIR"
	debugLog   = "debug.log"
	debugValue = "1"
	fileMode   = 0o600
	timeKey    = "ts"
	messageKey = "event"
	levelKey   = "level"
	callerKey  = "caller"
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
