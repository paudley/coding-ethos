// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/agentskills"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const (
	parentDefaultLintScope    = "full"
	parentExecutableDirMode   = 0o755
	parentPolicyBundleDirMode = 0o755
	parentStepFail            = "fail"
	parentStepPass            = "pass"
	parentWorkingTreeState    = "working_tree"
)

var (
	errParentArtifactDrift      = errors.New("parent artifact drift")
	errParentGoToolsStale       = errors.New("parent Go tools are stale")
	errParentPathIsDirectory    = errors.New("path is a directory, want file")
	errParentPathIsNotDirectory = errors.New("path is not a directory, want directory")
)

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
	printParentWorkflowReport(
		"parent-install",
		parentStepStatus(steps),
		options.Repo,
		steps,
	)
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
	if parentStepStatus(steps) != parentStepPass {
		printParentWorkflowReport("parent-lint", parentStepFail, options.Repo, steps)
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

	repoRoot, err := cleanParentRepoFlag(*repo)
	if err != nil {
		return parentWorkflowOptions{}, err
	}

	resolvedRepoEthos, err := firstExistingPath(
		*repoEthos,
		parentRepoEthosCandidates(repoRoot),
	)
	if err != nil {
		return parentWorkflowOptions{}, err
	}

	resolvedRepoConfig, err := firstExistingPath(
		*repoConfig,
		parentRepoConfigCandidates(repoRoot),
	)
	if err != nil {
		return parentWorkflowOptions{}, err
	}

	return parentWorkflowOptions{
		Repo:       repoRoot,
		RepoEthos:  resolvedRepoEthos,
		RepoConfig: resolvedRepoConfig,
		Scope:      strings.TrimSpace(*scope),
	}, nil
}

func cleanParentRepoFlag(repo string) (string, error) {
	if strings.TrimSpace(repo) == "" {
		return "", apperror.StaticError("parent workflow requires --repo")
	}

	return filepath.Clean(repo), nil
}

func syncParentArtifacts(
	paths runtimePaths,
	options parentWorkflowOptions,
) []parentWorkflowStep {
	steps := []parentWorkflowStep{}
	steps = append(steps, runParentStep("go_tools", func() error {
		return rebuildParentGoTools(paths)
	}))
	steps = append(steps, runParentStep("policy_bundle", func() error {
		return syncParentPolicyBundle(paths, options)
	}))
	steps = append(steps, runParentStep("tool_configs", func() error {
		_, err := toolconfigs.Sync(paths.EthosRoot, options.Repo, options.RepoConfig)
		if err != nil {
			return fmt.Errorf("sync tool configs: %w", err)
		}

		return nil
	}))
	steps = append(steps, runParentStep("gemini_prompts", func() error {
		_, err := geminiprompts.Sync(parentGeminiOptions(paths, options))
		if err != nil {
			return fmt.Errorf("sync gemini prompts: %w", err)
		}

		return nil
	}))
	steps = append(steps, runParentStep("agent_skills", func() error {
		_, err := agentskills.Sync(parentAgentSkillOptions(paths, options))
		if err != nil {
			return fmt.Errorf("sync agent skills: %w", err)
		}

		return nil
	}))
	steps = append(steps, runParentStep("agent_hooks", func() error {
		return agenthooks.SyncSettings(options.Repo, parentAgentHookCommand(paths))
	}))
	steps = append(steps, runParentStep("code_intel", func() error {
		return refreshParentCodeIntel(options.Repo)
	}))

	return steps
}

func checkParentArtifacts(
	paths runtimePaths,
	options parentWorkflowOptions,
) []parentWorkflowStep {
	steps := []parentWorkflowStep{}
	steps = append(steps, runParentStep("go_tools", func() error {
		return checkParentGoTools(paths, options)
	}))
	steps = append(steps, runParentStep("policy_bundle", func() error {
		return checkParentPolicyBundle(paths, options)
	}))
	steps = append(steps, runParentStep("tool_configs", func() error {
		mismatched, err := toolconfigs.Check(
			paths.EthosRoot,
			options.Repo,
			options.RepoConfig,
		)
		if err != nil {
			return fmt.Errorf("check tool configs: %w", err)
		}

		return parentDriftError(paths, options, "tool_configs", mismatched)
	}))
	steps = append(steps, runParentStep("gemini_prompts", func() error {
		mismatched, err := geminiprompts.Check(parentGeminiOptions(paths, options))
		if err != nil {
			return fmt.Errorf("check gemini prompts: %w", err)
		}

		return parentDriftError(paths, options, "gemini_prompts", mismatched)
	}))
	steps = append(steps, runParentStep("agent_skills", func() error {
		mismatched, err := agentskills.Check(parentAgentSkillOptions(paths, options))
		if err != nil {
			return fmt.Errorf("check agent skills: %w", err)
		}

		return parentDriftError(paths, options, "agent_skills", mismatched)
	}))
	steps = append(steps, runParentStep("agent_hooks", func() error {
		return agenthooks.DoctorSettings(options.Repo, parentAgentHookCommand(paths))
	}))
	steps = append(steps, runParentStep("code_intel", func() error {
		return refreshParentCodeIntel(options.Repo)
	}))

	return steps
}

