// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultHookCommandPrefersExplicitValue(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	got := defaultHookCommand("/custom/coding-ethos-run agent-hook")
	if got != "/custom/coding-ethos-run agent-hook" {
		t.Fatalf("defaultHookCommand explicit = %q", got)
	}
}

func TestDefaultHookCommandUsesRuntimeEnvironment(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	got := defaultHookCommand("")
	if got != "/repo/bin/coding-ethos-run agent-hook" {
		t.Fatalf("defaultHookCommand env = %q", got)
	}
}

func TestDefaultHookCommandReturnsEmptyWhenUnset(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "")

	got := defaultHookCommand(" ")
	if got != "" {
		t.Fatalf("defaultHookCommand unset = %q", got)
	}
}

func TestWriteJSONReportFormatsPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := writeJSONReport(file, map[string]string{"status": "valid"}); err != nil {
		t.Fatalf("writeJSONReport returned error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close report: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !bytes.Contains(data, []byte("\"status\": \"valid\"")) {
		t.Fatalf("report JSON = %s", data)
	}
}

func TestPrintSyncDoctorVerifySettingsCommands(t *testing.T) {
	root := t.TempDir()
	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	if err := printSettings([]string{"--hook-command", hookCommand}); err != nil {
		t.Fatalf("printSettings returned error: %v", err)
	}
	if err := syncSettings([]string{"--root", root, "--hook-command", hookCommand}); err != nil {
		t.Fatalf("syncSettings returned error: %v", err)
	}
	if err := doctorSettings([]string{"--root", root, "--hook-command", hookCommand}); err != nil {
		t.Fatalf("doctorSettings returned error: %v", err)
	}
	if err := verifySettings([]string{"--root", root, "--hook-command", hookCommand}); err == nil ||
		!strings.Contains(err.Error(), "verify agent hook settings") {
		t.Fatalf("verifySettings error = %v", err)
	}
}

func TestRunCLIDispatchesAgentHookCommands(t *testing.T) {
	root := t.TempDir()
	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	if code := runCLI([]string{"sync", "--root", root, "--hook-command", hookCommand}); code != 0 {
		t.Fatalf("sync exit code = %d, want 0", code)
	}
	if code := runCLI([]string{"doctor", "--root", root, "--hook-command", hookCommand}); code != 0 {
		t.Fatalf("doctor exit code = %d, want 0", code)
	}
	if code := runCLI([]string{"verify", "--root", root, "--hook-command", hookCommand}); code != 1 {
		t.Fatalf("verify exit code = %d, want 1 for missing executable probe", code)
	}
}

func TestRunCLIReturnsUsageAndCommandErrors(t *testing.T) {
	if code := runCLI(nil); code != commandArgsOffset {
		t.Fatalf("empty args exit code = %d, want %d", code, commandArgsOffset)
	}
	if code := runCLI([]string{"missing"}); code != 1 {
		t.Fatalf("unknown command exit code = %d, want 1", code)
	}
	if code := runCLI([]string{"sync", "--root"}); code != 1 {
		t.Fatalf("invalid flags exit code = %d, want 1", code)
	}
}

func TestUsageWritesCommandSummary(t *testing.T) {
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writeFile
	defer func() {
		os.Stderr = original
	}()
	usage()
	if err := writeFile.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	buffer := make([]byte, 256)
	n, readErr := readFile.Read(buffer)
	if readErr != nil {
		t.Fatalf("read usage pipe: %v", readErr)
	}
	if !strings.Contains(string(buffer[:n]), "coding-ethos-agent-hooks") {
		t.Fatalf("usage output = %q", string(buffer[:n]))
	}
}
