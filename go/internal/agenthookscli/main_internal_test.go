// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthookscli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/syncstate"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestDefaultHookCommandPrefersExplicitValue(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	got := defaultHookCommand("/custom/coding-ethos-run agent-hook")
	if got != "/custom/coding-ethos-run agent-hook" {
		t.Fatalf("defaultHookCommand explicit = %q", got)
	}
}

func TestDefaultHookCommandUsesRuntimeEnvironment(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	got := defaultHookCommand("")
	if got != "/repo/bin/coding-ethos-run agent-hook" {
		t.Fatalf("defaultHookCommand env = %q", got)
	}
}

func TestDefaultHookCommandReturnsEmptyWhenUnset(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "")

	got := defaultHookCommand(" ")
	if got != "" {
		t.Fatalf("defaultHookCommand unset = %q", got)
	}
}

func TestWriteJSONReportFormatsPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.json")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}

	inlineErr0 := writeJSONReport(file, map[string]string{"status": "valid"})
	if inlineErr0 != nil {
		t.Fatalf("writeJSONReport returned error: %v", inlineErr0)
	}

	inlineErr1 := file.Close()
	if inlineErr1 != nil {
		t.Fatalf("close report: %v", inlineErr1)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}

	if !bytes.Contains(data, []byte("\"status\": \"valid\"")) {
		t.Fatalf("report JSON = %s", data)
	}
}