func refreshParentCodeIntel(repo string) error {
	_, err := codeintel.RefreshRepository(context.Background(), repo, []string{"."})
	if err != nil {
		return fmt.Errorf("refresh code-intel: %w", err)
	}

	return nil
}

func syncParentPolicyBundle(paths runtimePaths, options parentWorkflowOptions) error {
	bundle, metadata, err := compileParentPolicyBundle(paths, options, "")
	if err != nil {
		return err
	}

	err = writePolicyBundleArtifacts(filepath.Dir(paths.PolicyBundle), bundle, metadata)
	if err != nil {
		return err
	}

	runtimePolicyDir, err := parentRuntimePolicyDir(paths, options.Repo)
	if err != nil {
		return err
	}

	err = writePolicyBundleArtifacts(runtimePolicyDir, bundle, metadata)
	if err != nil {
		return err
	}

	return syncParentRuntimeTools(paths, options)
}

func checkParentPolicyBundle(paths runtimePaths, options parentWorkflowOptions) error {
	existing, generatedAt, err := existingPolicyBundle(paths.PolicyBundle)
	if err != nil {
		return err
	}

	bundle, metadata, err := compileParentPolicyBundle(paths, options, generatedAt)
	if err != nil {
		return err
	}

	existingMetadata, err := existingPolicyMetadata(paths.PolicyMetadata)
	if err != nil {
		return err
	}

	if !policyBundleCurrent(existing, existingMetadata, bundle, metadata) {
		return fmt.Errorf(
			"%w: policy_bundle out of sync in %s checkout; run: %s",
			errParentArtifactDrift,
			parentCheckoutLocation(paths, options),
			parentInstallCommand(options),
		)
	}

	runtimePolicyDir, err := parentRuntimePolicyDir(paths, options.Repo)
	if err != nil {
		return err
	}

	runtimeBundlePath := filepath.Join(runtimePolicyDir, "policy-bundle.json")

	runtimeBundle, _, err := existingPolicyBundle(runtimeBundlePath)
	if err != nil {
		return err
	}

	runtimeMetadata, err := existingPolicyMetadata(
		filepath.Join(runtimePolicyDir, "policy-metadata.json"),
	)
	if err != nil {
		return err
	}

	if !policyBundleCurrent(runtimeBundle, runtimeMetadata, bundle, metadata) {
		return fmt.Errorf(
			"%w: installed policy_bundle out of sync in %s checkout; run: %s",
			errParentArtifactDrift,
			parentCheckoutLocation(paths, options),
			parentInstallCommand(options),
		)
	}

	return nil
}

func compileParentPolicyBundle(
	paths runtimePaths,
	options parentWorkflowOptions,
	generatedAt string,
) (policy.Bundle, policy.Metadata, error) {
	bundle, metadata, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(paths.EthosRoot, "coding_ethos.yml"),
		RepoEthos: firstNonEmptyPath(
			options.RepoEthos,
			filepath.Join(paths.EthosRoot, "repo_ethos.yml"),
		),
		Config:      filepath.Join(paths.EthosRoot, "config.yaml"),
		RepoConfig:  options.RepoConfig,
		GeneratedAt: generatedAt,
	})
	if err != nil {
		return policy.Bundle{}, policy.Metadata{}, fmt.Errorf(
			"compile parent policy bundle: %w",
			err,
		)
	}

	return bundle, metadata, nil
}

func writePolicyBundleArtifacts(
	outDir string,
	bundle policy.Bundle,
	metadata policy.Metadata,
) error {
	err := os.MkdirAll(outDir, parentPolicyBundleDirMode)
	if err != nil {
		return fmt.Errorf("create policy bundle dir: %w", err)
	}

	err = writePolicyJSONFile(
		filepath.Join(outDir, "policy-bundle.json"),
		func(file *os.File) error {
			return policy.EncodeBundle(file, bundle)
		},
	)
	if err != nil {
		return err
	}

	return writePolicyJSONFile(
		filepath.Join(outDir, "policy-metadata.json"),
		func(file *os.File) error {
			return policy.EncodeMetadata(file, metadata)
		},
	)
}

