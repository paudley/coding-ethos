// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const semanticPolicyInjectionHeaderLines = 2

type semanticPolicyInjection struct {
	Trigger     string
	Reason      string
	SkillID     string
	PolicyScope string
	Next        string
}

func semanticPolicyInjectionOutput(
	bundle policy.Bundle,
	event Event,
) *HookSpecificOutput {
	if event.HookEventName != eventPreToolUse {
		return nil
	}

	injections := semanticPolicyInjections(bundle, event)
	if len(injections) == 0 {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: renderSemanticPolicyInjections(injections),
	}
}

func semanticPolicyInjections(
	bundle policy.Bundle,
	event Event,
) []semanticPolicyInjection {
	injections := []semanticPolicyInjection{}

	if semanticGitMutationIntent(event) {
		injections = append(injections, semanticPolicyInjection{
			Trigger:     "git_mutation",
			Reason:      "mutating Git intent detected in the incoming tool call",
			SkillID:     semanticSkillID(bundle, "safe-git-workflow"),
			PolicyScope: "git",
			Next: "Load the safe-git-workflow skill before changing Git state; " +
				"use " + wrapperRunnerPath + " policy-git and keep hook output visible.",
		})
	}

	if semanticPythonFileIntent(event) {
		injections = append(injections, semanticPolicyInjection{
			Trigger:     "python_file",
			Reason:      "Python file target detected in the incoming tool call",
			SkillID:     semanticSkillID(bundle, "conditional-imports", "lint-remediation"),
			PolicyScope: "python",
			Next: "Apply Python static-analysis policy before editing or reasoning; " +
				"prefer ruff/mypy evidence and load conditional-imports for import-cycle findings.",
		})
	}

	return injections
}

func semanticSkillID(bundle policy.Bundle, candidates ...string) string {
	for _, candidate := range candidates {
		if _, ok := bundle.Skills[candidate]; ok {
			return candidate
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	return candidates[0]
}

func semanticGitMutationIntent(event Event) bool {
	if event.ToolName != toolBash {
		return false
	}

	commands, err := shellparse.Commands(event.Command())
	if err != nil {
		return false
	}

	return slices.ContainsFunc(commands, shellCommandMutatesGit)
}

func shellCommandMutatesGit(command shellparse.Command) bool {
	args := command.Argv
	if len(args) == 0 {
		return false
	}

	switch shellCommandName(command) {
	case tokenGit:
		return semanticGitMutation(semanticGitSubcommand(args[1:]))
	case wrapperRunnerName:
		return len(args) > 2 &&
			args[1] == "policy-git" &&
			semanticGitMutation(semanticGitSubcommand(args[2:]))
	case "coding-ethos-git":
		return semanticGitMutation(semanticGitSubcommand(args[1:]))
	}

	return false
}

func semanticGitSubcommand(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "" {
			continue
		}

		if arg == "--" {
			return ""
		}

		if !strings.HasPrefix(arg, "-") {
			return arg
		}

		if semanticGitGlobalOptionConsumesNext(arg) {
			index++
		}
	}

	return ""
}

func semanticGitGlobalOptionConsumesNext(option string) bool {
	if strings.Contains(option, "=") {
		return false
	}

	switch option {
	case "-C",
		"-c",
		"--config-env",
		"--exec-path",
		"--git-dir",
		"--namespace",
		"--work-tree":
		return true
	default:
		return false
	}
}

func semanticGitMutation(operation string) bool {
	operation = strings.TrimSpace(operation)
	if operation == "" || strings.HasPrefix(operation, "-") {
		return false
	}

	switch operation {
	case "add",
		"am",
		"apply",
		"branch",
		"checkout",
		"cherry-pick",
		"clean",
		gitCommitOperation,
		"merge",
		"mv",
		"pull",
		operationPush,
		"rebase",
		"reset",
		"restore",
		"revert",
		"rm",
		"stash",
		"switch",
		"tag",
		"worktree":
		return true
	default:
		return false
	}
}

func semanticPythonFileIntent(event Event) bool {
	for _, file := range event.Files() {
		if strings.EqualFold(filepath.Ext(file), ".py") {
			return true
		}
	}

	return false
}

func renderSemanticPolicyInjections(
	injections []semanticPolicyInjection,
) string {
	lines := make([]string, 0, semanticPolicyInjectionHeaderLines+len(injections))
	lines = append(
		lines,
		"event: PreToolUse",
		"semantic_policy_injection["+strconv.Itoa(len(injections))+
			"]{trigger,reason,skill_id,policy_scope,next}:",
	)

	for _, injection := range injections {
		lines = append(
			lines,
			"  "+toonCell(injection.Trigger)+","+
				toonCell(injection.Reason)+","+
				toonCell(injection.SkillID)+","+
				toonCell(injection.PolicyScope)+","+
				toonCell(injection.Next),
		)
	}

	return strings.Join(lines, "\n")
}
