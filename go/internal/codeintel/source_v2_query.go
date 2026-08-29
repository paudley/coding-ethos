// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	defaultSourceV2QueryLimit = 50
	maximumSourceV2QueryLimit = 1000
)

var (
	ErrSourceIndexMissing        = errors.New("code-intel v2 source generation is missing")
	ErrSourceIndexStale          = errors.New("code-intel v2 source generation is stale")
	errInvalidSourceV2Generation = errors.New("invalid code-intel v2 generation")
)

// SourceIndexQuery filters immutable source facts from one exact generation.
type SourceIndexQuery struct {
	Path     string
	Language string
	Limit    int
}

// SourceIndexRecord binds path-neutral fragment facts to one manifest path.
type SourceIndexRecord struct {
	Path          string                  `json:"path"`
	Language      string                  `json:"language"`
	ContentSHA256 string                  `json:"content_sha256"`
	FragmentID    string                  `json:"fragment_id"`
	Symbols       []SourceSymbolFact      `json:"symbols,omitempty"`
	Imports       []SourceImportFact      `json:"imports,omitempty"`
	Facts         []ExternalExtractorFact `json:"facts,omitempty"`
}

// SourceIndexQueryResult carries the exact generation identity with every result set.
type SourceIndexQueryResult struct {
	Contract        string              `json:"contract"`
	SourceReadiness SourceReadiness     `json:"source_readiness"`
	Records         []SourceIndexRecord `json:"records"`
}

// QuerySourceIndex reads one lane's immutable base-plus-delta generation.
func QuerySourceIndex(
	ctx context.Context,
	root string,
	query SourceIndexQuery,
) (SourceIndexQueryResult, error) {
	status, err := SourceIndexStatus(ctx, root)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	err = requireQueryableSourceV2Status(status)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	path, err := normalizeSourceV2QueryPath(query.Path)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	limit := normalizedSourceV2QueryLimit(query.Limit)

	layout, err := resolveSourceV2Layout(ctx, root)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	base, err := loadSourceV2BaseManifest(layout, status.Storage.BaseManifestID)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	delta, err := loadSourceV2DeltaManifest(layout, status.Storage.DeltaManifestID)
	if err != nil {
		return SourceIndexQueryResult{}, err
	}

	entries := mergeSourceV2Entries(base.Entries, delta.Entries, delta.Tombstones)
	ordered := sourceV2SortedManifestEntries(entries)

	records := make([]SourceIndexRecord, 0, min(len(ordered), limit))
	for _, entry := range ordered {
		if !sourceV2QueryMatches(entry, path, query.Language) {
			continue
		}

		fragment, readErr := loadSourceV2Fragment(layout, entry)
		if readErr != nil {
			return SourceIndexQueryResult{}, readErr
		}

		records = append(records, SourceIndexRecord{
			Path:          entry.Path,
			Language:      entry.Language,
			ContentSHA256: entry.ContentSHA256,
			FragmentID:    entry.FragmentID,
			Symbols:       fragment.Symbols,
			Imports:       fragment.Imports,
			Facts:         fragment.Facts,
		})
		if len(records) >= limit {
			break
		}
	}

	return SourceIndexQueryResult{
		Contract:        SourceV2Contract,
		SourceReadiness: status.SourceReadiness,
		Records:         records,
	}, nil
}

func requireQueryableSourceV2Status(status SourceStatusReceipt) error {
	switch status.SourceReadiness.Status {
	case SourceStatusMissing:
		return ErrSourceIndexMissing
	case SourceStatusStale:
		return ErrSourceIndexStale
	default:
		return nil
	}
}

func normalizedSourceV2QueryLimit(limit int) int {
	if limit <= 0 {
		return defaultSourceV2QueryLimit
	}

	return min(limit, maximumSourceV2QueryLimit)
}

