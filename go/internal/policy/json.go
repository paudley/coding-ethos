// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"encoding/json"
	"fmt"
	"io"
)

func DecodeBundle(reader io.Reader) (Bundle, error) {
	var bundle Bundle
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("decode policy bundle: %w", err)
	}
	return bundle, nil
}

func EncodeBundle(writer io.Writer, bundle Bundle) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		return fmt.Errorf("encode policy bundle: %w", err)
	}
	return nil
}
