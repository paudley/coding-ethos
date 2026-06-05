// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policycli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

const repoConfigSection = "repo"

func TestValidatedConfigSectionsRejectsUnknownTopLevelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")

	inlineErr0 := os.WriteFile(
		path,
		[]byte("style: {}\nwrong_section: {}\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write config: %v", inlineErr0)
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

	inlineErr1 := os.WriteFile(
		path,
		[]byte("python: {}\nstyle: {}\nhooks: {}\n"),
		0o600,
	)
	if inlineErr1 != nil {
		t.Fatalf("write config: %v", inlineErr1)
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

	inlineErr2 := os.WriteFile(
		configPath,
		[]byte("python:\n  comment_suppressions:\n    enabled: true\n"),
		0o600,
	)
	if inlineErr2 != nil {
		t.Fatalf("write config: %v", inlineErr2)
	}

	inlineErr3 := os.WriteFile(
		repoConfigPath,
		[]byte("python:\n  comment_supressions:\n    enabled: false\n"),
		0o600,
	)
	if inlineErr3 != nil {
		t.Fatalf("write repo config: %v", inlineErr3)
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

	inlineErr4 := os.WriteFile(
		configPath,
		[]byte("style:\n  line_length: 100\n"),
		0o600,
	)
	if inlineErr4 != nil {
		t.Fatalf("write config: %v", inlineErr4)
	}

	inlineErr5 := os.WriteFile(
		repoConfigPath,
		[]byte(
			"repo:\n  license:\n    spdx_identifier: AGPL-3.0-only\n    copyright: Example Inc.\n",
		),
		0o600,
	)
	if inlineErr5 != nil {
		t.Fatalf("write repo config: %v", inlineErr5)
	}

	configShape, _, err := validatedConfigSections(configPath, nil)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, configShape)
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != repoConfigSection {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsAllowsRepoProfiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte("repo:\n  kind: go-static-site\nprofiles:\n  - generated-site-output\n"),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "profiles,repo" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsAllowsCodeIntelOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte("code_intel:\n  exclude_paths:\n    - \"**/dist/**\"\n"),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "code_intel" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsRejectsUnknownCodeIntelKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte("code_intel:\n  exlude_paths:\n    - \"**/dist/**\"\n"),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	_, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err == nil {
		t.Fatal("expected unknown code_intel key error")
	}

	if !strings.Contains(err.Error(), "code_intel.exlude_paths") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRepoConfigSectionsAllowsProxyOutputCompression(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte(
			"proxy:\n"+
				"  output_compression:\n"+
				"    max_lines: 40\n"+
				"    head_lines: 10\n"+
				"    tail_lines: 10\n"+
				"    max_tokens: 1000\n"+
				"    head_tokens: 400\n"+
				"    tail_tokens: 400\n"+
				"    max_diagnostics: 8\n",
		),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "proxy" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsAllowsProxyCodeIntelEnrichment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte(
			"proxy:\n"+
				"  code_intel_enrichment:\n"+
				"    enabled: true\n"+
				"    max_paths: 3\n"+
				"    max_symbols: 6\n"+
				"    max_edges: 4\n"+
				"    max_failures: 4\n",
		),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "proxy" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsAllowsProxyInterception(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte(
			"proxy:\n"+
				"  interception:\n"+
				"    mode: required\n"+
				"    ca_approval: abc123\n"+
				"    allow_hosts:\n"+
				"      - api.anthropic.com\n"+
				"    max_normalize_bytes: 1048576\n"+
				"    on_error: fail_closed\n",
		),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	sections, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}

	if strings.Join(sections, ",") != "proxy" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateRepoConfigSectionsRejectsUnknownProxyInterceptionKey(
	t *testing.T,
) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte("proxy:\n  interception:\n    modez: required\n"),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	_, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err == nil {
		t.Fatal("expected unknown proxy interception key error")
	}

	if !strings.Contains(err.Error(), "proxy.interception.modez") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRepoConfigSectionsRejectsUnknownProxyOutputKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")

	inlineErr := os.WriteFile(
		repoConfigPath,
		[]byte("proxy:\n  output_compression:\n    max_tokenz: 1000\n"),
		0o600,
	)
	if inlineErr != nil {
		t.Fatalf("write repo config: %v", inlineErr)
	}

	_, err := validateRepoConfigSections(repoConfigPath, map[string]any{})
	if err == nil {
		t.Fatal("expected unknown proxy output key error")
	}

	if !strings.Contains(err.Error(), "proxy.output_compression.max_tokenz") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRepoConfigSectionsRejectsInvariantGitPolicyToggles(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"hook bypass":            "git:\n  hook_bypass:\n    enabled: false\n",
		"history rewrite":        "git:\n  history_rewrite_prevention:\n    enabled: false\n",
		"stash blocked":          "git:\n  stash_blocked:\n    enabled: false\n",
		"protected submodule":    "git:\n  protected_submodule_update:\n    enabled: false\n",
		"signed operations":      "git:\n  signed_operations:\n    enabled: false\n",
		"protected work guard":   "repo:\n  protected_branch_work:\n    enabled: false\n",
		"protected branch write": "filesystem:\n  protected_branch_write:\n    enabled: false\n",
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			repoConfigPath := filepath.Join(dir, "repo_config.yaml")

			inlineErr := os.WriteFile(repoConfigPath, []byte(payload), 0o600)
			if inlineErr != nil {
				t.Fatalf("write repo config: %v", inlineErr)
			}

			_, err := validateRepoConfigSections(repoConfigPath, map[string]any{
				"filesystem": map[string]any{},
				"git":        map[string]any{},
				"repo":       map[string]any{},
			})
			if err == nil {
				t.Fatal("expected invariant git policy toggle rejection")
			}

			if !strings.Contains(err.Error(), "unknown config path") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateMetadataCommandChecksPolicySources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "config.yaml")
	metadataPath := filepath.Join(dir, "policy-metadata.json")

	inlineErr6 := os.WriteFile(sourcePath, []byte("style: {}\n"), 0o600)
	if inlineErr6 != nil {
		t.Fatalf("write source: %v", inlineErr6)
	}

	metadata := policy.Metadata{
		SourceHashes: map[string]string{
			sourcePath: "sha256:" +
				"99faa993bc5910bf699657cd8af777791cd11bf48267e1bdb68fa6f6e9181921",
		},
		BundleHash:  "sha256:bundle",
		GeneratedAt: "2026-05-01T00:00:00Z",
	}

	file, err := os.Create(metadataPath)
	if err != nil {
		t.Fatalf("create metadata: %v", err)
	}

	inlineErr7 := policy.EncodeMetadata(file, metadata)
	if inlineErr7 != nil {
		t.Fatalf("encode metadata: %v", inlineErr7)
	}

	inlineErr8 := file.Close()
	if inlineErr8 != nil {
		t.Fatalf("close metadata: %v", inlineErr8)
	}

	inlineErr9 := validateMetadata([]string{"--metadata", metadataPath})
	if inlineErr9 != nil {
		t.Fatalf("validate metadata: %v", inlineErr9)
	}

	inlineErr10 := os.WriteFile(
		sourcePath,
		[]byte("style:\n  line_length: 88\n"),
		0o600,
	)
	if inlineErr10 != nil {
		t.Fatalf("rewrite source: %v", inlineErr10)
	}

	err = validateMetadata([]string{"--metadata", metadataPath})
	if err == nil || !strings.Contains(err.Error(), "policy source hash mismatch") {
		t.Fatalf("validate stale metadata error = %v", err)
	}
}

func TestPolicyArtifactCommandsRoundTripExampleBundle(t *testing.T) {
	t.Parallel()

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
		_, inlineErrA := os.Stat(filepath.Join(outDir, name))
		if inlineErrA != nil {
			t.Fatalf("missing artifact %s: %v", name, inlineErrA)
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
	t.Parallel()

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

func TestExportedRunReturnsUsageCode(t *testing.T) {
	t.Parallel()

	if code := Run(nil); code != commandArgsOffset {
		t.Fatalf("Run(nil) exit = %d, want %d", code, commandArgsOffset)
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

func TestConfigTraceWritersEmitTextAndJSON(t *testing.T) {
	report := configTraceReport{
		Config:             "config.yaml",
		RepoConfig:         "repo_config.yaml",
		Status:             "valid",
		ConfigSections:     []string{"repo", "style"},
		RepoConfigSections: []string{"repo"},
		DispatchScopes:     1,
		EvidenceMaps:       2,
		Policies:           3,
	}

	textOutput := captureStdout(t, func() {
		writeConfigTraceText(report)
	})
	if !strings.Contains(textOutput, "status: valid") ||
		!strings.Contains(textOutput, "repo_config_sections: repo") {
		t.Fatalf("config trace text missing fields:\n%s", textOutput)
	}

	jsonOutput := captureStdout(t, func() {
		if err := writeConfigTraceJSON(report); err != nil {
			t.Fatalf("write JSON config trace: %v", err)
		}
	})
	if !strings.Contains(jsonOutput, `"status": "valid"`) ||
		!strings.Contains(jsonOutput, `"dispatch_scopes": 1`) {
		t.Fatalf("config trace JSON missing fields:\n%s", jsonOutput)
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

	inlineErr11 := os.WriteFile(primaryPath, []byte("principles: []\n"), 0o600)
	if inlineErr11 != nil {
		t.Fatalf("write primary: %v", inlineErr11)
	}

	inlineErr12 := os.WriteFile(
		configPath,
		[]byte("style:\n  line_length: 100\n"),
		0o600,
	)
	if inlineErr12 != nil {
		t.Fatalf("write config: %v", inlineErr12)
	}

	inlineErr13 := os.WriteFile(
		repoConfigPath,
		[]byte("repo:\n  license:\n    spdx_identifier: AGPL-3.0-only\n"),
		0o600,
	)
	if inlineErr13 != nil {
		t.Fatalf("write repo config: %v", inlineErr13)
	}

	err := configTrace([]string{
		"--primary", primaryPath,
		"--config", configPath,
		"--repo-config", repoConfigPath,
	})
	if err == nil || !strings.Contains(err.Error(), "compile policy bundle") {
		t.Fatalf(
			"configTrace should validate sections before compile failure, got %v",
			err,
		)
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

	release := testlock.ProcessStateScope(t, "coding-ethos-policy")
	defer release()

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

	inlineErr14 := writer.Close()
	if inlineErr14 != nil {
		t.Fatalf("close stdout writer: %v", inlineErr14)
	}

	var buffer bytes.Buffer

	_, inlineErrB := io.Copy(&buffer, reader)
	if inlineErrB != nil {
		t.Fatalf("read stdout: %v", inlineErrB)
	}

	inlineErr15 := reader.Close()
	if inlineErr15 != nil {
		t.Fatalf("close stdout reader: %v", inlineErr15)
	}

	return buffer.String()
}