func TestCapabilitiesReportsRuntimeContractAndKimi(t *testing.T) {
	ethosRoot := t.TempDir()
	writeAgentHooksCLITestFile(
		t,
		filepath.Join(ethosRoot, "pyproject.toml"),
		"[project]\nversion = \"7.8.9\"\n",
	)

	var err error
	output := captureStdout(t, func() {
		err = capabilities([]string{"--json", "--ethos-root", ethosRoot})
	})
	if err != nil {
		t.Fatalf("capabilities returned error: %v", err)
	}

	for _, expected := range []string{
		`"api_version": "coding-ethos.agent-hooks/v1"`,
		`"runtime_version": "7.8.9"`,
		`"contract_version": "coding-ethos.hook/v1"`,
		`"selector": "neutral-v1"`,
		`"state_root_flag": "--state-root"`,
		`"mcp_command_flag": "--mcp-command"`,
		`"hook_timeout_flag": "--hook-timeout-seconds"`,
		`"runtime_policy_command": "runtime-policy"`,
		`"provider": "kimi"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("capability output missing %q:\n%s", expected, output)
		}
	}
}

func TestPrintSyncDoctorVerifySettingsCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))

	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	err := printSettings([]string{"--hook-command", hookCommand})
	if err != nil {
		t.Fatalf("printSettings returned error: %v", err)
	}

	err = syncSettings([]string{"--root", root, "--hook-command", hookCommand})
	if err != nil {
		t.Fatalf("syncSettings returned error: %v", err)
	}

	err = doctorSettings([]string{"--root", root, "--hook-command", hookCommand})
	if err != nil {
		t.Fatalf("doctorSettings returned error: %v", err)
	}

	err = verifySettings([]string{"--root", root, "--hook-command", hookCommand})
	if err == nil ||
		!strings.Contains(err.Error(), "verify agent hook settings") {
		t.Fatalf("verifySettings error = %v", err)
	}
}

func TestSyncSettingsDryRunDoesNotWriteSettingsOrState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))

	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	var err error
	captureStdout(t, func() {
		err = syncSettings([]string{
			"--root", root,
			"--hook-command", hookCommand,
			"--dry-run",
			"--format", "json",
		})
	})
	if err != nil {
		t.Fatalf("syncSettings dry-run returned error: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".claude", "settings.local.json"),
		filepath.Join(root, ".mcp.json"),
		filepath.Join(root, ".codex", "config.toml"),
		filepath.Join(root, ".gemini", "settings.json"),
		filepath.Join(root, ".kimi-code", "config.toml"),
		filepath.Join(root, ".kimi-code", "mcp.json"),
		syncstate.FilePath(root),
	} {
		if _, statErr := os.Stat(path); statErr == nil {
			t.Fatalf("dry-run wrote %s", path)
		}
	}
}

func TestSyncSettingsUsesEthosRootForInstallState(t *testing.T) {
	root := t.TempDir()
	ethosRoot := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))

	writeAgentHooksCLITestFile(
		t,
		filepath.Join(ethosRoot, "pyproject.toml"),
		"[project]\nversion = \"7.8.9\"\n",
	)

	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	err := syncSettings([]string{
		"--root", root,
		"--ethos-root", ethosRoot,
		"--hook-command", hookCommand,
	})
	if err != nil {
		t.Fatalf("syncSettings returned error: %v", err)
	}

	state, err := syncstate.Read(root)
	if err != nil {
		t.Fatalf("read install sync state: %v", err)
	}
	if state.RuntimeVersion != "7.8.9" {
		t.Fatalf("runtime version = %q", state.RuntimeVersion)
	}
}

func TestSyncAndDoctorSettingsAcceptPrivateOverlayRepoRoot(t *testing.T) {
	settingsRoot := t.TempDir()
	repoRoot := t.TempDir()
	globalCodexHome := filepath.Join(t.TempDir(), "must-remain-absent")
	t.Setenv("CODEX_HOME", globalCodexHome)

	hookCommand := filepath.Join(settingsRoot, "bin", "coding-ethos-run") +
		" agent-hook"

	err := syncSettings([]string{
		"--root", settingsRoot,
		"--repo-root", repoRoot,
		"--hook-command", hookCommand,
	})
	if err != nil {
		t.Fatalf("syncSettings overlay returned error: %v", err)
	}

	err = doctorSettings([]string{
		"--root", settingsRoot,
		"--repo-root", repoRoot,
		"--hook-command", hookCommand,
	})
	if err != nil {
		t.Fatalf("doctorSettings overlay returned error: %v", err)
	}

	codexConfig := filepath.Join(settingsRoot, ".codex", "config.toml")
	configPayload, err := os.ReadFile(codexConfig)
	if err != nil {
		t.Fatalf("read private Codex overlay config: %v", err)
	}
	if !strings.Contains(string(configPayload), "[hooks.state.") {
		t.Fatalf("private Codex overlay lacks hook trust state:\n%s", configPayload)
	}
	if _, statErr := os.Stat(
		filepath.Join(globalCodexHome, "config.toml"),
	); !os.IsNotExist(
		statErr,
	) {
		t.Fatalf("private overlay mutated global Codex config: %v", statErr)
	}

	for _, path := range []string{
		filepath.Join(repoRoot, ".claude", "settings.local.json"),
		filepath.Join(repoRoot, ".mcp.json"),
		filepath.Join(repoRoot, ".codex", "config.toml"),
		filepath.Join(repoRoot, ".gemini", "settings.json"),
		filepath.Join(repoRoot, ".kimi-code", "config.toml"),
		filepath.Join(repoRoot, ".kimi-code", "mcp.json"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("overlay CLI wrote target-repo setting %s", path)
		}
	}
}

func TestSyncAndDoctorSettingsAcceptExternalHookAndMCPCommands(t *testing.T) {
	settingsRoot := t.TempDir()
	repoRoot := t.TempDir()
	stateRoot := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "must-remain-absent"))

	hookCommand := "env NYAR_HOME=/private/nyar " +
		"NYAR_CODING_ETHOS_ROOT=/opt/coding-ethos /opt/nyar hook"
	mcpCommand := "/opt/coding-ethos/bin/coding-ethos-run mcp"

	err := syncSettings([]string{
		"--root", settingsRoot,
		"--repo-root", repoRoot,
		"--state-root", stateRoot,
		"--hook-command", hookCommand,
		"--mcp-command", mcpCommand,
	})
	if err != nil {
		t.Fatalf("syncSettings external wrapper returned error: %v", err)
	}

	err = doctorSettings([]string{
		"--root", settingsRoot,
		"--repo-root", repoRoot,
		"--state-root", stateRoot,
		"--hook-command", hookCommand,
		"--mcp-command", mcpCommand,
	})
	if err != nil {
		t.Fatalf("doctorSettings external wrapper returned error: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(settingsRoot)
	for _, expectation := range []struct {
		path string
		text string
	}{
		{path: paths.Claude, text: hookCommand},
		{path: paths.CodexConfig, text: hookCommand},
		{path: paths.KimiConfig, text: hookCommand + " --provider kimi"},
		{path: paths.ClaudeMCP, text: "/opt/coding-ethos/bin/coding-ethos-run"},
		{path: paths.CodexConfig, text: "/opt/coding-ethos/bin/coding-ethos-run"},
		{path: paths.KimiMCP, text: "/opt/coding-ethos/bin/coding-ethos-run"},
		{path: paths.ClaudeMCP, text: repoRoot},
		{path: paths.ClaudeMCP, text: stateRoot},
		{path: paths.CodexConfig, text: repoRoot},
		{path: paths.CodexConfig, text: stateRoot},
		{path: paths.KimiMCP, text: repoRoot},
		{path: paths.KimiMCP, text: stateRoot},
	} {
		payload, readErr := os.ReadFile(expectation.path)
		if readErr != nil {
			t.Fatalf("read %s: %v", expectation.path, readErr)
		}
		if !strings.Contains(string(payload), expectation.text) {
			t.Fatalf("%s missing %q:\n%s", expectation.path, expectation.text, payload)
		}
	}

	if _, statErr := os.Stat(
		filepath.Join(stateRoot, ".coding-ethos", "memories", "MEMORY.md"),
	); statErr != nil {
		t.Fatalf("private state root lacks centralized memory: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".coding-ethos")); !os.IsNotExist(
		statErr,
	) {
		t.Fatalf("repository root gained durable supervisor state: %v", statErr)
	}
}

func TestRunCLIDispatchesAgentHookCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))

	hookCommand := filepath.Join(root, "bin", "coding-ethos-run") + " agent-hook"

	if code := runCLI(
		[]string{"sync", "--root", root, "--hook-command", hookCommand},
	); code != 0 {
		t.Fatalf("sync exit code = %d, want 0", code)
	}

	if code := runCLI(
		[]string{"doctor", "--root", root, "--hook-command", hookCommand},
	); code != 0 {
		t.Fatalf("doctor exit code = %d, want 0", code)
	}

	if code := runCLI(
		[]string{"verify", "--root", root, "--hook-command", hookCommand},
	); code != 1 {
		t.Fatalf("verify exit code = %d, want 1 for missing executable probe", code)
	}

	if code := runCLI(
		[]string{"sync-provider-matrix", "--root", root},
	); code != 0 {
		t.Fatalf("sync-provider-matrix exit code = %d, want 0", code)
	}

	if code := runCLI(
		[]string{"check-provider-matrix", "--root", root},
	); code != 0 {
		t.Fatalf("check-provider-matrix exit code = %d, want 0", code)
	}
}

func writeAgentHooksCLITestFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	release := testlock.ProcessStateScope(t, "coding-ethos-agent-hooks")
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

	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}

	var buffer bytes.Buffer

	_, err = io.Copy(&buffer, reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	closeErr = reader.Close()
	if closeErr != nil {
		t.Fatalf("close stdout reader: %v", closeErr)
	}

	return buffer.String()
}

func TestRunCLIReturnsUsageAndCommandErrors(t *testing.T) {
	t.Parallel()

	if code := runCLI(nil); code != commandArgsOffset {
		t.Fatalf("empty args exit code = %d, want %d", code, commandArgsOffset)
	}

	if code := runCLI([]string{"missing"}); code != 1 {
		t.Fatalf("unknown command exit code = %d, want 1", code)
	}

	if code := runCLI([]string{"sync", "--root"}); code != 1 {
		t.Fatalf("invalid flags exit code = %d, want 1", code)
	}

	if code := runCLI([]string{"check-provider-matrix", "--root"}); code != 1 {
		t.Fatalf("invalid provider matrix flags exit code = %d, want 1", code)
	}
}

func TestUsageWritesCommandSummary(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	usageTo(&output)

	if !strings.Contains(output.String(), "coding-ethos-agent-hooks") {
		t.Fatalf("usage output = %q", output.String())
	}
}
