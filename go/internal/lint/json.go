// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

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
		return fmt.Errorf("encode lint result: %w", err)
	}

	return nil
}
