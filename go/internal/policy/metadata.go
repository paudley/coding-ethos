// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type Metadata struct {
	SourceHashes map[string]string `json:"source_hashes,omitempty"`
	BundleHash   string            `json:"bundle_hash"`
	GeneratedAt  string            `json:"generated_at"`
}

var errMetadataSourceHashesRequired = apperror.StaticError(
	"metadata does not contain source_hashes",
)

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

	err := encoder.Encode(metadata)
	if err != nil {
		return fmt.Errorf("encode policy metadata: %w", err)
	}

	return nil
}

func DecodeMetadata(reader io.Reader) (Metadata, error) {
	var metadata Metadata

	err := json.NewDecoder(reader).Decode(&metadata)
	if err != nil {
		return Metadata{}, fmt.Errorf("decode policy metadata: %w", err)
	}

	return metadata, nil
}

func ValidateMetadataSourceHashes(metadata Metadata) error {
	if len(metadata.SourceHashes) == 0 {
		return errMetadataSourceHashesRequired
	}

	paths := make([]string, 0, len(metadata.SourceHashes))
	for path := range metadata.SourceHashes {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	var failures []string

	for _, path := range paths {
		actual, err := hashFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				failures = append(failures, "missing policy source: "+path)

				continue
			}

			failures = append(
				failures,
				fmt.Sprintf("hash policy source %s: %v", path, err),
			)

			continue
		}

		if actual != metadata.SourceHashes[path] {
			failures = append(failures, "policy source hash mismatch: "+path)
		}
	}

	if len(failures) > 0 {
		return apperror.Wrapf(
			apperror.StaticError("policy metadata validation failed"),
			"%s",
			strings.Join(failures, "\n"),
		)
	}

	return nil
}

func hashFile(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read policy source %s: %w", path, err)
	}

	sum := sha256.Sum256(payload)

	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
