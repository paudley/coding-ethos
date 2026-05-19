// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package debuglog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestConfigureDisabledSuppressesDebugOutput(t *testing.T) {
	t.Cleanup(Reset)

	runDir := t.TempDir()
	var stderr bytes.Buffer

	err := Configure(false, runDir, &stderr)
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	Debug("debuglog.test.disabled")

	if stderr.Len() != 0 {
		t.Fatalf("stderr was written while debug disabled: %q", stderr.String())
	}

	_, statErr := os.Stat(filepath.Join(runDir, debugLog))
	if !os.IsNotExist(statErr) {
		t.Fatalf("debug log should be absent while disabled: %v", statErr)
	}
}

func TestConfigureEnabledWritesJSONToRunDirAndStderr(t *testing.T) {
	t.Cleanup(Reset)

	runDir := t.TempDir()
	var stderr bytes.Buffer

	err := Configure(true, runDir, &stderr)
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	Debug("debuglog.test.enabled", zap.String("key", "value"))

	err = Sync()
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	for name, payload := range map[string]string{
		"stderr": stderr.String(),
		"file":   readDebugLog(t, runDir),
	} {
		for _, want := range []string{
			`"level":"debug"`,
			`"event":"debuglog.test.enabled"`,
			`"key":"value"`,
		} {
			if !strings.Contains(payload, want) {
				t.Fatalf("%s debug payload missing %q:\n%s", name, want, payload)
			}
		}
	}
}

func TestConfigureReturnsOpenError(t *testing.T) {
	t.Cleanup(Reset)

	err := Configure(true, filepath.Join(t.TempDir(), "missing"), nil)
	if err == nil || !strings.Contains(err.Error(), "open debug log") {
		t.Fatalf("Configure() error = %v, want open debug log", err)
	}
}

func TestEnabledFromEnvRequiresExplicitValue(t *testing.T) {
	t.Setenv(EnvName, debugValue)
	if !EnabledFromEnv() {
		t.Fatal("EnabledFromEnv() = false with debug value")
	}

	t.Setenv(EnvName, "true")
	if EnabledFromEnv() {
		t.Fatal("EnabledFromEnv() = true for non-debug value")
	}
}

func TestProcessEnterExitLogTimingAndTokenEstimate(t *testing.T) {
	t.Cleanup(Reset)

	runDir := t.TempDir()
	var stderr bytes.Buffer

	err := Configure(true, runDir, &stderr)
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	argv := []string{"make", "check"}
	startedAt := ProcessEnter(argv, "/repo")
	ProcessExit(startedAt, argv, "/repo", 7, os.ErrNotExist)

	err = Sync()
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	payload := stderr.String()
	for _, want := range []string{
		`"event":"process.exec.enter"`,
		`"event":"process.exec.exit"`,
		`"argv":["make","check"]`,
		`"cwd":"/repo"`,
		`"exit_code":7`,
		`"argv_token_estimate":3`,
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("process debug payload missing %q:\n%s", want, payload)
		}
	}
}

func TestEstimateTokensRoundsUpByFourRunes(t *testing.T) {
	t.Parallel()

	tests := map[string]int{
		"":      0,
		"abc":   1,
		"abcd":  1,
		"abcde": 2,
	}

	for input, want := range tests {
		if got := EstimateTokens(input); got != want {
			t.Fatalf("EstimateTokens(%q) = %d, want %d", input, got, want)
		}
	}
}

func readDebugLog(t *testing.T, runDir string) string {
	t.Helper()

	payload, err := os.ReadFile(filepath.Join(runDir, debugLog))
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}

	return string(payload)
}
