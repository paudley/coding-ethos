// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e

import (
	"slices"
	"testing"
)

func TestCommandEnvironmentDropsInheritedRuntimeContext(t *testing.T) {
	t.Setenv("CODE_ETHOS_CONSUMER_ROOT", "/outer/repo")
	t.Setenv("CODE_ETHOS_HOOK_RUN_DIR", "/outer/hook-run")
	t.Setenv("CODE_ETHOS_STATE_ROOT", "/outer/state")
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")

	env := commandEnvironmentWith(t, map[string]string{
		"CODE_ETHOS_CONSUMER_ROOT": "/fixture/repo",
	})

	if !slices.Contains(env, "CODE_ETHOS_CONSUMER_ROOT=/fixture/repo") {
		t.Fatalf("explicit fixture root missing from environment: %#v", env)
	}
	if !slices.Contains(env, "CODING_ETHOS_AGENT_SHELL_SANDBOX=1") {
		t.Fatalf("outer sandbox provenance missing from environment: %#v", env)
	}
	if slices.Contains(env, "CODE_ETHOS_CONSUMER_ROOT=/outer/repo") ||
		slices.Contains(env, "CODE_ETHOS_HOOK_RUN_DIR=/outer/hook-run") ||
		slices.Contains(env, "CODE_ETHOS_STATE_ROOT=/outer/state") {
		t.Fatalf("inherited runtime context leaked into fixture: %#v", env)
	}
}
