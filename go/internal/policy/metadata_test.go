// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	. "blackcat.ca/coding-ethos/go/internal/policy"
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

func TestEncodeDecodeMetadataRoundTrips(t *testing.T) {
	t.Parallel()

	original := Metadata{
		BundleHash:  "sha256:bundle",
		GeneratedAt: "2026-05-04T00:00:00Z",
		SourceHashes: map[string]string{
			"coding_ethos.yml": "sha256:source",
		},
	}

	var buffer bytes.Buffer

	inlineErr0 := EncodeMetadata(&buffer, original)
	if inlineErr0 != nil {
		t.Fatalf("EncodeMetadata() error = %v", inlineErr0)
	}

	if !strings.Contains(buffer.String(), `"bundle_hash": "sha256:bundle"`) {
		t.Fatalf("metadata JSON should be indented and stable: %s", buffer.String())
	}

	decoded, err := DecodeMetadata(&buffer)
	if err != nil {
		t.Fatalf("DecodeMetadata() error = %v", err)
	}

	if decoded.BundleHash != original.BundleHash ||
		decoded.GeneratedAt != original.GeneratedAt ||
		decoded.SourceHashes["coding_ethos.yml"] != "sha256:source" {
		t.Fatalf("decoded metadata mismatch: %#v", decoded)
	}
}

func TestValidateMetadataSourceHashesAcceptsMatchingFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(path, []byte("policy: true\n"), 0o600)
	if err != nil {
		t.Fatalf("write policy source: %v", err)
	}

	metadata := Metadata{
		SourceHashes: map[string]string{
			path: "sha256:592ecb12e4bc3f8c36ba39a3e896459351d5660458b492ff90460037c194a917",
		},
	}

	err = ValidateMetadataSourceHashes(metadata)
	if err != nil {
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

	inlineErr1 := os.WriteFile(mismatchPath, []byte("policy: true\n"), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write policy source: %v", inlineErr1)
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

func TestFormatValidationErrorSortsMultilineErrors(t *testing.T) {
	t.Parallel()

	formatted := FormatValidationError(apperror.StaticError("z problem\na problem"))
	if formatted != "a problem\nz problem" {
		t.Fatalf("validation formatting should sort lines: %q", formatted)
	}

	err := ValidateMetadataSourceHashes(Metadata{
		SourceHashes: map[string]string{
			"/tmp/z-missing": "sha256:z",
			"/tmp/a-missing": "sha256:a",
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	formatted = FormatValidationError(err)

	lines := strings.Split(formatted, "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "/tmp/a-missing") ||
		!strings.Contains(lines[1], "/tmp/z-missing") {
		t.Fatalf("formatted validation error not sorted: %q", formatted)
	}

	if FormatValidationError(nil) != "" {
		t.Fatal("nil validation error should format as an empty string")
	}
}
