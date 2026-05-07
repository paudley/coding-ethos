// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestRunWithIORequiresCommandRootAndBundleRoot(t *testing.T) {
	t.Parallel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

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
	t.Parallel()
	testlock.ProcessState(t, "coding-ethos-hook-log")

	originalArgs := os.Args

	t.Cleanup(func() {
		os.Args = originalArgs
	})

	os.Args = []string{"coding-ethos-hook-log"}

	err := run()
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunWithIOExecutesAndCapturesHookOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	bundleRoot := filepath.Join(root, "pre-commit")

	inlineErr0 := os.MkdirAll(bundleRoot, 0o755)
	if inlineErr0 != nil {
		t.Fatalf("create bundle root: %v", inlineErr0)
	}

	gitPath := filepath.Join(root, "git")

	inlineErr1 := os.WriteFile(
		gitPath,
		[]byte("#!/usr/bin/env sh\nexit 0\n"),
		0o600,
	)
	if inlineErr1 != nil {
		t.Fatalf("write fake git: %v", inlineErr1)
	}

	inlineErr1 = os.Chmod(gitPath, 0o700)
	if inlineErr1 != nil {
		t.Fatalf("chmod fake git: %v", inlineErr1)
	}

	commandPath := filepath.Join(root, "hook")

	inlineErr2 := os.WriteFile(
		commandPath,
		[]byte(
			"#!/usr/bin/env sh\nprintf 'stdout text\\n'\nprintf 'stderr text\\n' >&2\n",
		),
		0o600,
	)
	if inlineErr2 != nil {
		t.Fatalf("write hook command: %v", inlineErr2)
	}

	inlineErr2 = os.Chmod(commandPath, 0o700)
	if inlineErr2 != nil {
		t.Fatalf("chmod hook command: %v", inlineErr2)
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

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

	matches, err := filepath.Glob(
		filepath.Join(root, ".coding-ethos", "hook-runs", "*", "metadata.env"),
	)
	if err != nil {
		t.Fatalf("glob metadata: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("metadata matches = %#v", matches)
	}
}

func TestRunWithIOPreservesWrappedCommandExitCode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundleRoot := filepath.Join(root, "pre-commit")

	err := os.MkdirAll(bundleRoot, 0o755)
	if err != nil {
		t.Fatalf("create bundle root: %v", err)
	}

	gitPath := filepath.Join(root, "git")

	err = os.WriteFile(gitPath, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o600)
	if err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	err = os.Chmod(gitPath, 0o700)
	if err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}

	commandPath := filepath.Join(root, "hook")

	err = os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\nexit 37\n"), 0o600)
	if err != nil {
		t.Fatalf("write hook command: %v", err)
	}

	err = os.Chmod(commandPath, 0o700)
	if err != nil {
		t.Fatalf("chmod hook command: %v", err)
	}

	err = runWithIO(
		[]string{
			"--root", root,
			"--bundle-root", bundleRoot,
			"--git", gitPath,
			commandPath,
		},
		strings.NewReader(""),
		&bytes.Buffer{},
		&bytes.Buffer{},
	)

	var exitErr exitCoder
	if !errors.As(err, &exitErr) {
		t.Fatalf("runWithIO() error does not expose exit code: %v", err)
	}

	if exitErr.ExitCode() != 37 {
		t.Fatalf("exit code = %d, want 37", exitErr.ExitCode())
	}
}
