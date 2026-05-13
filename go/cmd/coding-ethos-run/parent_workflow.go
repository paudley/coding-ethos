// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const parentDefaultLintScope = "full"

type parentWorkflowOptions struct {
	Repo       string
	RepoEthos  string
	RepoConfig string
	Scope      string
}

type parentWorkflowStep struct {
	Name   string
	Status string
	Detail string
}

func runParentInstall(paths runtimePaths, rest []string) error {
	options, err := parseParentWorkflowFlags(paths, "parent-install", rest)
	if err != nil {
		return err
	}

	steps := syncParentArtifacts(paths, options)
	printParentWorkflowReport("parent-install", parentStepStatus(steps), options.Repo, steps)
	exitForFailedParentSteps(steps)

	return nil
}

func runParentCheck(paths runtimePaths, rest []string) error {
	options, err := parseParentWorkflowFlags(paths, "parent-check", rest)
	if err != nil {
		return err
	}

	steps := checkParentArtifacts(paths, options)
	printParentWorkflowReport("parent-check", parentStepStatus(steps), options.Repo, steps)
	exitForFailedParentSteps(steps)

	return nil
}

func runParentLint(paths runtimePaths, rest []string) error {
	options, err := parseParentWorkflowFlags(paths, "parent-lint", rest)
	if err != nil {
		return err
	}

	requirePolicyBundle(paths)
	installGitWrapperShim(paths)
	installLintToolShims(paths)

	steps := syncParentArtifacts(paths, options)
	if parentStepStatus(steps) != "pass" {
		printParentWorkflowReport("parent-lint", "fail", options.Repo, steps)
		requestRuntimeExit(1)
	}

	restoreFormat := forceTOONOutput()
	defer restoreFormat()

	runtimeExecLint(paths, parentLintArgs(paths, options)...)

	return nil
}

func parseParentWorkflowFlags(
	paths runtimePaths,
	command string,
	args []string,
) (parentWorkflowOptions, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", paths.Root, "Parent repository root")
	repoEthos := flags.String("repo-ethos", "", "Optional parent repo ethos overlay")
	repoConfig := flags.String("repo-config", "", "Optional parent repo config")
	scope := flags.String("scope", parentDefaultLintScope, "Parent lint scope")

	err := flags.Parse(args)
	if err != nil {
		return parentWorkflowOptions{}, fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*repo) == "" {
		return parentWorkflowOptions{}, apperror.StaticError("parent workflow requires --repo")
	}

	return parentWorkflowOptions{
		Repo:       filepath.Clean(*repo),
		RepoEthos:  firstExistingPath(*repoEthos, parentRepoEthosCandidates(*repo)),
		RepoConfig: firstExistingPath(*repoConfig, parentRepoConfigCandidates(*repo)),
		Scope:      strings.TrimSpace(*scope),
	}, nil
}

func syncParentArtifacts(
	paths runtimePaths,
	options parentWorkflowOptions,
) []parentWorkflowStep {
	steps := []parentWorkflowStep{}
	steps = append(steps, runParentStep("tool_configs", func() error {
		_, err := toolconfigs.Sync(paths.EthosRoot, options.Repo, options.RepoConfig)

		return err
	}))
	steps = append(steps, runParentStep("gemini_prompts", func() error {
		_, err := geminiprompts.Sync(parentGeminiOptions(paths, options))

		return err
	}))
	steps = append(steps, runParentStep("agent_skills", func() error {
		_, err := agentskills.Sync(parentAgentSkillOptions(paths, options))

		return err
	}))
	steps = append(steps, runParentStep("agent_hooks", func() error {
		return agenthooks.SyncSettings(options.Repo, paths.RunBinary+" agent-hook")
	}))

	return steps
}

func checkParentArtifacts(
	paths runtimePaths,
	options parentWorkflowOptions,
) []parentWorkflowStep {
	steps := []parentWorkflowStep{}
	steps = append(steps, runParentStep("tool_configs", func() error {
		mismatched, err := toolconfigs.Check(paths.EthosRoot, options.Repo, options.RepoConfig)
		if err != nil {
			return err
		}

		return parentDriftError(paths, options, "tool_configs", mismatched)
	}))
	steps = append(steps, runParentStep("gemini_prompts", func() error {
		mismatched, err := geminiprompts.Check(parentGeminiOptions(paths, options))
		if err != nil {
			return err
		}

		return parentDriftError(paths, options, "gemini_prompts", mismatched)
	}))
	steps = append(steps, runParentStep("agent_skills", func() error {
		mismatched, err := agentskills.Check(parentAgentSkillOptions(paths, options))
		if err != nil {
			return err
		}

		return parentDriftError(paths, options, "agent_skills", mismatched)
	}))
	steps = append(steps, runParentStep("agent_hooks", func() error {
		return agenthooks.DoctorSettings(options.Repo, paths.RunBinary+" agent-hook")
	}))

	return steps
}

func runParentStep(name string, action func() error) parentWorkflowStep {
	err := action()
	if err != nil {
		return parentWorkflowStep{Name: name, Status: "fail", Detail: err.Error()}
	}

	return parentWorkflowStep{Name: name, Status: "pass"}
}

func parentDriftError(
	paths runtimePaths,
	options parentWorkflowOptions,
	artifact string,
	mismatched []string,
) error {
	if len(mismatched) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%s out of sync in %s checkout; run: %s; drift: %s",
		artifact,
		parentCheckoutLocation(paths, options),
		parentInstallCommand(options),
		parentDriftItems(paths, options, mismatched),
	)
}