func writePolicyJSONFile(path string, encode func(*os.File) error) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary policy file: %w", err)
	}

	tempPath := temp.Name()

	defer func() {
		_ = os.Remove(tempPath)
	}()

	err = encode(temp)
	if err != nil {
		_ = temp.Close()

		return err
	}

	err = temp.Close()
	if err != nil {
		return fmt.Errorf("close temporary policy file: %w", err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		return fmt.Errorf("install policy file %s: %w", path, err)
	}

	return nil
}

func existingPolicyBundle(path string) (policy.Bundle, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, "", fmt.Errorf("open policy bundle %s: %w", path, err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, "", fmt.Errorf("decode policy bundle %s: %w", path, err)
	}

	return bundle, bundle.GeneratedAt, nil
}

func existingPolicyMetadata(path string) (policy.Metadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Metadata{}, fmt.Errorf("open policy metadata %s: %w", path, err)
	}
	defer file.Close()

	metadata, err := policy.DecodeMetadata(file)
	if err != nil {
		return policy.Metadata{}, fmt.Errorf("decode policy metadata %s: %w", path, err)
	}

	return metadata, nil
}

func policyBundleCurrent(
	existing policy.Bundle,
	existingMetadata policy.Metadata,
	expected policy.Bundle,
	expectedMetadata policy.Metadata,
) bool {
	return samePolicyIDs(existing.Policies, expected.Policies) &&
		sameStringMap(existingMetadata.SourceHashes, expectedMetadata.SourceHashes)
}

func samePolicyIDs(left, right map[string]policy.Policy) bool {
	if len(left) != len(right) {
		return false
	}

	for id := range left {
		if _, found := right[id]; !found {
			return false
		}
	}

	return true
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}

	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}

	return true
}

