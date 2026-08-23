// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	eventLogDirMode          = 0o700
	eventLogFileMode         = 0o600
	eventLogScannerInitBytes = 64 * 1024
	eventLogMaxRecordBytes   = 64 * 1024 * 1024
	eventLogMaxCreateTries   = 10_000
)

var (
	errEventLogCreateAttemptsExhausted = errors.New(
		"create unique code-intel event log attempts exhausted",
	)
	errEventLogNotPrivate = errors.New(
		"code-intel event stream is not a private regular file",
	)
	errEventLogShortWrite = errors.New(
		"code-intel event stream append was incomplete",
	)
)

// EventRecord is the durable append-only code-intel telemetry unit.
type EventRecord struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	RecordedAtUTC string          `json:"recorded_at_utc"`
	SourceRunID   string          `json:"source_run_id,omitempty"`
	TraceID       string          `json:"trace_id,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	Tool          string          `json:"tool,omitempty"`
	CommandShape  string          `json:"command_shape_sha256,omitempty"`
	PolicyID      string          `json:"policy_id,omitempty"`
	SkillID       string          `json:"skill_id,omitempty"`
	Path          string          `json:"path,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// EventLog appends and reads per-run JSONL event files.
type EventLog struct {
	dir string
}

func DefaultEventLogDir(root string) string {
	return filepath.Join(root, downstreamStateDir, "events")
}

func NewEventLog(dir string) EventLog {
	return EventLog{dir: dir}
}

func (log EventLog) Append(runID string, records []EventRecord) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return apperror.StaticError("event log run ID is required")
	}

	if len(records) == 0 {
		return nil
	}

	err := os.MkdirAll(log.dir, eventLogDirMode)
	if err != nil {
		return fmt.Errorf("create code-intel event log dir: %w", err)
	}

	path, file, err := createUniqueEventLogFile(log.dir, runID)
	if err != nil {
		return err
	}

	closed := false

	defer func() {
		if !closed {
			_ = file.Close()
			_ = os.Remove(filepath.Clean(path))
		}
	}()

	encoder := json.NewEncoder(file)

	for index, record := range records {
		record = normalizeEventRecord(record, runID, index)

		validateErr := validateEventRecord(record)
		if validateErr != nil {
			return validateErr
		}

		encodeErr := encoder.Encode(record)
		if encodeErr != nil {
			return fmt.Errorf("write code-intel event %q: %w", record.ID, encodeErr)
		}
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("close code-intel event log %q: %w", path, err)
	}

	closed = true

	return nil
}

// AppendStream appends records to one private, session-addressed JSONL stream.
// It is used for high-frequency proxy evidence so one hook event does not
// create one filesystem object. The content remains append-only and each
// record keeps its own deterministic identity.
func (log EventLog) AppendStream(streamID string, records []EventRecord) error {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return apperror.StaticError("event log stream ID is required")
	}

	if len(records) == 0 {
		return nil
	}

	err := os.MkdirAll(log.dir, eventLogDirMode)
	if err != nil {
		return fmt.Errorf("create code-intel event log dir: %w", err)
	}

	payload, err := encodeEventStream(streamID, records)
	if err != nil {
		return err
	}

	digest := sha256.Sum256([]byte(streamID))
	fileName := "session-" + hex.EncodeToString(digest[:])[:32] + ".jsonl"
	path := filepath.Join(log.dir, fileName)

	return appendEventStream(path, payload)
}

func encodeEventStream(streamID string, records []EventRecord) ([]byte, error) {
	var payload bytes.Buffer

	encoder := json.NewEncoder(&payload)

	for index, record := range records {
		record = normalizeEventRecord(record, streamID, index)

		err := validateEventRecord(record)
		if err != nil {
			return nil, err
		}

		err = encoder.Encode(record)
		if err != nil {
			return nil, fmt.Errorf("encode code-intel stream event %q: %w", record.ID, err)
		}
	}

	return payload.Bytes(), nil
}

func openPrivateEventStream(path string) (int, error) {
	descriptor, err := unix.Open(
		filepath.Clean(path),
		unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		eventLogFileMode,
	)
	if err != nil {
		return -1, fmt.Errorf("open code-intel event stream %q: %w", path, err)
	}

	var info unix.Stat_t

	err = unix.Fstat(descriptor, &info)
	if err != nil {
		_ = unix.Close(descriptor)

		return -1, fmt.Errorf("stat code-intel event stream %q: %w", path, err)
	}

	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0o077 != 0 {
		_ = unix.Close(descriptor)

		return -1, fmt.Errorf("%w: %q", errEventLogNotPrivate, path)
	}

	return descriptor, nil
}

