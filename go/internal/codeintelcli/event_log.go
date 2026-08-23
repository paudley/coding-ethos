// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"encoding/json"
	"fmt"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func appendCLIEvent(root, runID string, record codeintel.EventRecord) error {
	if record.RecordedAtUTC == "" {
		record.RecordedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}

	runID = fmt.Sprintf("%s-%d", runID, time.Now().UTC().UnixNano())

	err := codeintel.NewEventLog(
		codeintel.DefaultEventLogDir(codeintel.ResolveStateRoot(root)),
	).
		Append(runID, []codeintel.EventRecord{record})
	if err != nil {
		return fmt.Errorf("append code-intel CLI event: %w", err)
	}

	return nil
}

func rawEventPayload(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal code-intel event payload: %w", err)
	}

	return payload, nil
}
