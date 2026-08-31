// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
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
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/repoignore"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const (
	parentCheckoutCodingEthos = "coding-ethos"
	parentDefaultLintScope    = "full"
	parentExecutableDirMode   = 0o755
	parentPolicyBundleDirMode = 0o755
	parentStepFail            = "fail"
	parentStepPass            = "pass"
	parentWorkingTreeState    = "working_tree"
)

var (
	errParentArtifactDrift       = errors.New("parent artifact drift")
	errParentGoToolsStale        = errors.New("parent Go tools are stale")
	errParentPathIsDirectory     = errors.New("path is a directory, want file")
	errParentPathIsNotDirectory  = errors.New("path is not a directory, want directory")
	errParentRootNotAbsolute     = errors.New("parent workflow root must be absolute")
	errParentSourceNotExecutable = errors.New(
		"source executable is not a regular executable",
	)
)

type parentWorkflowOptions struct {
	Repo       string
	StateRoot  string
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

func runParentRuntimeSync(paths runtimePaths, rest []string) error {
	options, err := parseParentWorkflowFlags(paths, "parent-runtime-sync", rest)
	if err != nil {
		return err
	}

	steps := []parentWorkflowStep{runParentStep("hook_runtime", func() error {
		return syncParentHookRuntimeExecutables(paths, options)
	})}
	printParentWorkflowReport(
		"parent-runtime-sync",
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

func runRuntimePolicy(paths runtimePaths, rest []string) error {
	if len(rest) == 0 || (rest[0] != "sync" && rest[0] != "check") {
		return apperror.StaticError(
			"runtime-policy requires sync or check followed by --repo <path>",
		)
	}

	action := rest[0]

	options, err := parseParentWorkflowFlags(
		paths,
		"runtime-policy "+action,
		rest[1:],
	)
	if err != nil {
		return err
	}

	step := runParentStep("policy_bundle", func() error {
		if action == "sync" {
			return syncParentPolicyBundle(paths, options)
		}

		return checkParentPolicyBundle(paths, options)
	})
	steps := []parentWorkflowStep{step}
	printParentWorkflowReport(
		"runtime-policy "+action,
		parentStepStatus(steps),
		options.Repo,
		steps,
	)
	exitForFailedParentSteps(steps)

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
	stateRoot := flags.String(
		"state-root",
		"",
		"Private Coding Ethos state root for runtime policy artifacts",
	)
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

	resolvedStateRoot, err := cleanOptionalRoot("state-root", *stateRoot)
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
		StateRoot:  resolvedStateRoot,
		RepoEthos:  resolvedRepoEthos,
		RepoConfig: resolvedRepoConfig,
		Scope:      strings.TrimSpace(*scope),
	}, nil
}

func cleanOptionalRoot(label, root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", nil
	}

	cleaned := filepath.Clean(root)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%w: %s=%s", errParentRootNotAbsolute, label, root)
	}

	return cleaned, nil
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
		return checkParentGoTools(paths)
	}))
	if parentStepStatus(steps) != parentStepPass {
		return steps
	}

	steps = append(steps, runParentStep("hook_runtime", func() error {
		return syncParentHookRuntimeExecutables(paths, options)
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

	if !parentUsesExternalStateRoot(options) {
		steps = append(steps, runParentStep("repo_ignores", func() error {
			_, err := repoignore.RepairGitignore(options.Repo)
			if err != nil {
				return fmt.Errorf("repair repo ignores: %w", err)
			}

			return nil
		}))
	}

	return steps
}

func parentUsesExternalStateRoot(options parentWorkflowOptions) bool {
	return options.StateRoot != "" && !sameCleanPath(options.StateRoot, options.Repo)
}

func checkParentArtifacts(
	paths runtimePaths,
	options parentWorkflowOptions,
) []parentWorkflowStep {
	steps := []parentWorkflowStep{}
	steps = append(steps, runParentStep("go_tools", func() error {
		return checkParentGoTools(paths)
	}))
	steps = append(steps, runParentStep("hook_runtime", func() error {
		return checkParentHookRuntimeExecutables(paths, options)
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

	return steps
}

func syncParentPolicyBundle(paths runtimePaths, options parentWorkflowOptions) error {
	bundle, metadata, err := compileParentPolicyBundle(paths, options, "")
	if err != nil {
		return err
	}

	return writePolicyBundleArtifacts(
		parentPolicyBundleDir(paths, options),
		bundle,
		metadata,
	)
}

func checkParentPolicyBundle(paths runtimePaths, options parentWorkflowOptions) error {
	existing, generatedAt, err := existingPolicyBundle(
		parentPolicyBundlePath(paths, options),
	)
	if err != nil {
		return err
	}

	bundle, metadata, err := compileParentPolicyBundle(paths, options, generatedAt)
	if err != nil {
		return err
	}

	existingMetadata, err := existingPolicyMetadata(
		parentPolicyMetadataPath(paths, options),
	)
	if err != nil {
		return err
	}

	if !policyBundleCurrent(existing, existingMetadata, bundle, metadata) {
		return fmt.Errorf(
			"%w: policy_bundle out of sync in %s checkout; run: %s",
			errParentArtifactDrift,
			parentCheckoutLocation(paths, options),
			parentPolicySyncCommand(options),
		)
	}

	return nil
}

func parentPolicyBundlePath(paths runtimePaths, options parentWorkflowOptions) string {
	return filepath.Join(parentPolicyBundleDir(paths, options), "policy-bundle.json")
}

func parentPolicyMetadataPath(
	paths runtimePaths,
	options parentWorkflowOptions,
) string {
	return filepath.Join(parentPolicyBundleDir(paths, options), "policy-metadata.json")
}

func parentPolicyBundleDir(paths runtimePaths, options parentWorkflowOptions) string {
	if options.StateRoot != "" {
		return filepath.Join(options.StateRoot, ".coding-ethos", "policy")
	}

	return filepath.Join(
		parentGitCommonDir(paths, options.Repo),
		"coding-ethos-hooks",
		"policy",
	)
}

func parentGitCommonDir(paths runtimePaths, repo string) string {
	gitCommonDir, err := gitOutput(
		paths.RealGit,
		repo,
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	if err == nil && strings.TrimSpace(gitCommonDir) != "" {
		return gitCommonDir
	}

	return filepath.Join(repo, ".git")
}

func parentHookRuntimeBinDir(paths runtimePaths, options parentWorkflowOptions) string {
	return filepath.Join(
		parentGitCommonDir(paths, options.Repo),
		"coding-ethos-hooks",
		"bin",
	)
}

func syncParentHookRuntimeExecutables(
	paths runtimePaths,
	options parentWorkflowOptions,
) error {
	tools, err := parentGoToolCommands(paths)
	if err != nil {
		return err
	}

	runtimeBin := parentHookRuntimeBinDir(paths, options)

	err = os.MkdirAll(runtimeBin, parentExecutableDirMode)
	if err != nil {
		return fmt.Errorf("create parent hook runtime bin dir: %w", err)
	}

	for _, tool := range tools {
		source := filepath.Join(paths.BinDir, tool)
		destination := filepath.Join(runtimeBin, tool)

		err = installParentHookRuntimeExecutable(source, destination)
		if err != nil {
			return fmt.Errorf("install parent hook runtime %s: %w", tool, err)
		}
	}

	return nil
}

func installParentHookRuntimeExecutable(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("stat source executable %s: %w", source, err)
	}

	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%w: %s", errParentSourceNotExecutable, source)
	}

	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source executable %s: %w", source, err)
	}
	defer input.Close()

	temporary, err := os.CreateTemp(
		filepath.Dir(destination),
		"."+filepath.Base(destination)+"-*.tmp",
	)
	if err != nil {
		return fmt.Errorf("create temporary hook runtime executable: %w", err)
	}

	temporaryPath := temporary.Name()

	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	err = temporary.Chmod(info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("set temporary hook runtime executable mode: %w", err)
	}

	_, err = io.Copy(temporary, input)
	if err != nil {
		return fmt.Errorf("copy hook runtime executable: %w", err)
	}

	err = temporary.Sync()
	if err != nil {
		return fmt.Errorf("sync temporary hook runtime executable: %w", err)
	}

	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("close temporary hook runtime executable: %w", err)
	}

	err = os.Rename(temporaryPath, destination)
	if err != nil {
		return fmt.Errorf("activate hook runtime executable %s: %w", destination, err)
	}

	return nil
}

func checkParentHookRuntimeExecutables(
	paths runtimePaths,
	options parentWorkflowOptions,
) error {
	tools, err := parentGoToolCommands(paths)
	if err != nil {
		return err
	}

	runtimeBin := parentHookRuntimeBinDir(paths, options)
	mismatched := []string{}

	for _, tool := range tools {
		source := filepath.Join(paths.BinDir, tool)
		destination := filepath.Join(runtimeBin, tool)

		status, err := compareParentHookRuntimeExecutable(source, destination)
		if err != nil {
			return fmt.Errorf("check parent hook runtime %s: %w", tool, err)
		}

		if status != "" {
			mismatched = append(mismatched, tool+"("+status+")")
		}
	}

	if len(mismatched) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: hook_runtime out of sync in %s checkout; run: %s; drift: %s",
		errParentArtifactDrift,
		parentCheckoutLocation(paths, options),
		parentInstallCommand(options),
		strings.Join(mismatched, " "),
	)
}

func compareParentHookRuntimeExecutable(source, destination string) (string, error) {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return "", fmt.Errorf("stat source executable %s: %w", source, err)
	}

	if !sourceInfo.Mode().IsRegular() || sourceInfo.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%w: %s", errParentSourceNotExecutable, source)
	}

	destinationInfo, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}

	if err != nil {
		return "", fmt.Errorf("stat installed executable %s: %w", destination, err)
	}

	if !destinationInfo.Mode().IsRegular() {
		return "not_regular", nil
	}

	if destinationInfo.Mode()&0o111 == 0 {
		return "not_executable", nil
	}

	if sourceInfo.Size() != destinationInfo.Size() {
		return "content", nil
	}

	sourceHash, err := parentFileSHA256(source)
	if err != nil {
		return "", err
	}

	destinationHash, err := parentFileSHA256(destination)
	if err != nil {
		return "", err
	}

	if sourceHash != destinationHash {
		return "content", nil
	}

	return "", nil
}

