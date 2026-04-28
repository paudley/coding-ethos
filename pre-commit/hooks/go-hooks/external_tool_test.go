// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"
)

func TestExternalToolEnvRemovesGitHookLocalEnvironment(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/wrong-git-dir")
	t.Setenv("GIT_INDEX_FILE", "/tmp/wrong-index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "test@example.com")
	t.Setenv(consumerRootEnv, "/tmp/repo")
	t.Setenv(hookGroupChildEnv, hookPlanBoolTrue)
	t.Setenv(hookGroupResultPathEnv, "/tmp/result.json")

	env := externalToolEnv([]string{"KEEP_EXTRA=1"})

	for _, item := range env {
		name, _, found := strings.Cut(item, "=")
		if found && externalToolEnvBlocked(name+"=value") {
			t.Fatalf("externalToolEnv leaked %s in %#v", name, env)
		}
	}

	if !slices.Contains(env, "KEEP_EXTRA=1") {
		t.Fatalf("externalToolEnv dropped explicit extra env: %#v", env)
	}
}
