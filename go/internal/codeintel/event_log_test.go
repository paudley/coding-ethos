// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"encoding/json"
	"path/filepath"
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

func TestEventLogRejectsMissingKind(t *testing.T) {
	t.Parallel()

	log := NewEventLog(filepath.Join(t.TempDir(), "events"))

	err := log.Append("run-1", []EventRecord{{TraceID: "trace-1"}})
	if err == nil {
		t.Fatal("expected missing kind error")
	}
}