func sourceV2QueryMatches(
	entry sourceV2ManifestEntry,
	path string,
	language string,
) bool {
	return entry.Status == sourceV2EntryStatusIndexed &&
		(path == "" || entry.Path == path) &&
		(language == "" || entry.Language == language)
}

func normalizeSourceV2QueryPath(path string) (string, error) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" {
		return "", nil
	}

	path = strings.TrimPrefix(path, "./")

	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") ||
		filepath.IsAbs(path) {
		return "", fmt.Errorf(
			"%w: invalid code-intel v2 query path %q",
			errInvalidSourceV2Generation,
			path,
		)
	}

	return path, nil
}

func loadSourceV2BaseManifest(
	layout sourceV2Layout,
	manifestID string,
) (sourceV2BaseManifest, error) {
	if !validSourceV2ID(manifestID, "base") {
		return sourceV2BaseManifest{}, fmt.Errorf(
			"%w: invalid code-intel base manifest ID %q",
			errInvalidSourceV2Generation,
			manifestID,
		)
	}

	var manifest sourceV2BaseManifest

	err := readSourceV2JSON(
		layout.baseManifestPath(manifestID),
		&manifest,
	)
	if err != nil {
		return sourceV2BaseManifest{}, err
	}

	if manifest.Contract != SourceV2Contract ||
		manifest.Kind != sourceV2BaseManifestKind || manifest.ManifestID != manifestID {
		return sourceV2BaseManifest{}, fmt.Errorf(
			"%w: invalid code-intel v2 base manifest",
			errInvalidSourceV2Generation,
		)
	}

	computedID, err := sourceV2ManifestID("base", manifest)
	if err != nil {
		return sourceV2BaseManifest{}, err
	}

	if computedID != manifestID {
		return sourceV2BaseManifest{}, fmt.Errorf(
			"%w: code-intel v2 base manifest content ID mismatch",
			errInvalidSourceV2Generation,
		)
	}

	return manifest, nil
}

func verifySourceV2Generation(
	layout sourceV2Layout,
	receipt SourceStatusReceipt,
) error {
	base, err := loadSourceV2BaseManifest(layout, receipt.Storage.BaseManifestID)
	if err != nil {
		return fmt.Errorf("verify code-intel v2 base manifest: %w", err)
	}

	delta, err := loadSourceV2DeltaManifest(layout, receipt.Storage.DeltaManifestID)
	if err != nil {
		return fmt.Errorf("verify code-intel v2 delta manifest: %w", err)
	}

	identity := receipt.SourceReadiness.Identity

	expectedGenerationID := GenerationID(sourceV2Digest(
		"generation",
		string(identity.SourceSnapshotID)+"\x00"+
			base.ManifestID+"\x00"+delta.ManifestID,
	))
	if receipt.Storage.SharedRoot != layout.sharedRoot ||
		receipt.Storage.LaneRoot != layout.laneRoot ||
		identity.GenerationID != expectedGenerationID ||
		!sourceV2BaseIdentityMatches(base, identity) ||
		!sourceV2DeltaIdentityMatches(delta, base, identity) {
		return fmt.Errorf(
			"%w: code-intel v2 manifest identity does not match receipt",
			errInvalidSourceV2Generation,
		)
	}

	entries := mergeSourceV2Entries(base.Entries, delta.Entries, delta.Tombstones)
	for _, entry := range entries {
		if entry.Status != sourceV2EntryStatusIndexed {
			continue
		}

		if !sourceV2FragmentReady(layout.fragmentPath(entry.FragmentID), entry) {
			return fmt.Errorf(
				"%w: code-intel v2 fragment is missing or invalid: %s",
				errInvalidSourceV2Generation,
				entry.Path,
			)
		}
	}

	return nil
}

func sourceV2BaseIdentityMatches(
	base sourceV2BaseManifest,
	identity SourceIdentity,
) bool {
	return base.RepositoryID == identity.RepositoryID &&
		base.HeadOID == identity.HeadOID &&
		base.ConfigHash == identity.ConfigHash &&
		base.ExtractorSetHash == identity.ExtractorSetHash
}