func syncParentRuntimeTools(paths runtimePaths, options parentWorkflowOptions) error {
	runtimeBinDir, err := parentRuntimeBinDir(paths, options.Repo)
	if err != nil {
		return err
	}

	err = os.MkdirAll(runtimeBinDir, parentExecutableDirMode)
	if err != nil {
		return fmt.Errorf("create parent runtime bin dir: %w", err)
	}

	entries, err := os.ReadDir(paths.BinDir)
	if err != nil {
		return fmt.Errorf("read parent tool bin dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "coding-ethos-") {
			continue
		}

		source := filepath.Join(paths.BinDir, entry.Name())
		destination := filepath.Join(runtimeBinDir, entry.Name())

		err = copyFileMode(source, destination, parentExecutableDirMode)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyFileMode(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()

	temp, err := os.CreateTemp(
		filepath.Dir(destination),
		"."+filepath.Base(destination)+"-*.tmp",
	)
	if err != nil {
		return fmt.Errorf("create temporary copy for %s: %w", destination, err)
	}

	tempPath := temp.Name()

	defer func() {
		_ = os.Remove(tempPath)
	}()

	_, err = io.Copy(temp, input)
	if err != nil {
		_ = temp.Close()

		return fmt.Errorf("copy %s to %s: %w", source, destination, err)
	}

	err = temp.Chmod(mode)
	if err != nil {
		_ = temp.Close()

		return fmt.Errorf("chmod temporary copy %s: %w", destination, err)
	}

	err = temp.Close()
	if err != nil {
		return fmt.Errorf("close temporary copy %s: %w", destination, err)
	}

	err = os.Rename(tempPath, destination)
	if err != nil {
		return fmt.Errorf("install %s: %w", destination, err)
	}

	return nil
}

func parentRuntimeBinDir(paths runtimePaths, repo string) (string, error) {
	runtimeDir, err := parentHookRuntimeDir(paths, repo)
	if err != nil {
		return "", err
	}

	return filepath.Join(runtimeDir, "bin"), nil
}

func parentRuntimePolicyDir(paths runtimePaths, repo string) (string, error) {
	runtimeDir, err := parentHookRuntimeDir(paths, repo)
	if err != nil {
		return "", err
	}

	return filepath.Join(runtimeDir, "policy"), nil
}

func parentHookRuntimeDir(paths runtimePaths, repo string) (string, error) {
	gitCommonDir, err := gitOutput(paths.RealGit, repo, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve parent git common dir: %w", err)
	}

	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(repo, gitCommonDir)
	}

	return filepath.Join(filepath.Clean(gitCommonDir), "coding-ethos-hooks"), nil
}

func firstNonEmptyPath(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func rebuildParentGoTools(paths runtimePaths) error {
	err := requireParentGoToolsSource(paths)
	if err != nil {
		return err
	}

	err = os.MkdirAll(paths.BinDir, parentExecutableDirMode)
	if err != nil {
		return fmt.Errorf("create Go tool bin dir: %w", err)
	}

	tools, err := parentGoToolCommands(paths)
	if err != nil {
		return err
	}

	for _, tool := range tools {
		err = buildParentGoTool(paths, tool)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildParentGoTool(paths runtimePaths, tool string) error {
	outputPath := filepath.Join(paths.BinDir, tool)

	tempFile, err := os.CreateTemp(paths.BinDir, "."+tool+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary binary for %s: %w", tool, err)
	}

	tempPath := tempFile.Name()

	err = tempFile.Close()
	if err != nil {
		return fmt.Errorf("close temporary binary for %s: %w", tool, err)
	}

	err = os.Remove(tempPath)
	if err != nil {
		return fmt.Errorf("prepare temporary binary for %s: %w", tool, err)
	}

	defer func() {
		_ = os.Remove(tempPath)
	}()

	command := safeexec.CommandContext(
		context.Background(),
		"go",
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-o",
		tempPath,
		"./cmd/"+tool,
	)
	command.Dir = paths.ToolsSource

	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return fmt.Errorf("build %s: %w: %s", tool, err, detail)
		}

		return fmt.Errorf("build %s: %w", tool, err)
	}

	err = os.Rename(tempPath, outputPath)
	if err != nil {
		return fmt.Errorf("install %s: %w", tool, err)
	}

	return nil
}

func checkParentGoTools(paths runtimePaths, options parentWorkflowOptions) error {
	err := requireParentGoToolsSource(paths)
	if err != nil {
		return err
	}

	latestSource, err := latestParentGoToolSourceModTime(paths.ToolsSource)
	if err != nil {
		return err
	}

	stale := []string{}

	tools, err := parentGoToolCommands(paths)
	if err != nil {
		return err
	}

	for _, tool := range tools {
		toolPath := filepath.Join(paths.BinDir, tool)

		info, err := os.Stat(toolPath)
		if err != nil {
			stale = append(stale, tool+"(missing)")

			continue
		}

		if info.IsDir() || info.Mode()&0o111 == 0 {
			stale = append(stale, tool+"(not_executable)")

			continue
		}

		if info.ModTime().Before(latestSource) {
			stale = append(stale, tool+"(stale)")
		}
	}

	if len(stale) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: run %s; tools: %s",
		errParentGoToolsStale,
		parentInstallCommand(options),
		strings.Join(stale, " "),
	)
}

func requireParentGoToolsSource(paths runtimePaths) error {
	info, err := os.Stat(paths.ToolsSource)
	if err != nil {
		return fmt.Errorf("stat Go tools source: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf(
			"go tools source %s: %w",
			paths.ToolsSource,
			errParentPathIsNotDirectory,
		)
	}

	return nil
}

func parentGoToolCommands(paths runtimePaths) ([]string, error) {
	cmdRoot := filepath.Join(paths.ToolsSource, "cmd")

	entries, err := os.ReadDir(cmdRoot)
	if err != nil {
		return nil, fmt.Errorf("read Go cmd dir: %w", err)
	}

	tools := []string{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		mainPath := filepath.Join(cmdRoot, entry.Name(), "main.go")

		info, err := os.Stat(mainPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("stat Go command %s: %w", entry.Name(), err)
		}

		if info.IsDir() {
			continue
		}

		tools = append(tools, entry.Name())
	}

	sort.Strings(tools)

	if len(tools) == 0 {
		return nil, apperror.StaticError("Go cmd dir has no buildable tools")
	}

	return tools, nil
}

func latestParentGoToolSourceModTime(root string) (time.Time, error) {
	latest := time.Time{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if path != root && shouldSkipParentGoToolSourceDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if !parentGoToolSourceFile(entry.Name()) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat Go source %s: %w", path, err)
		}

		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}

		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("walk Go tools source: %w", err)
	}

	if latest.IsZero() {
		return time.Time{}, apperror.StaticError("Go tools source has no build inputs")
	}

	return latest, nil
}

func shouldSkipParentGoToolSourceDir(name string) bool {
	switch name {
	case "bin", ".git", ".pytest_cache", ".ruff_cache", ".mypy_cache":
		return true
	default:
		return false
	}
}

func parentGoToolSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go") ||
		name == "go.mod" ||
		name == "go.sum"
}

