// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooklog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWritesHookLogsAndMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	git := fakeGit(t)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	err := Run(Options{
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		GitPath:    git,
		Root:       root,
		BundleRoot: filepath.Join(root, "pre-commit"),
		Command:    commandThatPrints(t),
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("run hook log: %v", err)
	}

	runDirs, err := os.ReadDir(filepath.Join(root, ".coding-ethos", "hook-runs"))
	if err != nil {
		t.Fatalf("read hook runs: %v", err)
	}

	if len(runDirs) != 1 {
		t.Fatalf("hook run dirs = %d, want 1", len(runDirs))
	}

	runDir := filepath.Join(root, ".coding-ethos", "hook-runs", runDirs[0].Name())
	assertFileContains(t, filepath.Join(runDir, "stdout.log"), "hello stdout")
	assertFileContains(t, filepath.Join(runDir, "stderr.log"), "hello stderr")
	assertFileContains(
		t,
		filepath.Join(runDir, "metadata.env"),
		"started_at_utc='20260501T123456Z'",
	)
	assertFileContains(t, filepath.Join(runDir, "metadata.env"), "exit_code='0'")

	if !strings.Contains(stdout.String(), "hello stdout") {
		t.Fatalf("stdout was not mirrored: %q", stdout.String())
	}

	if !strings.Contains(stderr.String(), "hello stderr") {
		t.Fatalf("stderr was not mirrored: %q", stderr.String())
	}
}

func fakeGit(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "git")

	script := "#!/usr/bin/env bash\nexit 0\n"

	err := os.WriteFile(path, []byte(script), 0o755)
	if err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	return path
}

func commandThatPrints(t *testing.T) []string {
	t.Helper()

	return []string{os.Args[0], "-test.run=TestHelperProcess", "--", "print"}
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if !strings.Contains(string(content), substring) {
		t.Fatalf("%s does not contain %q:\n%s", path, substring, string(content))
	}
}

func TestHelperProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-1] != "print" {
		return
	}

	t.Log("helper process")
	os.Stdout.WriteString("hello stdout\n")
	os.Stderr.WriteString("hello stderr\n")
	os.Exit(0)
}
