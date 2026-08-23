// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEventLogAppendsAndReadsRecords(t *testing.T) {
	t.Parallel()

	log := NewEventLog(filepath.Join(t.TempDir(), "events"))
	payload := json.RawMessage(`{"status":"blocked"}`)

	err := log.Append("run-1", []EventRecord{
		{
			Kind:    "hook_decision",
			TraceID: "trace-1",
			Tool:    "Bash",
			Payload: payload,
		},
	})
	if err != nil {
		t.Fatalf("append event log: %v", err)
	}

	records, err := log.ReadAll()
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].ID == "" ||
		records[0].Kind != "hook_decision" ||
		records[0].SourceRunID != "run-1" ||
		string(records[0].Payload) != string(payload) {
		t.Fatalf("unexpected event record: %#v", records[0])
	}
}

func TestEventLogReadsLargeRecords(t *testing.T) {
	t.Parallel()

	log := NewEventLog(filepath.Join(t.TempDir(), "events"))
	payload := json.RawMessage(`{"body":"` + strings.Repeat("x", 70*1024) + `"}`)

	err := log.Append("run-1", []EventRecord{{Kind: "sarif", Payload: payload}})
	if err != nil {
		t.Fatalf("append large event log: %v", err)
	}

	records, err := log.ReadAll()
	if err != nil {
		t.Fatalf("read large event log: %v", err)
	}

	if len(records) != 1 || string(records[0].Payload) != string(payload) {
		t.Fatalf("large record mismatch: %#v", records)
	}
}

func TestEventLogAppendUsesNextAtomicPath(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "events")
	log := NewEventLog(dir)

	err := os.MkdirAll(dir, eventLogDirMode)
	if err != nil {
		t.Fatalf("create event log dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir, "run-1.jsonl"), []byte("{}\n"), eventLogFileMode)
	if err != nil {
		t.Fatalf("seed existing event log: %v", err)
	}

	err = log.Append("run-1", []EventRecord{{Kind: "hook_trace"}})
	if err != nil {
		t.Fatalf("append event log with collision: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "run-1-1.jsonl")); err != nil {
		t.Fatalf("stat collision event log: %v", err)
	}
}

func TestEventLogRejectsMissingKind(t *testing.T) {
	t.Parallel()

	log := NewEventLog(filepath.Join(t.TempDir(), "events"))

	err := log.Append("run-1", []EventRecord{{TraceID: "trace-1"}})
	if err == nil {
		t.Fatal("expected missing kind error")
	}
}

func TestEventLogAppendStreamAggregatesOneSessionAcrossConcurrentHooks(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "events")
	log := NewEventLog(dir)
	const count = 24
	var wait sync.WaitGroup
	errors := make(chan error, count)
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- log.AppendStream("session-shared", []EventRecord{{
				ID:      fmt.Sprintf("event-%02d", index),
				Kind:    "proxy_event",
				Payload: json.RawMessage(fmt.Sprintf(`{"ordinal":%d}`, index)),
			}})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("append session stream: %v", err)
		}
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatalf("glob session stream: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("one session created %d event files: %v", len(paths), paths)
	}
	records, err := log.ReadAll()
	if err != nil {
		t.Fatalf("read session stream: %v", err)
	}
	if len(records) != count {
		t.Fatalf("session stream records = %d, want %d", len(records), count)
	}
}
