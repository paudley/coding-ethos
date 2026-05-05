// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func TestGitWrapperRoutingHelpersRewriteAndBlockEvasiveShell(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	rewritten, rewrite, ok := rewriteGitCommandChain("git status --short && echo ok")
	if !ok || !rewrite {
		t.Fatalf("rewriteGitCommandChain did not rewrite: %q %v %v", rewritten, rewrite, ok)
	}
	if !strings.Contains(rewritten, "'/repo/bin/coding-ethos-run' policy-git 'status' '--short'") {
		t.Fatalf("rewritten command = %q", rewritten)
	}

	if !managedGitCommandChain("/repo/bin/coding-ethos-run policy-git status") {
		t.Fatal("managed git command chain was not recognized")
	}
	if !commandReferencesUnmanagedGit("command git status") {
		t.Fatal("command wrapper did not reference unmanaged git")
	}
	for _, command := range []string{
		"bash -c 'git status'",
		"python -c 'import subprocess; subprocess.run([\"git\", \"status\"])'",
		"PATH=/tmp:$PATH git status",
		"command git status",
	} {
		if !evasiveGitShell(command) {
			t.Fatalf("evasiveGitShell(%q) = false", command)
		}
	}
}

func TestLintToolRoutingHelpersRewriteAndBlockEvasiveShell(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	rewritten, tool, rewrite, ok := rewriteLintToolCommandChain("python -m ruff check pkg 2>&1")
	if !ok || !rewrite || tool.Name != "ruff" {
		t.Fatalf("rewrite lint = %q %#v %v %v", rewritten, tool, rewrite, ok)
	}
	if !strings.Contains(rewritten, "'/repo/bin/coding-ethos-run' policy-tool ruff 'check' 'pkg' 2>&1") {
		t.Fatalf("rewritten lint command = %q", rewritten)
	}

	for _, command := range []string{
		"bash -c 'ruff check pkg'",
		"python -c 'import subprocess; subprocess.run([\"ruff\", \"check\"])'",
		"PATH=/tmp:$PATH ruff check pkg",
		"eval 'mypy pkg'",
	} {
		if !evasiveLintToolShell(command) {
			t.Fatalf("evasiveLintToolShell(%q) = false", command)
		}
	}
	if firstMentionedCapturedTool("uv run mypy pkg").Name != "mypy" {
		t.Fatal("first mentioned captured tool should detect mypy")
	}
	if lintCapturePolicyID("golangci-lint") != "tool.golangci_lint_capture_required" {
		t.Fatalf("lint capture policy id mismatch")
	}
}

func TestRoutingEvidenceAndBlockDecisionHelpers(t *testing.T) {
	commands, err := shellparse.Commands("git status --short > out.txt")
	if err != nil {
		t.Fatalf("parse shell command: %v", err)
	}
	if !shellCommandIsGit(commands[0]) {
		t.Fatal("shell command should be recognized as git")
	}
	if !shellCommandArgReferencesTool(commands[0], "git") {
		t.Fatal("git command args should reference git tool")
	}
	args, redirects := splitShellRedirections([]string{"status", "--short", ">", "out.txt"})
	if strings.Join(args, " ") != "status --short" || len(redirects) != 1 || redirects[0] != "> 'out.txt'" {
		t.Fatalf("split redirects args=%#v redirects=%#v", args, redirects)
	}

	bundle := policy.Bundle{Policies: map[string]policy.Policy{}}
	decision := gitWrapperBlockDecision(bundle, "blocked")
	if decision.PolicyID != gitWrapperPolicyID || decision.Decision != modeBlock {
		t.Fatalf("block decision = %#v", decision)
	}
}