func parentFileSHA256(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("open executable %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()

	_, err = io.Copy(hash, file)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("hash executable %s: %w", path, err)
	}

	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))

	return digest, nil
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
	existingHash, err := policy.HashBundle(existing)
	if err != nil {
		return false
	}

	return existingMetadata.BundleHash == existingHash &&
		samePolicyIDs(existing.Policies, expected.Policies) &&
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

func firstNonEmptyPath(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func checkParentGoTools(paths runtimePaths) error {
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
		"%w: run `make build` in %s before parent workflows; tools: %s",
		errParentGoToolsStale,
		paths.EthosRoot,
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
	var builder strings.Builder
	builder.WriteString("tool: " + hookoutput.TOONCell(tool) + "\n")
	builder.WriteString("status: " + hookoutput.TOONCell(status) + "\n")
	builder.WriteString("repo: " + hookoutput.TOONCell(repo) + "\n")
	fmt.Fprintf(&builder, "steps[%d]{name,status,detail}:\n", len(steps))

	for _, step := range steps {
		fmt.Fprintf(
			&builder,
			"  %s,%s,%s\n",
			hookoutput.TOONCell(step.Name),
			hookoutput.TOONCell(step.Status),
			hookoutput.TOONFindingCell(step.Detail),
		)
	}

	feedback.EmitRendered(
		os.Stdout,
		strings.TrimSuffix(builder.String(), "\n"),
		feedback.FormatTOON,
	)
}

func parentLintArgs(paths runtimePaths, options parentWorkflowOptions) []string {
	scope := firstNonBlank(options.Scope, parentDefaultLintScope)
	args := []string{
		"--bundle",
		parentPolicyBundlePath(paths, options),
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

func parentPolicySyncCommand(options parentWorkflowOptions) string {
	if options.StateRoot == "" {
		return parentInstallCommand(options)
	}

	return "coding-ethos/bin/coding-ethos-run runtime-policy sync --repo " +
		shellquote.Arg(options.Repo) +
		" --state-root " +
		shellquote.Arg(options.StateRoot)
}

func parentCheckoutLocation(paths runtimePaths, options parentWorkflowOptions) string {
	if sameCleanPath(options.Repo, paths.EthosRoot) {
		return parentCheckoutCodingEthos
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
