// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashBundleIsStable(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()

	first, err := HashBundle(bundle)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}

	second, err := HashBundle(bundle)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}

	if first != second {
		t.Fatalf("hash mismatch: %q != %q", first, second)
	}

	if !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("hash missing sha256 prefix: %q", first)
	}
}

func TestBuildMetadataUsesBundleGeneratedAt(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()

	metadata, err := BuildMetadata(
		bundle,
		map[string]string{"config.yaml": "sha256:test"},
	)
	if err != nil {
		t.Fatalf("build metadata: %v", err)
	}

	if metadata.GeneratedAt != bundle.GeneratedAt {
		t.Fatalf(
			"generated_at mismatch: got %q want %q",
			metadata.GeneratedAt,
			bundle.GeneratedAt,
		)
	}

	if metadata.SourceHashes["config.yaml"] != "sha256:test" {
		t.Fatalf("source hash missing: %#v", metadata.SourceHashes)
	}
}

func TestValidateMetadataSourceHashesAcceptsMatchingFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("policy: true\n"), 0o600); err != nil {
		t.Fatalf("write policy source: %v", err)
	}

	metadata := Metadata{
		SourceHashes: map[string]string{
			path: "sha256:592ecb12e4bc3f8c36ba39a3e896459351d5660458b492ff90460037c194a917",
		},
	}

	if err := ValidateMetadataSourceHashes(metadata); err != nil {
		t.Fatalf("validate source hashes: %v", err)
	}
}

func TestValidateMetadataSourceHashesRejectsMissingManifest(t *testing.T) {
	t.Parallel()

	err := ValidateMetadataSourceHashes(Metadata{})
	if err == nil || !strings.Contains(err.Error(), "source_hashes") {
		t.Fatalf("validate missing hashes error = %v", err)
	}
}

func TestValidateMetadataSourceHashesReportsMissingAndMismatchedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mismatchPath := filepath.Join(dir, "config.yaml")
	missingPath := filepath.Join(dir, "missing.yaml")
	if err := os.WriteFile(mismatchPath, []byte("policy: true\n"), 0o600); err != nil {
		t.Fatalf("write policy source: %v", err)
	}

	metadata := Metadata{
		SourceHashes: map[string]string{
			missingPath:  "sha256:missing",
			mismatchPath: "sha256:mismatch",
		},
	}

	err := ValidateMetadataSourceHashes(metadata)
	if err == nil {
		t.Fatal("expected source hash validation error")
	}
	for _, want := range []string{
		"missing policy source: " + missingPath,
		"policy source hash mismatch: " + mismatchPath,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error %q missing %q", err, want)
		}
	}
}
