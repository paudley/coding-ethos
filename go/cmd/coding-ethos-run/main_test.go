// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"
)

func TestPolicyToolLintArgsPropagateSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{
		PolicyBundle:  "/repo/build/policy/policy-bundle.json",
		EthosRoot:     "/repo/coding-ethos",
		Root:          "/repo",
		InvocationCWD: "/repo/pkg",
	}, "ruff", []string{"check", "pkg"})

	for _, want := range []string{
		"--bundle",
		"/repo/build/policy/policy-bundle.json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		"/repo/coding-ethos",
		"--consumer-root",
		"/repo",
		"--invocation-cwd",
		"/repo/pkg",
		"--sandbox-mode",
		"required",
		"--",
		"check",
		"pkg",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("policyToolLintArgs() missing %q: %#v", want, args)
		}
	}
	if slices.Index(args, "--sandbox-mode") > slices.Index(args, "--") {
		t.Fatalf("sandbox flag must precede tool args separator: %#v", args)
	}
}

func TestPolicyToolLintArgsOmitBlankSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", " ")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("blank sandbox mode should not be forwarded: %#v", args)
	}
}

func TestPolicyToolLintArgsIgnoreAmbientSandboxModeOutsideShim(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("ambient sandbox mode should not affect direct policy-tool calls: %#v", args)
	}
}
