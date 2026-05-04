// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	if err := run(context.Background(), []string{"unknown"}); err == nil {
		t.Fatalf("expected unknown command error")
	}
}

func TestStatsCreatesStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	err := run(context.Background(), []string{"stats", "--root", root, "--db", dbPath})
	if err != nil {
		t.Fatalf("stats command returned error: %v", err)
	}
}