func sourceV2DeltaIdentityMatches(
	delta sourceV2DeltaManifest,
	base sourceV2BaseManifest,
	identity SourceIdentity,
) bool {
	return delta.RepositoryID == identity.RepositoryID &&
		delta.WorktreeID == identity.WorktreeID &&
		delta.HeadOID == identity.HeadOID &&
		delta.BaseManifestID == base.ManifestID &&
		delta.ConfigHash == identity.ConfigHash &&
		delta.ExtractorSetHash == identity.ExtractorSetHash
}

func loadSourceV2DeltaManifest(
	layout sourceV2Layout,
	manifestID string,
) (sourceV2DeltaManifest, error) {
	if !validSourceV2ID(manifestID, "delta") {
		return sourceV2DeltaManifest{}, fmt.Errorf(
			"%w: invalid code-intel delta manifest ID %q",
			errInvalidSourceV2Generation,
			manifestID,
		)
	}

	var manifest sourceV2DeltaManifest

	err := readSourceV2JSON(
		layout.deltaManifestPath(manifestID),
		&manifest,
	)
	if err != nil {
		return sourceV2DeltaManifest{}, err
	}

	if manifest.Contract != SourceV2Contract ||
		manifest.Kind != sourceV2DeltaManifestKind || manifest.ManifestID != manifestID {
		return sourceV2DeltaManifest{}, fmt.Errorf(
			"%w: invalid code-intel v2 delta manifest",
			errInvalidSourceV2Generation,
		)
	}

	computedID, err := sourceV2DeltaManifestID(manifest)
	if err != nil {
		return sourceV2DeltaManifest{}, err
	}

	if computedID != manifestID {
		return sourceV2DeltaManifest{}, fmt.Errorf(
			"%w: code-intel v2 delta manifest content ID mismatch",
			errInvalidSourceV2Generation,
		)
	}

	return manifest, nil
}

func loadSourceV2Fragment(
	layout sourceV2Layout,
	entry sourceV2ManifestEntry,
) (sourceV2Fragment, error) {
	if !validSourceV2ID(entry.FragmentID, "fragment") {
		return sourceV2Fragment{}, fmt.Errorf(
			"%w: invalid code-intel fragment ID %q",
			errInvalidSourceV2Generation,
			entry.FragmentID,
		)
	}

	var fragment sourceV2Fragment

	err := readSourceV2JSON(
		layout.fragmentPath(entry.FragmentID),
		&fragment,
	)
	if err != nil {
		return sourceV2Fragment{}, err
	}

	if fragment.Contract != SourceV2Contract ||
		fragment.Kind != sourceV2FragmentKind ||
		fragment.FragmentID != entry.FragmentID ||
		fragment.ContentSHA256 != entry.ContentSHA256 {
		return sourceV2Fragment{}, fmt.Errorf(
			"%w: invalid code-intel v2 fragment %q",
			errInvalidSourceV2Generation,
			entry.FragmentID,
		)
	}

	return fragment, nil
}

func readSourceV2JSON(path string, value any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read code-intel v2 object %s: %w", path, err)
	}

	err = json.Unmarshal(payload, value)
	if err != nil {
		return fmt.Errorf("decode code-intel v2 object %s: %w", path, err)
	}

	return nil
}

func validSourceV2ID(value, namespace string) bool {
	prefix := namespace + ":sha256:"

	digest := strings.TrimPrefix(value, prefix)
	if len(digest) != 64 || value == digest {
		return false
	}

	for _, character := range digest {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}

	return true
}

func sourceV2SortedManifestEntries(
	entries map[string]sourceV2ManifestEntry,
) []sourceV2ManifestEntry {
	ordered := make([]sourceV2ManifestEntry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}

	slices.SortFunc(ordered, func(left, right sourceV2ManifestEntry) int {
		return strings.Compare(left.Path, right.Path)
	})

	return ordered
}
