// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	storeMigrationManifestKind = "code_intel.store_migration.v2"
	storeMigrationFileMode     = 0o600
)

// StoreMigrationTable records the row-level merge and verification evidence
// for one table.
type StoreMigrationTable struct {
	Table              string `json:"table"`
	SourceSHA256       string `json:"source_sha256"`
	DestinationSHA256  string `json:"destination_sha256"`
	SourceRows         int64  `json:"source_rows"`
	ImportedRows       int64  `json:"imported_rows"`
	MatchedRows        int64  `json:"matched_rows"`
	DeduplicatedRows   int64  `json:"deduplicated_rows,omitempty"`
	DestinationRows    int64  `json:"destination_rows"`
	SourceRowsVerified bool   `json:"source_rows_verified"`
}

// StoreMigrationManifest is the durable evidence for one successful store
// merge.
type StoreMigrationManifest struct {
	Kind                    string                `json:"kind"`
	RepositoryRoot          string                `json:"repository_root"`
	SourcePath              string                `json:"source_path"`
	DestinationPath         string                `json:"destination_path"`
	SourceSHA256Before      string                `json:"source_sha256_before"`
	SourceSHA256After       string                `json:"source_sha256_after"`
	DestinationSHA256       string                `json:"destination_sha256"`
	StartedAtUTC            string                `json:"started_at_utc"`
	CompletedAtUTC          string                `json:"completed_at_utc"`
	Tables                  []StoreMigrationTable `json:"tables"`
	SchemaVersion           int                   `json:"schema_version"`
	SourceUnchanged         bool                  `json:"source_unchanged"`
	DestinationRowsVerified bool                  `json:"destination_rows_verified"`
}

// StoreMigrationResult reports the verified manifest and its detached digest.
type StoreMigrationResult struct {
	ManifestPath   string                 `json:"manifest_path"`
	ManifestSHA256 string                 `json:"manifest_sha256"`
	DigestPath     string                 `json:"digest_path"`
	Manifest       StoreMigrationManifest `json:"manifest"`
	Verified       bool                   `json:"verified"`
}

func newStoreMigrationManifest(
	repositoryRoot string,
	sourcePath string,
	destinationPath string,
	sourceHash string,
	started time.Time,
) StoreMigrationManifest {
	return StoreMigrationManifest{
		Kind:               storeMigrationManifestKind,
		RepositoryRoot:     repositoryRoot,
		SourcePath:         sourcePath,
		DestinationPath:    destinationPath,
		SourceSHA256Before: sourceHash,
		StartedAtUTC:       started.UTC().Format(time.RFC3339Nano),
		SchemaVersion:      schemaVersion,
	}
}

func defaultStoreMigrationManifestPath(destinationPath string, now time.Time) string {
	stamp := now.UTC().Format("20060102T150405.000000000Z")

	return destinationPath + ".migration-" + stamp + ".json"
}

func validateNewMigrationAuditPaths(
	sourcePath string,
	destinationPath string,
	manifestPath string,
) error {
	digestPath := manifestPath + ".sha256"
	for _, auditPath := range []string{manifestPath, digestPath} {
		if auditPath == sourcePath || auditPath == destinationPath {
			return fmt.Errorf(
				"%w: audit path collides with a database: %s",
				errStoreMigrationInvalid,
				auditPath,
			)
		}

		_, err := os.Stat(auditPath)
		if err == nil {
			return fmt.Errorf(
				"%w: audit file already exists: %s",
				errStoreMigrationInvalid,
				auditPath,
			)
		}

		if !os.IsNotExist(err) {
			return fmt.Errorf("stat store migration audit path: %w", err)
		}
	}

	return nil
}

func writeAndVerifyStoreMigrationManifest(
	manifest StoreMigrationManifest,
	manifestPath string,
) (StoreMigrationResult, error) {
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return StoreMigrationResult{}, fmt.Errorf("marshal store migration manifest: %w", err)
	}

	payload = append(payload, '\n')

	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	digestPath := manifestPath + ".sha256"

	err = atomicWriteNewMigrationFile(manifestPath, payload)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	digestPayload := []byte(
		digestText + "  " + filepath.Base(manifestPath) + "\n",
	)

	err = atomicWriteNewMigrationFile(digestPath, digestPayload)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	err = verifyStoreMigrationManifest(manifestPath, digestPath, digestText)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	return StoreMigrationResult{
		Manifest:       manifest,
		ManifestPath:   manifestPath,
		ManifestSHA256: digestText,
		DigestPath:     digestPath,
		Verified:       true,
	}, nil
}

func verifyStoreMigrationManifest(manifestPath, digestPath, expected string) error {
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read store migration manifest for verification: %w", err)
	}

	digest := sha256.Sum256(payload)

	actual := hex.EncodeToString(digest[:])
	if actual != expected {
		return fmt.Errorf(
			"%w: manifest hash mismatch: got %s, expected %s",
			errStoreMigrationIntegrity,
			actual,
			expected,
		)
	}

	digestPayload, err := os.ReadFile(digestPath)
	if err != nil {
		return fmt.Errorf("read store migration manifest digest: %w", err)
	}

	wantDigest := expected + "  " + filepath.Base(manifestPath) + "\n"
	if string(digestPayload) != wantDigest {
		return fmt.Errorf(
			"%w: manifest digest file is invalid",
			errStoreMigrationIntegrity,
		)
	}

	return nil
}

func atomicWriteNewMigrationFile(path string, payload []byte) error {
	directory := filepath.Dir(path)

	err := os.MkdirAll(directory, storeDirMode)
	if err != nil {
		return fmt.Errorf("create store migration manifest directory: %w", err)
	}

	_, statErr := os.Stat(path)
	if statErr == nil {
		return fmt.Errorf("%w: audit file already exists: %s", errStoreMigrationInvalid, path)
	}

	if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat store migration audit file: %w", statErr)
	}

	temporary, err := os.CreateTemp(directory, ".store-migration-*")
	if err != nil {
		return fmt.Errorf("create temporary store migration audit file: %w", err)
	}

	temporaryPath := temporary.Name()

	defer func() { _ = os.Remove(temporaryPath) }()

	err = writeMigrationFile(temporary, payload)
	if err != nil {
		return err
	}

	err = os.Link(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("publish store migration audit file: %w", err)
	}

	return nil
}

func writeMigrationFile(file *os.File, payload []byte) error {
	err := file.Chmod(storeMigrationFileMode)
	if err != nil {
		_ = file.Close()

		return fmt.Errorf("set store migration audit file permissions: %w", err)
	}

	_, err = file.Write(payload)
	if err != nil {
		_ = file.Close()

		return fmt.Errorf("write store migration audit file: %w", err)
	}

	err = file.Sync()
	if err != nil {
		_ = file.Close()

		return fmt.Errorf("sync store migration audit file: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("close store migration audit file: %w", err)
	}

	return nil
}

func storeMigrationFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open store migration file for hashing: %w", err)
	}
	defer file.Close()

	digest := sha256.New()

	_, err = io.Copy(digest, file)
	if err != nil {
		return "", fmt.Errorf("hash store migration file: %w", err)
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}
