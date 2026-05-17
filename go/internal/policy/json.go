// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

	err := decoder.Decode(&bundle)
	if err != nil {
		return Bundle{}, fmt.Errorf("decode policy bundle: %w", err)
	}

	return bundle, nil
}

func EncodeBundle(writer io.Writer, bundle Bundle) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(bundle)
	if err != nil {
		return fmt.Errorf("encode policy bundle: %w", err)
	}

	return nil
}
