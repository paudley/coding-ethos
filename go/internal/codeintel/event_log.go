// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	eventLogDirMode  = 0o700
	eventLogFileMode = 0o600
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

	path := uniqueEventLogPath(log.dir, runID)
	tmpPath := path + ".tmp"

	file, err := os.OpenFile(
		filepath.Clean(tmpPath),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		eventLogFileMode,
	)
	if err != nil {
		return fmt.Errorf("open code-intel event log %q: %w", tmpPath, err)
	}

	closed := false

	defer func() {
		if !closed {
			_ = file.Close()
			_ = os.Remove(filepath.Clean(tmpPath))
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
		return fmt.Errorf("close code-intel event log %q: %w", tmpPath, err)
	}

	closed = true

	err = os.Rename(filepath.Clean(tmpPath), filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("publish code-intel event log %q: %w", path, err)
	}

	return nil
}

func (log EventLog) ReadAll() ([]EventRecord, error) {
	records := []EventRecord{}

	err := filepath.WalkDir(
		log.dir,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}

			fileRecords, readErr := readEventLogFile(path)
			if readErr != nil {
				return readErr
			}

			records = append(records, fileRecords...)

			return nil
		},
	)
	if err != nil {
		if os.IsNotExist(err) {
			return records, nil
		}

		return nil, fmt.Errorf("read code-intel event logs: %w", err)
	}

	return records, nil
}

func EventLogStats(root string) (int, error) {
	records, err := NewEventLog(DefaultEventLogDir(root)).ReadAll()
	if err != nil {
		return 0, err
	}

	return len(records), nil
}

func readEventLogFile(path string) ([]EventRecord, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open code-intel event log %q: %w", path, err)
	}
	defer file.Close()

	records := []EventRecord{}

	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		var record EventRecord

		unmarshalErr := json.Unmarshal(scanner.Bytes(), &record)
		if unmarshalErr != nil {
			return nil, fmt.Errorf(
				"decode code-intel event %s:%d: %w",
				path,
				line,
				unmarshalErr,
			)
		}

		validateErr := validateEventRecord(record)
		if validateErr != nil {
			return nil, fmt.Errorf(
				"validate code-intel event %s:%d: %w",
				path,
				line,
				validateErr,
			)
		}

		records = append(records, record)
	}

	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("scan code-intel event log %q: %w", path, err)
	}

	return records, nil
}

func uniqueEventLogPath(dir, runID string) string {
	for index := 0; ; index++ {
		name := runID
		if index > 0 {
			name += "-" + strconv.Itoa(index)
		}

		path := filepath.Join(dir, name+".jsonl")

		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return path
		}
	}
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