func appendEventStream(path string, payload []byte) error {
	descriptor, err := openPrivateEventStream(path)
	if err != nil {
		return err
	}

	defer func() { _ = unix.Close(descriptor) }()

	err = unix.Flock(descriptor, unix.LOCK_EX)
	if err != nil {
		return fmt.Errorf("lock code-intel event stream %q: %w", path, err)
	}

	written, err := unix.Write(descriptor, payload)
	if err != nil {
		return fmt.Errorf("append code-intel event stream %q: %w", path, err)
	}

	if written != len(payload) {
		return fmt.Errorf(
			"%w %q: wrote %d of %d bytes",
			errEventLogShortWrite,
			path,
			written,
			len(payload),
		)
	}

	err = unix.Fsync(descriptor)
	if err != nil {
		return fmt.Errorf("sync code-intel event stream %q: %w", path, err)
	}

	return nil
}

func (log EventLog) Records() iter.Seq2[EventRecord, error] {
	return func(yield func(EventRecord, error) bool) {
		err := filepath.WalkDir(
			log.dir,
			func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}

				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
					return nil
				}

				return yieldEventLogFile(path, yield)
			},
		)
		if err != nil && !os.IsNotExist(err) {
			var record EventRecord

			_ = yield(record, fmt.Errorf("read code-intel event logs: %w", err))
		}
	}
}

func (log EventLog) ReadAll() ([]EventRecord, error) {
	records := []EventRecord{}

	for record, err := range log.Records() {
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}

func EventLogStats(root string) (int, error) {
	count := 0

	for _, err := range NewEventLog(DefaultEventLogDir(ResolveStateRoot(root))).Records() {
		if err != nil {
			return 0, err
		}

		count++
	}

	return count, nil
}

func yieldEventLogFile(
	path string,
	yield func(EventRecord, error) bool,
) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open code-intel event log %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, eventLogScannerInitBytes), eventLogMaxRecordBytes)

	for line := 1; scanner.Scan(); line++ {
		var record EventRecord

		unmarshalErr := json.Unmarshal(scanner.Bytes(), &record)
		if unmarshalErr != nil {
			return fmt.Errorf(
				"decode code-intel event %s:%d: %w",
				path,
				line,
				unmarshalErr,
			)
		}

		validateErr := validateEventRecord(record)
		if validateErr != nil {
			return fmt.Errorf(
				"validate code-intel event %s:%d: %w",
				path,
				line,
				validateErr,
			)
		}

		if !yield(record, nil) {
			return nil
		}
	}

	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("scan code-intel event log %q: %w", path, err)
	}

	return nil
}

func createUniqueEventLogFile(
	dir string,
	runID string,
) (string, *os.File, error) {
	for index := range eventLogMaxCreateTries {
		name := runID
		if index > 0 {
			name += "-" + strconv.Itoa(index)
		}

		path := filepath.Join(dir, name+".jsonl")

		file, err := os.OpenFile(
			filepath.Clean(path),
			os.O_CREATE|os.O_EXCL|os.O_WRONLY,
			eventLogFileMode,
		)
		if err == nil {
			return path, file, nil
		}

		if os.IsExist(err) {
			continue
		}

		return "", nil, fmt.Errorf("open code-intel event log %q: %w", path, err)
	}

	return "", nil, fmt.Errorf(
		"%w: run %q after %d attempts",
		errEventLogCreateAttemptsExhausted,
		runID,
		eventLogMaxCreateTries,
	)
}

func normalizeEventRecord(record EventRecord, runID string, ordinal int) EventRecord {
	record.Kind = strings.TrimSpace(record.Kind)

	record.SourceRunID = firstNonEmpty(record.SourceRunID, runID)
	if strings.TrimSpace(record.RecordedAtUTC) == "" {
		record.RecordedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}

	if strings.TrimSpace(record.ID) == "" {
		record.ID = deterministicEventID(record, ordinal)
	}

	return record
}

func validateEventRecord(record EventRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return apperror.StaticError("event record id is required")
	}

	if strings.TrimSpace(record.Kind) == "" {
		return apperror.StaticError("event record kind is required")
	}

	if strings.TrimSpace(record.RecordedAtUTC) == "" {
		return apperror.StaticError("event record recorded_at_utc is required")
	}

	return nil
}

func deterministicEventID(record EventRecord, ordinal int) string {
	payload := strings.Join([]string{
		record.Kind,
		record.SourceRunID,
		record.TraceID,
		record.PolicyID,
		record.Path,
		strconv.Itoa(ordinal),
		string(record.Payload),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))

	return "evt-" + hex.EncodeToString(sum[:])
}
