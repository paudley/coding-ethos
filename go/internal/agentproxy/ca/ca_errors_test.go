// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca_test

import (
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

// caDir returns the on-disk CA storage directory for repoRoot, creating it so
// tests can plant malformed material before invoking the loader.
func caDir(t *testing.T, repoRoot string) string {
	t.Helper()

	dir := filepath.Join(repoRoot, ".coding-ethos", "cache", "agent-proxy-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create CA dir: %v", err)
	}

	return dir
}

func TestLoadRejectsCorruptCertificatePEM(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	dir := caDir(t, repoRoot)

	if err := os.WriteFile(
		filepath.Join(dir, "ca-cert.pem"),
		[]byte("not a pem"),
		0o600,
	); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	_, err := ca.Load(repoRoot)
	if err == nil || errors.Is(err, ca.ErrCANotProvisioned) {
		t.Fatalf("expected decode error for corrupt cert PEM, got %v", err)
	}
}

func TestLoadReturnsSentinelWhenKeyMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	dir := caDir(t, repoRoot)

	// A syntactically valid but parse-failing certificate block is enough to pass
	// readCertDER's PEM decode and reach the missing-key branch in Load.
	block := pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid der")},
	)
	if err := os.WriteFile(filepath.Join(dir, "ca-cert.pem"), block, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	_, err := ca.Load(repoRoot)
	if !errors.Is(err, ca.ErrCANotProvisioned) {
		t.Fatalf("expected ErrCANotProvisioned when key absent, got %v", err)
	}
}

func TestEnsureCATimesOutWhenLockHeldWithoutCA(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	dir := caDir(t, repoRoot)

	// Holding the mint lock with no valid CA on disk forces EnsureCA to exhaust
	// its bounded retry budget and surface the timeout sentinel rather than block
	// forever.
	lockPath := filepath.Join(dir, "mint.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o600); err != nil {
		t.Fatalf("plant lock: %v", err)
	}

	_, err := ca.EnsureCA(repoRoot, fixedNow())
	if !errors.Is(err, ca.ErrCAMintLockTimeout) {
		t.Fatalf("expected ErrCAMintLockTimeout, got %v", err)
	}
}

func TestEnsureCAReturnsConcurrentlyMintedCA(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	// Two concurrent EnsureCA callers race for the mint lock. The loser must
	// observe and return the CA the winner wrote rather than minting a second.
	var (
		results [2]ca.CA
		errs    [2]error
		wait    chan struct{}
	)

	wait = make(chan struct{})

	done := make(chan int, 2)

	for index := range 2 {
		go func() {
			<-wait

			results[index], errs[index] = ca.EnsureCA(repoRoot, fixedNow())
			done <- index
		}()
	}

	close(wait)
	<-done
	<-done

	for index := range 2 {
		if errs[index] != nil {
			t.Fatalf("concurrent EnsureCA[%d]: %v", index, errs[index])
		}
	}

	if results[0].Fingerprint() != results[1].Fingerprint() {
		t.Fatalf(
			"concurrent mints produced distinct CAs: %q vs %q",
			results[0].Fingerprint(),
			results[1].Fingerprint(),
		)
	}
}

func TestNewLeafIssuerRejectsCorruptKeyPEM(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	dir := caDir(t, repoRoot)

	if err := os.WriteFile(
		filepath.Join(dir, "ca-key.pem"),
		[]byte("garbage"),
		0o600,
	); err != nil {
		t.Fatalf("write key: %v", err)
	}

	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("der")})
	if err := os.WriteFile(filepath.Join(dir, "ca-cert.pem"), block, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	_, err := ca.NewLeafIssuer(repoRoot, ca.LeafIssuerOptions{})
	if err == nil || errors.Is(err, ca.ErrCANotProvisioned) {
		t.Fatalf("expected key-decode error, got %v", err)
	}
}
