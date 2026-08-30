// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestSyncAndStatusCommandsEmitTypedV2Receipts(t *testing.T) {
	root := t.TempDir()
	runSourceV2CLIGit(t, root, "init", "--quiet")
	runSourceV2CLIGit(t, root, "config", "user.email", "test@example.test")
	runSourceV2CLIGit(t, root, "config", "user.name", "Code Intel CLI Test")
	if err := os.WriteFile(
		filepath.Join(root, "app.go"),
		[]byte("package app\n\nfunc Current() {}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runSourceV2CLIGit(t, root, "add", "app.go")
	runSourceV2CLIGit(
		t,
		root,
		"-c",
		"commit.gpgSign=false",
		"commit",
		"--no-gpg-sign",
		"-m",
		"base",
	)

	var runErr error
	syncOutput := captureStdout(t, func() {
		runErr = run(context.Background(), []string{"sync", "--root", root})
	})
	if runErr != nil {
		t.Fatalf("sync command: %v", runErr)
	}

	var syncReceipt codeintel.SourceSyncReceipt
	if err := json.Unmarshal([]byte(syncOutput), &syncReceipt); err != nil {
		t.Fatalf("decode sync receipt: %v\n%s", err, syncOutput)
	}
	if syncReceipt.Contract != codeintel.SourceV2Contract ||
		syncReceipt.Kind != "coding-ethos.code-intel.sync/v2" ||
		syncReceipt.SourceReadiness.Status != codeintel.SourceStatusExact ||
		syncReceipt.Repair != "coding-ethos-code-intel sync --root ." {
		t.Fatalf("sync receipt = %#v", syncReceipt)
	}

	statusOutput := captureStdout(t, func() {
		runErr = run(context.Background(), []string{"status", "--root", root})
	})
	if runErr != nil {
		t.Fatalf("status command: %v", runErr)
	}

	var statusReceipt codeintel.SourceStatusReceipt
	if err := json.Unmarshal([]byte(statusOutput), &statusReceipt); err != nil {
		t.Fatalf("decode status receipt: %v\n%s", err, statusOutput)
	}
	if statusReceipt.Contract != codeintel.SourceV2Contract ||
		statusReceipt.Kind != "coding-ethos.code-intel.status/v2" ||
		statusReceipt.SourceReadiness.Identity.GenerationID !=
			syncReceipt.SourceReadiness.Identity.GenerationID {
		t.Fatalf("status receipt = %#v", statusReceipt)
	}
}

func TestRebuildIndexIsDeprecatedAliasForRebuildDerived(t *testing.T) {
	root := t.TempDir()
	duckDBPath := filepath.Join(root, ".coding-ethos", "derived.duckdb")

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{
			"rebuild-index",
			"--root", root,
			"--duckdb", duckDBPath,
		})
	})
	if runErr != nil {
		t.Fatalf("deprecated rebuild alias: %v", runErr)
	}

	var receipt rebuildDerivedReceipt
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatalf("decode rebuild receipt: %v\n%s", err, output)
	}
	if receipt.Contract != codeintel.SourceV2Contract ||
		receipt.DeprecatedCommand != "rebuild-index" ||
		receipt.Replacement != "rebuild-derived" {
		t.Fatalf("rebuild alias receipt = %#v", receipt)
	}
}

func runSourceV2CLIGit(t *testing.T, root string, arguments ...string) {
	t.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
