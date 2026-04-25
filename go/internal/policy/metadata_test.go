// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"strings"
	"testing"
)

func TestHashBundleIsStable(t *testing.T) {
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
	bundle := ExampleBundle()
	metadata, err := BuildMetadata(bundle, map[string]string{"config.yaml": "sha256:test"})
	if err != nil {
		t.Fatalf("build metadata: %v", err)
	}
	if metadata.GeneratedAt != bundle.GeneratedAt {
		t.Fatalf("generated_at mismatch: got %q want %q", metadata.GeneratedAt, bundle.GeneratedAt)
	}
	if metadata.SourceHashes["config.yaml"] != "sha256:test" {
		t.Fatalf("source hash missing: %#v", metadata.SourceHashes)
	}
}
