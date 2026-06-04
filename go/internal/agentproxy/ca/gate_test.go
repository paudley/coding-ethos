// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
)

func caDirExists(repoRoot string) bool {
	_, err := os.Stat(
		filepath.Join(repoRoot, ".coding-ethos", "cache", "agent-proxy-ca"),
	)

	return err == nil
}

func TestEvaluateDisabledWhenModeOff(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:      fixedNow(),
		Mode:     agentproxy.InterceptionModeOff,
		RepoRoot: repoRoot,
		EnvOptIn: true,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if evidence.Enabled || evidence.Denied {
		t.Fatalf("expected disabled, got %#v", evidence)
	}

	if caDirExists(repoRoot) {
		t.Fatal("CA directory must not be provisioned when disabled")
	}
}

func TestEvaluateDeniesWhenEnvOptInMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:      fixedNow(),
		Mode:     agentproxy.InterceptionModeRequired,
		RepoRoot: repoRoot,
		EnvOptIn: false,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if evidence.Enabled || !evidence.Denied {
		t.Fatalf("expected denied stale-config guard, got %#v", evidence)
	}

	if caDirExists(repoRoot) {
		t.Fatal("CA directory must not be provisioned when env opt-in missing")
	}
}

func TestEvaluateEnabledWithFingerprint(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:      fixedNow(),
		Mode:     agentproxy.InterceptionModeRequired,
		RepoRoot: repoRoot,
		EnvOptIn: true,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if !evidence.Enabled || evidence.Denied {
		t.Fatalf("expected enabled, got %#v", evidence)
	}

	if evidence.CAFingerprint == "" || evidence.CACertPath == "" {
		t.Fatalf("expected fingerprint and cert path, got %#v", evidence)
	}

	if !caDirExists(repoRoot) {
		t.Fatal("CA directory must be provisioned when enabled")
	}
}

func TestEvaluateDeniesOnFingerprintMismatch(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	_, err := ca.EnsureCA(repoRoot, fixedNow())
	if err != nil {
		t.Fatalf("pre-provision CA: %v", err)
	}

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:        fixedNow(),
		Mode:       agentproxy.InterceptionModeRequired,
		CAApproval: "deadbeefdeadbeef",
		RepoRoot:   repoRoot,
		EnvOptIn:   true,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if evidence.Enabled || !evidence.Denied {
		t.Fatalf("expected denied on approval mismatch, got %#v", evidence)
	}
}
