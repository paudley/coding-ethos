// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestValidatedConfigSectionsRejectsUnknownTopLevelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(
		path,
		[]byte("style: {}\nwrong_section: {}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := validatedConfigSections(path, nil)
	if err == nil {
		t.Fatal("expected unknown top-level section error")
	}

	if !strings.Contains(err.Error(), `wrong_section`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidatedConfigSectionsSortsKnownTopLevelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(
		path,
		[]byte("python: {}\nstyle: {}\nhooks: {}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, sections, err := validatedConfigSections(path, nil)
	if err != nil {
		t.Fatalf("validate config sections: %v", err)
	}

	want := strings.Join([]string{"hooks", "python", "style"}, ",")
	if strings.Join(sections, ",") != want {
		t.Fatalf("sections = %#v, want %s", sections, want)
	}
}

func TestValidateRepoConfigSectionsRejectsNestedTypos(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	if err := os.WriteFile(
		configPath,
		[]byte("python:\n  comment_suppressions:\n    enabled: true\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(
		repoConfigPath,
		[]byte("python:\n  comment_supressions:\n    enabled: false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	configShape, _, err := validatedConfigSections(configPath, nil)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	_, err = validateRepoConfigSections(repoConfigPath, configShape)
	if err == nil {
		t.Fatal("expected nested typo error")
	}

	if !strings.Contains(err.Error(), "python.comment_supressions") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRepoConfigSectionsAllowsRepoLicenseOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	if err := os.WriteFile(
		configPath,
		[]byte("style:\n  line_length: 100\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(
		repoConfigPath,
		[]byte("repo:\n  license:\n    spdx_identifier: MIT\n    copyright: Example Inc.\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	configShape, _, err := validatedConfigSections(configPath, nil)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, configShape)
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "repo" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateMetadataCommandChecksPolicySources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "config.yaml")
	metadataPath := filepath.Join(dir, "policy-metadata.json")

	if err := os.WriteFile(sourcePath, []byte("style: {}\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	metadata := policy.Metadata{
		SourceHashes: map[string]string{
			sourcePath: "sha256:99faa993bc5910bf699657cd8af777791cd11bf48267e1bdb68fa6f6e9181921",
		},
		BundleHash:  "sha256:bundle",
		GeneratedAt: "2026-05-01T00:00:00Z",
	}

	file, err := os.Create(metadataPath)
	if err != nil {
		t.Fatalf("create metadata: %v", err)
	}

	if err := policy.EncodeMetadata(file, metadata); err != nil {
		t.Fatalf("encode metadata: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}

	if err := validateMetadata([]string{"--metadata", metadataPath}); err != nil {
		t.Fatalf("validate metadata: %v", err)
	}

	if err := os.WriteFile(
		sourcePath,
		[]byte("style:\n  line_length: 88\n"),
		0o600,
	); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}

	err = validateMetadata([]string{"--metadata", metadataPath})
	if err == nil || !strings.Contains(err.Error(), "policy source hash mismatch") {
		t.Fatalf("validate stale metadata error = %v", err)
	}
}

func TestPolicyArtifactCommandsRoundTripExampleBundle(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "policy")
	captureStdout(t, func() {
		err := writeExample([]string{"--out-dir", outDir})
		if err != nil {
			t.Fatalf("write example: %v", err)
		}
	})

	for _, name := range []string{
		"policy-bundle.json",
		"policy-metadata.json",
		"policy-summary.md",
	} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}

	bundlePath := filepath.Join(outDir, "policy-bundle.json")

	validateOutput := captureStdout(t, func() {
		err := validate([]string{"--bundle", bundlePath})
		if err != nil {
			t.Fatalf("validate bundle: %v", err)
		}
	})
	if !strings.Contains(validateOutput, "policy bundle valid") {
		t.Fatalf("validate output = %q", validateOutput)
	}

	explainOutput := captureStdout(t, func() {
		err := explain([]string{"--bundle", bundlePath, "git.hook_bypass"})
		if err != nil {
			t.Fatalf("explain policy: %v", err)
		}
	})
	if !strings.Contains(explainOutput, "git.hook_bypass") {
		t.Fatalf("explain output missing policy id:\n%s", explainOutput)
	}
}

func TestRunCLIDispatchesPolicyCommands(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "policy")

	writeOutput := captureStdout(t, func() {
		if code := runCLI([]string{"write-example", "--out-dir", outDir}); code != 0 {
			t.Fatalf("write-example exit = %d", code)
		}
	})
	if !strings.Contains(writeOutput, "wrote policy artifacts") {
		t.Fatalf("write-example output = %q", writeOutput)
	}

	bundlePath := filepath.Join(outDir, "policy-bundle.json")

	validateOutput := captureStdout(t, func() {
		if code := runCLI([]string{"validate", "--bundle", bundlePath}); code != 0 {
			t.Fatalf("validate exit = %d", code)
		}
	})
	if !strings.Contains(validateOutput, "policy bundle valid") {
		t.Fatalf("validate output = %q", validateOutput)
	}

	explainOutput := captureStdout(t, func() {
		if code := runCLI(
			[]string{"explain", "--bundle", bundlePath, "git.hook_bypass"},
		); code != 0 {
			t.Fatalf("explain exit = %d", code)
		}
	})
	if !strings.Contains(explainOutput, "git.hook_bypass") {
		t.Fatalf("explain output = %q", explainOutput)
	}

	dumpOutput := captureStdout(t, func() {
		if code := runCLI([]string{"dump-example"}); code != 0 {
			t.Fatalf("dump-example exit = %d", code)
		}
	})
	if !strings.Contains(dumpOutput, "git.hook_bypass") {
		t.Fatalf("dump-example output = %q", dumpOutput)
	}
}

func TestRunCLIReturnsUsageAndCommandErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "missing command",
			args: nil,
			want: commandArgsOffset,
		},
		{
			name: "unknown command",
			args: []string{"unknown"},
			want: commandArgsOffset,
		},
		{
			name: "command error",
			args: []string{"validate"},
			want: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if code := runCLI(test.args); code != test.want {
				t.Fatalf("exit = %d, want %d", code, test.want)
			}
		})
	}
}

func TestPolicyCommandRequiredFlagErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want error
		run  func() error
		name string
	}{
		{
			name: "compile out dir",
			run:  func() error { return compile(nil) },
			want: errCompileOutDirRequired,
		},
		{
			name: "write example out dir",
			run:  func() error { return writeExample(nil) },
			want: errWriteExampleOutDirRequired,
		},
		{
			name: "validate bundle",
			run:  func() error { return validate(nil) },
			want: errValidateBundleRequired,
		},
		{
			name: "validate metadata",
			run:  func() error { return validateMetadata(nil) },
			want: errValidateMetadataRequired,
		},
		{
			name: "explain bundle",
			run:  func() error { return explain(nil) },
			want: errExplainBundleRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.run()
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConfigTraceReportsConfigAndRepoSections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "coding_ethos.yml")
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	if err := os.WriteFile(primaryPath, []byte("principles: []\n"), 0o600); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	if err := os.WriteFile(
		configPath,
		[]byte("style:\n  line_length: 100\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(
		repoConfigPath,
		[]byte("repo:\n  license:\n    spdx_identifier: MIT\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	err := configTrace([]string{
		"--primary", primaryPath,
		"--config", configPath,
		"--repo-config", repoConfigPath,
	})
	if err == nil || !strings.Contains(err.Error(), "compile policy bundle") {
		t.Fatalf("configTrace should validate sections before compile failure, got %v", err)
	}

	sections, err := validateRepoConfigSections(
		repoConfigPath,
		map[string]any{"style": map[string]any{"line_length": 0}},
	)
	if err != nil {
		t.Fatalf("validate repo config sections: %v", err)
	}

	if strings.Join(sections, ",") != "repo" {
		t.Fatalf("sections = %#v", sections)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	original := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	os.Stdout = writer

	defer func() {
		os.Stdout = original
	}()

	run()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return buffer.String()
}
