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

func TestRunWithIORequiresCommandRootAndBundleRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runWithIO(nil, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("runWithIO(nil) error = %v", err)
	}

	err = runWithIO([]string{"echo"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "root is required") {
		t.Fatalf("runWithIO(no root) error = %v", err)
	}

	err = runWithIO(
		[]string{"--root", t.TempDir(), "echo"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil || !strings.Contains(err.Error(), "bundle root is required") {
		t.Fatalf("runWithIO(no bundle root) error = %v", err)
	}
}

func TestRunUsesProcessArgs(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"coding-ethos-hook-log"}

	err := run()
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunWithIOExecutesAndCapturesHookOutput(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "pre-commit")
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("create bundle root: %v", err)
	}
	gitPath := filepath.Join(root, "git")
	if err := os.WriteFile(
		gitPath,
		[]byte("#!/usr/bin/env sh\nexit 0\n"),
		0o755,
	); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	commandPath := filepath.Join(root, "hook")
	if err := os.WriteFile(
		commandPath,
		[]byte("#!/usr/bin/env sh\nprintf 'stdout text\\n'\nprintf 'stderr text\\n' >&2\n"),
		0o755,
	); err != nil {
		t.Fatalf("write hook command: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"--root", root,
			"--bundle-root", bundleRoot,
			"--git", gitPath,
			commandPath,
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("runWithIO() returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "stdout text") ||
		!strings.Contains(stderr.String(), "stderr text") {
		t.Fatalf("captured stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(root, ".coding-ethos", "hook-runs", "*", "metadata.env"))
	if err != nil {
		t.Fatalf("glob metadata: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("metadata matches = %#v", matches)
	}
}
