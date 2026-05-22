// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputcli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown output command") {
		t.Fatalf("unknown command error = %v", err)
	}
}

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	output := captureStderr(t, func() {
		if err := Run(context.Background(), nil); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	})
	if !strings.Contains(output, "Usage: coding-ethos-run output") ||
		!strings.Contains(output, "report") ||
		!strings.Contains(output, "prune") {
		t.Fatalf("usage output missing commands:\n%s", output)
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

func TestReportWritesHumanOutputWithConfiguredTempInclude(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.report]\ninclude_temp = true\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"report",
			"--root",
			root,
			"--format",
			"human",
		})
		if err != nil {
			t.Fatalf("run report: %v", err)
		}
	})
	if !strings.Contains(output, "coding-ethos output surface report") ||
		!strings.Contains(output, "- proxy_temp_evidence:") {
		t.Fatalf("human report missing temp surface:\n%s", output)
	}
}

func TestReportWritesDefaultTOONOutput(t *testing.T) {
	root := t.TempDir()

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{"report", "--root", root})
		if err != nil {
			t.Fatalf("run report: %v", err)
		}
	})
	if !strings.Contains(output, "format: toon") ||
		!strings.Contains(output, "surfaces[") {
		t.Fatalf("default report output was not TOON:\n%s", output)
	}
}

func TestPruneRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{
		"prune",
		"--root",
		t.TempDir(),
		"--format",
		"xml",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported output prune format") {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func TestPruneRejectsInvalidOlderThan(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{
		"prune",
		"--root",
		t.TempDir(),
		"--older-than",
		"eventually",
	})
	if err == nil || !strings.Contains(err.Error(), "parse --older-than") {
		t.Fatalf("invalid older-than error = %v", err)
	}
}

func TestPruneDryRunWritesJSONReport(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		t.Fatalf("create lint trace dir: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write lint trace: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.prune.surfaces.lint_traces]\nrequire_code_intel_ingest = false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"prune",
			"--root",
			root,
			"--scope",
			" lint_traces ",
			"--older-than",
			"24h",
			"--format",
			"json",
		})
		if err != nil {
			t.Fatalf("run prune: %v", err)
		}
	})

	if !strings.Contains(output, `"candidates"`) ||
		!strings.Contains(output, `"surface_id": "lint_traces"`) {
		t.Fatalf("JSON prune report missing lint trace candidate:\n%s", output)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("dry-run deleted trace: %v", err)
	}
}

func TestPruneDryRunWritesDefaultTOONReport(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		t.Fatalf("create lint trace dir: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write lint trace: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.prune.surfaces.lint_traces]\nrequire_code_intel_ingest = false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"prune",
			"--root",
			root,
			"--scope",
			"lint_traces",
			"--older-than",
			"24h",
		})
		if err != nil {
			t.Fatalf("run prune: %v", err)
		}
	})

	if !strings.Contains(output, "format: toon") ||
		!strings.Contains(output, "candidates[1]") ||
		!strings.Contains(output, "lint_traces") {
		t.Fatalf("default prune output was not TOON:\n%s", output)
	}
}

func TestPruneUsesConfiguredDefaultFormat(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		t.Fatalf("create lint trace dir: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write lint trace: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte(
			"[outputs.report]\ndefault_format = \"json\"\n\n"+
				"[outputs.prune.surfaces.lint_traces]\nrequire_code_intel_ingest = false\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"prune",
			"--root",
			root,
			"--scope",
			"lint_traces",
			"--older-than",
			"24h",
		})
		if err != nil {
			t.Fatalf("run prune: %v", err)
		}
	})

	if !strings.HasPrefix(strings.TrimSpace(output), "{") ||
		!strings.Contains(output, `"surface_id": "lint_traces"`) {
		t.Fatalf("prune output did not use configured JSON default:\n%s", output)
	}
}

func TestPruneApplyWritesHumanReport(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		t.Fatalf("create lint trace dir: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write lint trace: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.prune.surfaces.lint_traces]\nrequire_code_intel_ingest = false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"prune",
			"--root",
			root,
			"--scope",
			"lint_traces",
			"--older-than",
			"24h",
			"--apply",
			"--format",
			"human",
		})
		if err != nil {
			t.Fatalf("run prune apply: %v", err)
		}
	})

	if !strings.Contains(output, "mode: apply") ||
		!strings.Contains(output, "- lint_traces deleted") {
		t.Fatalf("human prune report missing apply details:\n%s", output)
	}
	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Fatalf("apply did not delete trace: %v", err)
	}
}

func TestPruneDryRunOverridesApply(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
		t.Fatalf("create lint trace dir: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write lint trace: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[outputs.prune.surfaces.lint_traces]\nrequire_code_intel_ingest = false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo_config.toml: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"prune",
			"--root",
			root,
			"--scope",
			"lint_traces",
			"--older-than",
			"24h",
			"--apply",
			"--dry-run",
			"--format",
			"json",
		})
		if err != nil {
			t.Fatalf("run prune dry-run: %v", err)
		}
	})

	if !strings.Contains(output, `"apply": false`) {
		t.Fatalf("dry-run did not override apply:\n%s", output)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("dry-run deleted trace: %v", err)
	}
}

func TestSplitScopesPreservesConfiguredTokens(t *testing.T) {
	t.Parallel()

	got := splitScopes(" lint_traces ,hook_runs")
	want := []string{" lint_traces ", "hook_runs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitScopes() = %#v, want %#v", got, want)
	}
	if got := splitScopes(" \t "); got != nil {
		t.Fatalf("blank splitScopes() = %#v, want nil", got)
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

func captureStderr(t *testing.T, action func()) string {
	t.Helper()

	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = original
	}()

	action()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}

	return string(payload)
}
