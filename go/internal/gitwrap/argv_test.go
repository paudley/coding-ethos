// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestParseArgvSkipsGitGlobalOptions(t *testing.T) {
	t.Parallel()

	parsed := ParseArgv(
		[]string{"-C", "/repo", "-c", "commit.gpgsign=false", "commit", "-m", "test"},
	)
	if parsed.Operation != "commit" {
		t.Fatalf("operation mismatch: got %q", parsed.Operation)
	}

	if parsed.Argv[0] != "git" {
		t.Fatalf("argv was not normalized: %#v", parsed.Argv)
	}
}

func TestParseArgvAllowsGlobalPolicyForStatus(t *testing.T) {
	t.Parallel()

	bundle := policyBundleWithChangeDirPolicy(t)

	result, err := Check(bundle, Options{Argv: []string{"-C", "/repo", "status"}})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if result.Operation != "status" {
		t.Fatalf("operation mismatch: got %q", result.Operation)
	}
}

func policyBundleWithChangeDirPolicy(t *testing.T) policy.Bundle {
	t.Helper()

	bundle := policy.ExampleBundle()
	bundle.Policies["git.change_dir_flag"] = policy.Policy{
		ID:       "git.change_dir_flag",
		Category: "git",
		Source: policy.SourceRef{
			File: "config.yaml",
			Path: "git.change_dir_flag",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "git -C hides working directory context.",
		DefenseLayers:   policy.GitDefenseLayers("block", "wrapper", "block", "", ""),
		Evaluators: []policy.Evaluator{
			{
				Kind: "cel",
				Name: "cel.expression",
				Options: map[string]any{
					"mode":     "block",
					"skill_id": "agent-operating-discipline",
					"when":     `git_command.is_git && git_command.has_change_dir`,
				},
			},
		},
	}

	bundle.Dispatch.Git["*"] = policy.GitOperationDispatch{
		Pre: []string{"git.change_dir_flag"},
	}

	err := bundle.Validate()
	if err != nil {
		t.Fatalf("validate bundle: %v", err)
	}

	return bundle
}
