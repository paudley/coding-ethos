// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"encoding/json"
	"fmt"
	"io"
)

func EncodeResult(writer io.Writer, result Result) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(result)
	if err != nil {
		return fmt.Errorf("encode git wrapper result: %w", err)
	}

	return nil
}
