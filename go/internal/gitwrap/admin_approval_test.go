// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package gitwrap_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
)

func TestIsCodingEthosRepoRequiresCompleteMarkerSet(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "coding-ethos")

	err := os.MkdirAll(filepath.Join(root, "go"), 0o700)
	if err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	for _, marker := range []string{
		"coding_ethos.yml",
		"config.yaml",
		"go/go.mod",
	} {
		writeErr := os.WriteFile(filepath.Join(root, marker), []byte("x\n"), 0o600)
		if writeErr != nil {
			t.Fatalf("write marker %s: %v", marker, writeErr)
		}
	}

	if IsCodingEthosRepo(root) {
		t.Fatal("repo without runner marker should not be admin-approved")
	}

	bin := filepath.Join(root, "bin")

	err = os.MkdirAll(bin, 0o700)
	if err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	runnerPath := filepath.Join(bin, "coding-ethos-run")

	err = os.WriteFile(
		runnerPath,
		[]byte("#!/bin/sh\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write runner marker: %v", err)
	}

	err = os.Chmod(runnerPath, 0o700)
	if err != nil {
		t.Fatalf("chmod runner marker: %v", err)
	}

	nested := filepath.Join(root, "go", "internal")
	if !IsCodingEthosRepo(nested) {
		t.Fatalf("nested path should resolve coding-ethos root")
	}
}

func TestReadApprovedPIDsParsesBlankLinesAndRejectsBadRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := filepath.Join(dir, "pids")

	inlineErr0 := os.WriteFile(path, []byte("\n 123 \n456\n"), 0o600)
	if inlineErr0 != nil {
		t.Fatalf("write pid file: %v", inlineErr0)
	}

	approved, err := ReadApprovedPIDs(path)
	if err != nil {
		t.Fatalf("read approved pids: %v", err)
	}

	if !approved[123] || !approved[456] {
		t.Fatalf("approved pids = %#v", approved)
	}

	badPath := filepath.Join(dir, "bad-pids")

	inlineErr1 := os.WriteFile(badPath, []byte("not-a-pid\n"), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write bad pid file: %v", inlineErr1)
	}

	_, err = ReadApprovedPIDs(badPath)
	if err == nil || !strings.Contains(err.Error(), "parse admin pid") {
		t.Fatalf("bad pid error = %v", err)
	}
}

func TestProcessAncestryApprovedMatchesCurrentPID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pids")

	inlineErr2 := os.WriteFile(path, []byte("999999\n"), 0o600)
	if inlineErr2 != nil {
		t.Fatalf("write pid file: %v", inlineErr2)
	}

	approved, err := ProcessAncestryApproved(os.Getpid(), path)
	if err != nil {
		t.Fatalf("check ancestry: %v", err)
	}

	if approved {
		t.Fatal("unlisted process should not be approved")
	}

	inlineErr3 := os.WriteFile(
		path,
		[]byte(strconv.Itoa(os.Getpid())+"\n"),
		0o600,
	)
	if inlineErr3 != nil {
		t.Fatalf("rewrite pid file: %v", inlineErr3)
	}

	approved, err = ProcessAncestryApproved(os.Getpid(), path)
	if err != nil {
		t.Fatalf("check approved ancestry: %v", err)
	}

	if !approved {
		t.Fatal("current process should be approved")
	}
}

func TestProcessAncestryContainsCurrentPID(t *testing.T) {
	t.Parallel()

	contains, err := ProcessAncestryContains(os.Getpid(), os.Getpid())
	if err != nil {
		t.Fatalf("check ancestry contains current pid: %v", err)
	}

	if !contains {
		t.Fatal("current pid should contain itself in ancestry")
	}
}

func TestProcessCommandLineReadsCurrentProcess(t *testing.T) {
	t.Parallel()

	commandLine, err := ProcessCommandLine(os.Getpid())
	if err != nil {
		t.Fatalf("read process command line: %v", err)
	}

	if len(commandLine) == 0 || strings.TrimSpace(commandLine[0]) == "" {
		t.Fatalf("empty command line: %#v", commandLine)
	}
}
