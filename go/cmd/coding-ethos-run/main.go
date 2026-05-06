// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultGitPath = "/usr/bin/git"
	exitMissing    = 127
)

type runtimePaths struct {
	RealGit          string
	InvocationCWD    string
	LocalRoot        string
	Root             string
	HooksDir         string
	BinDir           string
	RunBinary        string
	BundleRoot       string
	EthosRoot        string
	GitHookRunner    string
	ToolsSource      string
	PolicyBundle     string
	PolicyMetadata   string
	ManagedGoBin     string
	ManagedPrefixBin string
	ManagedGitHubBin string
	ManagedManifest  string
}

func main() {
	paths, err := resolveRuntimePaths()
	if err != nil {
		exitErr(err)
	}

	paths.export()

	args := runnerArgs(os.Args)
	if len(args) > 0 &&
		args[0] != "cutover" &&
		args[0] != "lfs-hook" &&
		os.Getenv("CODE_ETHOS_HOOK_LOGGING_ACTIVE") == "" {
		execTool(paths, "coding-ethos-hook-log", append([]string{
			"--root", paths.Root,
			"--bundle-root", paths.BundleRoot,
			"--git", paths.RealGit,
			"--", paths.RunBinary,
		}, args...)...)
	}

	if err := run(paths, args); err != nil {
		exitErr(err)
	}
}

func resolveRuntimePaths() (runtimePaths, error) {
	realGit := strings.TrimSpace(os.Getenv("CODING_ETHOS_REAL_GIT"))
	if realGit == "" {
		realGit = defaultGitPath
	}

	invocationCWD, err := os.Getwd()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("get invocation cwd: %w", err)
	}

	localRoot, err := gitOutput(realGit, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return runtimePaths{}, err
	}

	root := strings.TrimSpace(os.Getenv("CODE_ETHOS_CONSUMER_ROOT"))
	if root == "" {
		root = localRoot
	}

	hooksDir, err := gitOutput(
		realGit,
		root,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"hooks",
	)
	if err != nil {
		return runtimePaths{}, err
	}

	runBinary, err := os.Executable()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("resolve runner executable: %w", err)
	}

	runBinary, err = filepath.EvalSymlinks(runBinary)
	if err != nil {
		return runtimePaths{}, fmt.Errorf("resolve runner symlinks: %w", err)
	}

	binDir := filepath.Dir(runBinary)
	ethosRoot := filepath.Dir(binDir)
	bundleRoot := filepath.Join(ethosRoot, "pre-commit")
	toolchainDir := filepath.Join(ethosRoot, "build", "toolchain")

	return runtimePaths{
		RealGit:          realGit,
		InvocationCWD:    invocationCWD,
		LocalRoot:        localRoot,
		Root:             root,
		HooksDir:         hooksDir,
		BinDir:           binDir,
		RunBinary:        runBinary,
		BundleRoot:       bundleRoot,
		EthosRoot:        ethosRoot,
		GitHookRunner:    filepath.Join(binDir, "coding-ethos-hook-runner"),
		ToolsSource:      filepath.Join(ethosRoot, "go"),
		PolicyBundle:     filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json"),
		PolicyMetadata:   filepath.Join(ethosRoot, "build", "policy", "policy-metadata.json"),
		ManagedGoBin:     filepath.Join(toolchainDir, "go-bin"),
		ManagedPrefixBin: filepath.Join(toolchainDir, "prefix", "bin"),
		ManagedGitHubBin: filepath.Join(toolchainDir, "github-bin"),
		ManagedManifest:  filepath.Join(toolchainDir, "manifest.tsv"),
	}, nil
}

func (paths runtimePaths) export() {
	prependPath := strings.Join([]string{
		paths.ManagedGoBin,
		paths.ManagedPrefixBin,
		paths.ManagedGitHubBin,
		paths.BinDir,
		filepath.Join(paths.Root, ".venv", "bin"),
		os.Getenv("PATH"),
	}, string(os.PathListSeparator))

	setenv := map[string]string{
		"INVOCATION_CWD":            paths.InvocationCWD,
		"CODE_ETHOS_PRECOMMIT_ROOT": paths.BundleRoot,
		"CODE_ETHOS_CONSUMER_ROOT":  paths.Root,
		"CODING_ETHOS_RUN_GO_HOOK":  paths.RunBinary,
		"GIT_HOOK_SRC_DIR": filepath.Join(
			paths.ToolsSource,
			"cmd",
			"coding-ethos-hook-runner",
		),
		"TOOLS_SRC_DIR":              paths.ToolsSource,
		"POLICY_METADATA":            paths.PolicyMetadata,
		"MANAGED_TOOLCHAIN_MANIFEST": paths.ManagedManifest,
		"CODING_ETHOS_REAL_GIT":      paths.RealGit,
		"PATH":                       prependPath,
	}
	for key, value := range setenv {
		_ = os.Setenv(key, value)
	}
}

func exitErr(err error) {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	}

	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
