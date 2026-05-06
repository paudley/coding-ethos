// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestGitSafetyInternalParsesCommitMessagesAndGlobalArgs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	messageFile := filepath.Join(repo, "message.txt")
	if err := os.WriteFile(
		messageFile,
		[]byte("fix(core): from file\n\nbody\n"),
		0o600,
	); err != nil {
		t.Fatalf("write message file: %v", err)
	}

	messages, err := commitMessagesFromArgv(
		[]string{
			"GIT_DIR=.git",
			"git",
			"-C",
			repo,
			"commit",
			"-m",
			"fix(core): subject",
			"-msecond paragraph",
		},
		repo,
		nil,
	)
	if err != nil {
		t.Fatalf("commit messages from argv: %v", err)
	}

	if len(messages) != 1 || !strings.Contains(messages[0], "second paragraph") {
		t.Fatalf("messages = %#v", messages)
	}

	fileMessages, err := commitMessagesFromArgv(
		[]string{"git", "commit", "-F", "message.txt"},
		repo,
		nil,
	)
	if err != nil {
		t.Fatalf("commit message file: %v", err)
	}

	if len(fileMessages) != 1 || !strings.Contains(fileMessages[0], "from file") {
		t.Fatalf("file messages = %#v", fileMessages)
	}

	stdinMessages, err := commitMessagesFromArgv(
		[]string{"git", "commit", "--file=-"},
		repo,
		[]byte("fix(stdin): ok\n"),
	)
	if err != nil {
		t.Fatalf("stdin commit message: %v", err)
	}

	if len(stdinMessages) != 1 || stdinMessages[0] != "fix(stdin): ok\n" {
		t.Fatalf("stdin messages = %#v", stdinMessages)
	}
}

func TestGitSafetyInternalBranchAndFlagHelpers(t *testing.T) {
	t.Parallel()

	if got := gitSubcommand(
		[]string{"git", "-C", "/repo", "-c", "core.quotePath=false", "checkout", "main"},
	); got != "checkout" {
		t.Fatalf("gitSubcommand() = %q", got)
	}

	if !hasCleanForceDelete([]string{"git", "clean", "-fd"}) {
		t.Fatal("combined clean force/delete flags were not detected")
	}

	if !hasForcePush([]string{"git", "push", "--force-with-lease", "origin", "main"}) {
		t.Fatal("force-with-lease was not detected")
	}

	if !hasProtectedBranchArg([]string{"git", "push", "origin", "main"}) {
		t.Fatal("protected branch arg was not detected")
	}

	if !isTheirsOrOurs("theirs") || isTheirsOrOurs("ourselves") {
		t.Fatal("merge shortcut helper misclassified strategy")
	}

	checkoutTargets := protectedCheckoutTargets([]string{"git", "checkout", "origin/main"})
	if !slices.Contains(checkoutTargets, "origin/main") {
		t.Fatalf("checkout targets = %#v", checkoutTargets)
	}

	switchTargets := protectedCheckoutTargets([]string{"git", "switch", "main"})
	if !slices.Contains(switchTargets, "main") {
		t.Fatalf("switch targets = %#v", switchTargets)
	}

	if protectedCheckoutTargets([]string{"git", "status"}) != nil {
		t.Fatal("non-checkout command should not have protected checkout targets")
	}
}

func TestLineLimitHelpersApplyOnlyToCodeGrowthPolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ext   string
		file  string
		lines int64
		want  bool
	}{
		{name: "python over limit", ext: ".py", file: "pkg/app.py", lines: 1001, want: true},
		{name: "python at limit", ext: ".py", file: "pkg/app.py", lines: 1000},
		{name: "go over limit", ext: ".go", file: "pkg/app.go", lines: 2001, want: true},
		{name: "shell over limit", ext: ".sh", file: "tools/hook.sh", lines: 501, want: true},
		{
			name:  "scripts path over limit",
			ext:   ".txt",
			file:  "scripts/generated",
			lines: 501,
			want:  true,
		},
		{name: "yaml excluded", ext: ".yml", file: ".github/workflows/ci.yml", lines: 5000},
		{name: "sql excluded", ext: ".sql", file: "queries/report.sql", lines: 5000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := fileExceedsLineLimit(test.ext, test.file, test.lines); got != test.want {
				t.Fatalf(
					"fileExceedsLineLimit(%q, %q, %d) = %v, want %v",
					test.ext,
					test.file,
					test.lines,
					got,
					test.want,
				)
			}
		})
	}
}

func TestGitBranchTargetHelpersHandleCreationFlags(t *testing.T) {
	t.Parallel()

	checkoutTargets := checkoutBranchTargets([]string{
		"-b", "feature-a", "origin/main",
		"--orphan", "new-root",
		"--", "literal",
	})
	if strings.Join(checkoutTargets, ",") != "feature-a,new-root,literal" {
		t.Fatalf("checkout targets = %#v", checkoutTargets)
	}

	switchTargets := switchBranchTargets([]string{
		"--create", "feature-b", "origin/main",
		"--force-create", "feature-c",
		"--discard-changes",
		"existing",
	})
	if strings.Join(switchTargets, ",") != "feature-b,feature-c,existing" {
		t.Fatalf("switch targets = %#v", switchTargets)
	}
}

func TestShellMalformedAndEvidenceHelpers(t *testing.T) {
	t.Parallel()

	policyDef := policy.Policy{ID: "shell.malformed", DefaultSeverity: "block"}

	decisions, err := EvaluateShellMalformedCommand(policyDef, Context{Command: "if then"})
	if err != nil {
		t.Fatalf("malformed command evaluation: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("malformed decisions = %#v", decisions)
	}

	decisions = blockShellDecision(policyDef, "git status --short")
	if len(decisions) != 1 {
		t.Fatalf("block shell decisions = %#v", decisions)
	}

	if _, ok := decisions[0].Evidence["shell_commands"]; !ok {
		t.Fatalf("parsed shell command evidence missing: %#v", decisions[0].Evidence)
	}
}

func TestFileSyntaxValidatorsCoverJSONAndTOML(t *testing.T) {
	t.Parallel()

	err := validateJSONSyntax([]byte(`{"ok": true}`))
	if err != nil {
		t.Fatalf("valid json: %v", err)
	}

	err = validateJSONSyntax([]byte(`{"bad":`))
	if err == nil {
		t.Fatal("invalid json should fail")
	}

	err = validateTOMLSyntax([]byte("name = \"coding-ethos\"\n"))
	if err != nil {
		t.Fatalf("valid toml: %v", err)
	}

	err = validateTOMLSyntax([]byte("name = \n"))
	if err == nil {
		t.Fatal("invalid toml should fail")
	}
}

func TestRegistryRequireReportsMissingEvaluator(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if _, err := registry.Require("missing.evaluator"); err == nil {
		t.Fatal("missing evaluator should return an error")
	}

	registry = DefaultRegistry()
	if _, err := registry.Require("cel.expression"); err != nil {
		t.Fatalf("registered evaluator should resolve: %v", err)
	}
}
