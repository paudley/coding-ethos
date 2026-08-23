// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPruneFullOutputEvidenceFilesEnforcesByteBudget(t *testing.T) {
	useIsolatedOutputEvidenceTempDir(t)
	cleanupOutputEvidenceFixtures(t)

	now := time.Now()
	oldPath := writeOutputEvidenceFixture(
		t,
		"old",
		strings.Repeat("o", 32),
		now.Add(-2*time.Hour),
	)
	newPath := writeOutputEvidenceFixture(
		t,
		"new",
		strings.Repeat("n", 32),
		now.Add(-time.Hour),
	)

	pruneFullOutputEvidenceFiles(now, 24*time.Hour, 32)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old evidence stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new evidence stat err = %v, want preserved", err)
	}
}

func TestWriteFullOutputEvidencePrunesByConfiguredAge(t *testing.T) {
	useIsolatedOutputEvidenceTempDir(t)
	cleanupOutputEvidenceFixtures(t)

	oldPath := writeOutputEvidenceFixture(
		t,
		"stale",
		"stale",
		time.Now().Add(-2*time.Hour),
	)

	writtenPath, err := writeFullOutputEvidence("fresh", time.Hour, 0)
	if err != nil {
		t.Fatalf("write full output evidence: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(writtenPath)
	})

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("stale evidence stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("written evidence stat err = %v, want present", err)
	}
}

func TestWriteFullOutputEvidenceIsContentAddressedCompressedAndReused(t *testing.T) {
	useIsolatedOutputEvidenceTempDir(t)
	cleanupOutputEvidenceFixtures(t)

	payload := strings.Repeat("repeated diagnostic output\n", 1_000)
	first, err := writeFullOutputEvidence(payload, time.Hour, 0)
	if err != nil {
		t.Fatalf("write first full output evidence: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(first) })
	second, err := writeFullOutputEvidence(payload, time.Hour, 0)
	if err != nil {
		t.Fatalf("reuse full output evidence: %v", err)
	}

	if first != second {
		t.Fatalf("content-addressed path changed: first=%q second=%q", first, second)
	}
	if filepath.Base(
		first,
	) != toolOutputEvidencePrefix+HashText(
		payload,
	)+toolOutputEvidenceSuffix {
		t.Fatalf("evidence path is not its content address: %q", first)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatalf("stat compressed evidence: %v", err)
	}
	if info.Size() >= int64(len(payload)) {
		t.Fatalf("evidence was not compressed: size=%d input=%d", info.Size(), len(payload))
	}
	if err := validateFullOutputEvidence(first, payload); err != nil {
		t.Fatalf("validate compressed evidence: %v", err)
	}
}

func useIsolatedOutputEvidenceTempDir(t *testing.T) {
	t.Helper()

	t.Setenv("TMPDIR", t.TempDir())
}

func cleanupOutputEvidenceFixtures(t *testing.T) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(os.TempDir(), toolOutputEvidencePattern))
	if err != nil {
		t.Fatalf("glob evidence fixtures: %v", err)
	}

	for _, path := range matches {
		if !isFullOutputEvidencePath(path) {
			continue
		}

		_ = os.Remove(path)
	}
}

func writeOutputEvidenceFixture(
	t *testing.T,
	label string,
	payload string,
	modTime time.Time,
) string {
	t.Helper()

	file, err := os.CreateTemp(
		"",
		toolOutputEvidencePrefix+label+"-*"+toolOutputEvidenceSuffix,
	)
	if err != nil {
		t.Fatalf("create evidence fixture: %v", err)
	}

	path := file.Name()
	_, writeErr := file.WriteString(payload)
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatalf("write evidence fixture: %v", writeErr)
	}
	if closeErr != nil {
		t.Fatalf("close evidence fixture: %v", closeErr)
	}

	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("set evidence fixture mtime: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Remove(path)
	})

	return path
}
