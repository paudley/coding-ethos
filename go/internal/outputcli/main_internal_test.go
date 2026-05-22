// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputcli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown output command") {
		t.Fatalf("unknown command error = %v", err)
	}
}

func TestReportRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{
		"report",
		"--root",
		t.TempDir(),
		"--format",
		"xml",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported output report format") {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func TestReportUsesConfiguredDefaultFormat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.report]\ndefault_format = \"json\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"report", "--root", root})
		if err != nil {
			t.Fatalf("run report: %v", err)
		}
	})
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("report output did not use configured JSON default: %q", output)
	}
}

func captureStdout(t *testing.T, action func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	action()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return string(payload)
}