func runParentStep(name string, action func() error) parentWorkflowStep {
	err := action()
	if err != nil {
		return parentWorkflowStep{Name: name, Status: parentStepFail, Detail: err.Error()}
	}

	return parentWorkflowStep{Name: name, Status: parentStepPass}
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
		"%w: %s out of sync in %s checkout; run: %s; drift: %s",
		errParentArtifactDrift,
		artifact,
		parentCheckoutLocation(paths, options),
		parentInstallCommand(options),
		parentDriftItems(paths, options, mismatched),
	)
}

func parentStepStatus(steps []parentWorkflowStep) string {
	for _, step := range steps {
		if step.Status != parentStepPass {
			return parentStepFail
		}
	}

	return parentStepPass
}

func exitForFailedParentSteps(steps []parentWorkflowStep) {
	if parentStepStatus(steps) != parentStepPass {
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
	args := []string{
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

	if options.RepoEthos != "" {
		args = append(args, "--repo-ethos", options.RepoEthos)
	}

	if options.RepoConfig != "" {
		args = append(args, "--repo-config", options.RepoConfig)
	}

	return args
}

func parentInstallCommand(options parentWorkflowOptions) string {
	return "coding-ethos/bin/coding-ethos-run parent-install --repo " +
		shellquote.Arg(options.Repo)
}

func parentCheckoutLocation(paths runtimePaths, options parentWorkflowOptions) string {
	if sameCleanPath(options.Repo, paths.EthosRoot) {
		return "coding-ethos"
	}

	return "parent"
}

func parentDriftItems(
	paths runtimePaths,
	options parentWorkflowOptions,
	mismatched []string,
) string {
	statuses := gitPathStates(paths.RealGit, options.Repo, mismatched)
	items := make([]string, 0, len(mismatched))

	for _, path := range mismatched {
		items = append(items, parentDriftItem(options, path, statuses))
	}

	return strings.Join(items, " ")
}

func parentDriftItem(
	options parentWorkflowOptions,
	path string,
	statuses map[string]string,
) string {
	relative := relativeToRepo(options.Repo, path)
	state := statuses[relative]

	if state == "" {
		state = parentWorkingTreeState
	}

	return relative + "(" + state + ")"
}

func parentAgentHookCommand(paths runtimePaths) string {
	return shellquote.Command(paths.RunBinary, "agent-hook")
}

func relativeToRepo(repo, path string) string {
	relative, err := filepath.Rel(filepath.Clean(repo), filepath.Clean(path))
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(filepath.Clean(path))
	}

	return filepath.ToSlash(relative)
}

func gitPathState(realGit, repo, relative string) string {
	return gitPathStates(realGit, repo, []string{relative})[relative]
}

func gitPathStates(realGit, repo string, paths []string) map[string]string {
	relativePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		relativePaths = append(relativePaths, relativeToRepo(repo, path))
	}

	args := append([]string{"status", "--porcelain", "--"}, relativePaths...)
	output, err := gitOutputRaw(realGit, repo, args...)

	if err != nil || strings.TrimSpace(output) == "" {
		return defaultGitPathStates(relativePaths)
	}

	states := defaultGitPathStates(relativePaths)

	for line := range strings.SplitSeq(strings.TrimRight(output, "\r\n"), "\n") {
		path, state := parseGitStatusLine(line)
		if path != "" {
			states[path] = state
		}
	}

	return states
}

func defaultGitPathStates(paths []string) map[string]string {
	states := make(map[string]string, len(paths))
	for _, path := range paths {
		states[path] = parentWorkingTreeState
	}

	return states
}

func parseGitStatusLine(line string) (string, string) {
	if path, found := strings.CutPrefix(line, "??"); found {
		return strings.TrimSpace(path), "untracked"
	}

	const gitStatusPorcelainWidth = 2

	if len(line) < gitStatusPorcelainWidth {
		return "", parentWorkingTreeState
	}

	indexChanged := line[0] != ' '
	workingTreeChanged := line[1] != ' '
	path := strings.TrimSpace(line[gitStatusPorcelainWidth:])

	switch {
	case indexChanged && workingTreeChanged:
		return path, "index+" + parentWorkingTreeState
	case indexChanged:
		return path, "index"
	default:
		return path, parentWorkingTreeState
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

func firstExistingPath(explicit string, candidates []string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		info, err := os.Stat(filepath.Clean(explicit))
		if err != nil {
			return "", fmt.Errorf("parent path %s: %w", explicit, err)
		}

		if info.IsDir() {
			return "", fmt.Errorf("parent path %s: %w", explicit, errParentPathIsDirectory)
		}

		return filepath.Clean(explicit), nil
	}

	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Clean(candidate))
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", nil
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
