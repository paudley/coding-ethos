// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"os"
	"testing"
)

// TestMain clears ambient agent-provider environment variables before running
// the hooks test suite. Event.Provider() falls back to providerFromEnvironment,
// which reads these variables, so a suite executed inside an agent runtime
// (Claude Code, Codex, Gemini) would otherwise observe a non-neutral provider
// and break tests that assert behavior for an unset provider. Clearing them
// once keeps the suite hermetic regardless of where it runs; tests that need a
// specific provider set it explicitly via Event.ProviderHint or t.Setenv.
func TestMain(m *testing.M) {
	for _, name := range []string{
		"CLAUDECODE",
		"CLAUDE_CODE_ENTRYPOINT",
		"CODEX_THREAD_ID",
		"CODEX_CI",
		"CODEX_MANAGED_BY_NPM",
		"GEMINI_CLI",
	} {
		if err := os.Unsetenv(name); err != nil {
			panic("unset ambient provider env for hooks tests: " + err.Error())
		}
	}

	os.Exit(m.Run())
}
