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

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunWithIORequiresBundle(t *testing.T) {
	var stdout bytes.Buffer

	err := runWithIO(nil, strings.NewReader(""), &stdout)
	if !errors.Is(err, errBundleRequired) {
		t.Fatalf("runWithIO(nil) error = %v, want %v", err, errBundleRequired)
	}
}

func TestRunUsesProcessArgs(t *testing.T) {
	originalArgs := os.Args

	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = []string{"coding-ethos-mcp"}

	err := run()
	if !errors.Is(err, errBundleRequired) {
		t.Fatalf("run() error = %v, want %v", err, errBundleRequired)
	}
}

func TestRunWithIOHandlesInitializeRequest(t *testing.T) {
	bundlePath := writeMCPTestBundle(t)

	var stdout bytes.Buffer

	err := runWithIO(
		[]string{
			"--bundle", bundlePath,
			"--ethos-root", "/ethos",
			"--consumer-root", "/repo",
			"--invocation-cwd", "/repo/pkg",
			"--lint-binary", "/bin/coding-ethos-lint",
		},
		strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`+"\n",
		),
		&stdout,
	)
	if err != nil {
		t.Fatalf("runWithIO() returned error: %v", err)
	}

	for _, want := range []string{
		`"jsonrpc":"2.0"`,
		`"id":1`,
		`"protocolVersion":"2024-11-05"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("MCP output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestReadBundleRejectsMissingFile(t *testing.T) {
	_, err := readBundle(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "open bundle") {
		t.Fatalf("readBundle(missing) error = %v", err)
	}
}

func writeMCPTestBundle(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "policy-bundle.json")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	if err := policy.EncodeBundle(file, policy.ExampleBundle()); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	return path
}
