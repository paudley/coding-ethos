// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

func fixedNow() time.Time {
	return time.Date(2026, time.June, 3, 12, 0, 0, 0, time.UTC)
}

func certFile(repoRoot string) string {
	return filepath.Join(
		repoRoot,
		".coding-ethos",
		"cache",
		"agent-proxy-ca",
		"ca-cert.pem",
	)
}

func keyFile(repoRoot string) string {
	return filepath.Join(
		repoRoot,
		".coding-ethos",
		"cache",
		"agent-proxy-ca",
		"ca-key.pem",
	)
}

func TestEnsureCAMintsWhenAbsent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	authority, err := ca.EnsureCA(repoRoot, fixedNow())
	if err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	if authority.Fingerprint() == "" {
		t.Fatal("expected non-empty fingerprint")
	}

	if authority.CertPath() != certFile(repoRoot) {
		t.Fatalf("cert path = %q want %q", authority.CertPath(), certFile(repoRoot))
	}

	for _, path := range []string{certFile(repoRoot), keyFile(repoRoot)} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist: %v", path, statErr)
		}
	}

	info, err := os.Stat(keyFile(repoRoot))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o want 0600", info.Mode().Perm())
	}
}

func TestEnsureCAReloadIsIdempotent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	first, err := ca.EnsureCA(repoRoot, fixedNow())
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	second, err := ca.EnsureCA(repoRoot, fixedNow().Add(time.Hour))
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf(
			"fingerprint changed on reload: %q -> %q",
			first.Fingerprint(),
			second.Fingerprint(),
		)
	}
}

func TestEnsureCARegeneratesWhenExpired(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	first, err := ca.EnsureCA(repoRoot, fixedNow())
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	future := fixedNow().Add(91 * 24 * time.Hour)

	second, err := ca.EnsureCA(repoRoot, future)
	if err != nil {
		t.Fatalf("regenerate ensure: %v", err)
	}

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("expected new fingerprint after expiry")
	}
}

func TestLoadReturnsSentinelWhenAbsent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	_, err := ca.Load(repoRoot)
	if !errors.Is(err, ca.ErrCANotProvisioned) {
		t.Fatalf("expected ErrCANotProvisioned, got %v", err)
	}
}
