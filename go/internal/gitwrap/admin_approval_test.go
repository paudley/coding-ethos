// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
		err := os.WriteFile(filepath.Join(root, marker), []byte("x\n"), 0o600)
		if err != nil {
			t.Fatalf("write marker %s: %v", marker, err)
		}
	}

	if isCodingEthosRepo(root) {
		t.Fatal("repo without runner marker should not be admin-approved")
	}

	bin := filepath.Join(root, "bin")

	err = os.MkdirAll(bin, 0o700)
	if err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(bin, "coding-ethos-run"),
		[]byte("#!/bin/sh\n"),
		0o700,
	)
	if err != nil {
		t.Fatalf("write runner marker: %v", err)
	}

	nested := filepath.Join(root, "go", "internal")
	if !isCodingEthosRepo(nested) {
		t.Fatalf("nested path should resolve coding-ethos root")
	}
}

func TestReadApprovedPIDsParsesBlankLinesAndRejectsBadRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := filepath.Join(dir, "pids")
	if err := os.WriteFile(path, []byte("\n 123 \n456\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	approved, err := readApprovedPIDs(path)
	if err != nil {
		t.Fatalf("read approved pids: %v", err)
	}

	if !approved[123] || !approved[456] {
		t.Fatalf("approved pids = %#v", approved)
	}

	badPath := filepath.Join(dir, "bad-pids")
	if err := os.WriteFile(badPath, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("write bad pid file: %v", err)
	}

	_, err = readApprovedPIDs(badPath)
	if err == nil || !strings.Contains(err.Error(), "parse admin pid") {
		t.Fatalf("bad pid error = %v", err)
	}
}

func TestProcessAncestryApprovedMatchesCurrentPID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pids")
	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	approved, err := processAncestryApproved(os.Getpid(), path)
	if err != nil {
		t.Fatalf("check ancestry: %v", err)
	}

	if approved {
		t.Fatal("unlisted process should not be approved")
	}

	if err := os.WriteFile(
		path,
		[]byte(strconv.Itoa(os.Getpid())+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("rewrite pid file: %v", err)
	}

	approved, err = processAncestryApproved(os.Getpid(), path)
	if err != nil {
		t.Fatalf("check approved ancestry: %v", err)
	}

	if !approved {
		t.Fatal("current process should be approved")
	}
}
