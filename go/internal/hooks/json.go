// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"encoding/json"
	"fmt"
	"io"
)

func DecodeEvent(reader io.Reader) (Event, error) {
	var event Event
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&event); err != nil {
		return Event{}, fmt.Errorf("decode hook event: %w", err)
	}
	return event, nil
}

func EncodeResult(writer io.Writer, result Result) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode hook result: %w", err)
	}
	return nil
}
