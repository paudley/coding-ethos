// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "testing"

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