func parentStepStatus(steps []parentWorkflowStep) string {
	for _, step := range steps {
		if step.Status != "pass" {
			return "fail"
		}
	}

	return "pass"
}

func exitForFailedParentSteps(steps []parentWorkflowStep) {
	if parentStepStatus(steps) != "pass" {
		requestRuntimeExit(1)
	}
}

func printParentWorkflowReport(
	tool string,
	status string,
	repo string,
	steps []parentWorkflowStep,
) {
	fmt.Fprintln(os.Stdout, "format: toon")
	fmt.Fprintln(os.Stdout, "tool: "+hookoutput.TOONCell(tool))
	fmt.Fprintln(os.Stdout, "status: "+hookoutput.TOONCell(status))
	fmt.Fprintln(os.Stdout, "repo: "+hookoutput.TOONCell(repo))
	fmt.Fprintf(os.Stdout, "steps[%d]{name,status,detail}:\n", len(steps))
	for _, step := range steps {
		fmt.Fprintf(
			os.Stdout,
			"  %s,%s,%s\n",
			hookoutput.TOONCell(step.Name),
			hookoutput.TOONCell(step.Status),
			hookoutput.TOONFindingCell(step.Detail),
		)
	}
}

func parentLintArgs(paths runtimePaths, options parentWorkflowOptions) []string {
	scope := firstNonBlank(options.Scope, parentDefaultLintScope)

	return []string{
		"--bundle",
		paths.PolicyBundle,
		"--ethos-root",
		paths.EthosRoot,
		"--consumer-root",
		options.Repo,
		"--invocation-cwd",
		paths.InvocationCWD,
		"--cwd",
		options.Repo,
		"--scope",
		scope,
	}
}

func parentInstallCommand(options parentWorkflowOptions) string {
	return "coding-ethos/bin/coding-ethos-run parent-install --repo " +
		shellQuoteForParent(options.Repo)
}

func parentCheckoutLocation(paths runtimePaths, options parentWorkflowOptions) string {
	if sameCleanPath(options.Repo, paths.EthosRoot) {
		return "submodule"
	}

	return "parent"
}

func parentDriftItems(
	paths runtimePaths,
	options parentWorkflowOptions,
	mismatched []string,
) string {
	items := make([]string, 0, len(mismatched))
	for _, path := range mismatched {
		items = append(items, parentDriftItem(paths, options, path))
	}

	return strings.Join(items, " ")
}

func parentDriftItem(paths runtimePaths, options parentWorkflowOptions, path string) string {
	relative := relativeToRepo(options.Repo, path)
	state := gitPathState(paths.RealGit, options.Repo, relative)

	return relative + "(" + state + ")"
}

func relativeToRepo(repo, path string) string {
	relative, err := filepath.Rel(filepath.Clean(repo), filepath.Clean(path))
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(filepath.Clean(path))
	}

	return filepath.ToSlash(relative)
}

func gitPathState(realGit, repo, relative string) string {
	output, err := gitOutput(realGit, repo, "status", "--porcelain", "--", relative)
	if err != nil || strings.TrimSpace(output) == "" {
		return "working_tree"
	}

	line := strings.TrimSpace(strings.Split(output, "\n")[0])
	if strings.HasPrefix(line, "??") {
		return "untracked"
	}

	if len(line) < 2 {
		return "working_tree"
	}

	indexChanged := line[0] != ' '
	workingTreeChanged := line[1] != ' '
	switch {
	case indexChanged && workingTreeChanged:
		return "index+working_tree"
	case indexChanged:
		return "index"
	default:
		return "working_tree"
	}
}

func sameCleanPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}

	return leftAbs == rightAbs
}

func shellQuoteForParent(value string) string {
	if value == "" {
		return "''"
	}

	if !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();<>|&") {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parentGeminiOptions(
	paths runtimePaths,
	options parentWorkflowOptions,
) geminiprompts.Options {
	return geminiprompts.Options{
		EthosRoot:  paths.EthosRoot,
		RepoRoot:   options.Repo,
		Primary:    filepath.Join(paths.EthosRoot, "coding_ethos.yml"),
		RepoEthos:  options.RepoEthos,
		RepoConfig: options.RepoConfig,
	}
}

func parentAgentSkillOptions(
	paths runtimePaths,
	options parentWorkflowOptions,
) agentskills.Options {
	return agentskills.Options{
		EthosRoot: paths.EthosRoot,
		RepoRoot:  options.Repo,
		Primary:   filepath.Join(paths.EthosRoot, "coding_ethos.yml"),
		RepoEthos: options.RepoEthos,
	}
}

func parentRepoEthosCandidates(repo string) []string {
	return []string{
		filepath.Join(repo, "repo_ethos.yml"),
		filepath.Join(repo, "repo_ethos.yaml"),
	}
}

func parentRepoConfigCandidates(repo string) []string {
	return []string{
		filepath.Join(repo, "repo_config.yaml"),
		filepath.Join(repo, "repo_config.yml"),
	}
}

func firstExistingPath(explicit string, candidates []string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}

	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Clean(candidate))
		if err == nil && !info.IsDir() {
			return candidate
		}
	}

	return ""
}

func forceTOONOutput() func() {
	previous, found := os.LookupEnv(hookoutput.FormatEnv)
	_ = os.Setenv(hookoutput.FormatEnv, hookoutput.FormatTOON)

	return func() {
		if found {
			_ = os.Setenv(hookoutput.FormatEnv, previous)

			return
		}

		_ = os.Unsetenv(hookoutput.FormatEnv)
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
