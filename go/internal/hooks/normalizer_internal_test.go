// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"path/filepath"
	"testing"
)

func TestHookOutputNormalizerTreatsGoTempDirAsTemporary(t *testing.T) {
	goTempDir := filepath.Join(t.TempDir(), "go-temp")
	t.Setenv("GOTMPDIR", goTempDir)

	transcript := filepath.Join(goTempDir, "session", "transcript.jsonl")
	got := hookOutputNormalizer("/repo").preserveLines(transcript)
	if got != "<tmp>/session/transcript.jsonl" {
		t.Fatalf("normalized Go temp path = %q", got)
	}
}
