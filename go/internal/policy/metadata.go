// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

type Metadata struct {
	SourceHashes map[string]string `json:"source_hashes,omitempty"`
	BundleHash   string            `json:"bundle_hash"`
	GeneratedAt  string            `json:"generated_at"`
}

func HashBundle(bundle Bundle) (string, error) {
	payload, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal policy bundle for hash: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func BuildMetadata(bundle Bundle, sourceHashes map[string]string) (Metadata, error) {
	bundleHash, err := HashBundle(bundle)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{
		BundleHash:   bundleHash,
		GeneratedAt:  bundle.GeneratedAt,
		SourceHashes: sourceHashes,
	}, nil
}

func EncodeMetadata(writer io.Writer, metadata Metadata) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("encode policy metadata: %w", err)
	}
	return nil
}
