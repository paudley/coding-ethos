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

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/hooklog"
)

const (
	exitMissing = 127
)

type runtimePaths struct {
	Executor         runtimeExecutor
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

func (paths runtimePaths) executor() runtimeExecutor {
	if paths.Executor == nil {
		return defaultRuntimeExecutor{}
	}

	return paths.Executor
}

func main() {
	execguard.Enter("coding-ethos-run")

	os.Exit(mainExitCode())
}

func mainExitCode() int {
	return withRuntimeExit(func() int {
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
			loggedCode, logErr := hooklog.RunInProcess(hooklog.Options{
				Stdin:      os.Stdin,
				Stdout:     os.Stdout,
				Stderr:     os.Stderr,
				GitPath:    paths.RealGit,
				Root:       paths.Root,
				BundleRoot: paths.BundleRoot,
				Command:    append([]string{paths.RunBinary}, args...),
			}, func() int {
				return runRuntime(paths, args)
			})
			if logErr != nil {
				exitErr(logErr)
			}

			return loggedCode
		}

		return runRuntime(paths, args)
	})
}

func runRuntime(paths runtimePaths, args []string) int {
	return withRuntimeExit(func() int {
		inlineErr0 := run(paths, args)
		if inlineErr0 != nil {
			exitErr(inlineErr0)
		}

		return 0
	})
}

func withRuntimeExit(action func() int) int {
	code := 0

	func() {
		defer captureRuntimeExit(&code)

		code = action()
	}()

	return code
}

func resolveRuntimePaths() (runtimePaths, error) {
	realGit, err := resolveRuntimeGit()
	if err != nil {
		return runtimePaths{}, err
	}

	invocationCWD, err := os.Getwd()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("get invocation cwd: %w", err)
	}

	root, localRoot := resolveRuntimeRoot(realGit, invocationCWD)
	hooksDir := resolveRuntimeHooksDir(realGit, root)

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

	return runtimePathSet(
		runtimePathInputs{
			RealGit:       realGit,
			InvocationCWD: invocationCWD,
			LocalRoot:     localRoot,
			Root:          root,
			HooksDir:      hooksDir,
			BinDir:        binDir,
			RunBinary:     runBinary,
			BundleRoot:    bundleRoot,
			EthosRoot:     ethosRoot,
			ToolchainDir:  toolchainDir,
		},
	), nil
}

type runtimePathInputs struct {
	RealGit       string
	InvocationCWD string
	LocalRoot     string
	Root          string
	HooksDir      string
	BinDir        string
	RunBinary     string
	BundleRoot    string
	EthosRoot     string
	ToolchainDir  string
}

func resolveRuntimeRoot(realGit, invocationCWD string) (string, string) {
	root := strings.TrimSpace(os.Getenv("CODE_ETHOS_CONSUMER_ROOT"))
	if root != "" {
		return root, root
	}

	localRoot := invocationCWD

	resolvedRoot, err := gitOutput(realGit, "", "rev-parse", "--show-toplevel")
	if err == nil {
		localRoot = resolvedRoot
		superRoot, superErr := gitOutput(
			realGit,
			resolvedRoot,
			"rev-parse",
			"--show-superproject-working-tree",
		)
		if superErr == nil && strings.TrimSpace(superRoot) != "" {
			return superRoot, localRoot
		}
	}

	return localRoot, localRoot
}

func resolveRuntimeHooksDir(realGit, root string) string {
	hooksDir, err := gitOutput(
		realGit,
		root,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"hooks",
	)
	if err == nil {
		return hooksDir
	}

	return filepath.Join(root, ".git", "hooks")
}

func runtimePathSet(inputs runtimePathInputs) runtimePaths {
	return runtimePaths{
		RealGit:       inputs.RealGit,
		InvocationCWD: inputs.InvocationCWD,
		LocalRoot:     inputs.LocalRoot,
		Root:          inputs.Root,
		HooksDir:      inputs.HooksDir,
		BinDir:        inputs.BinDir,
		RunBinary:     inputs.RunBinary,
		BundleRoot:    inputs.BundleRoot,
		EthosRoot:     inputs.EthosRoot,
		GitHookRunner: filepath.Join(inputs.BinDir, "coding-ethos-hook-runner"),
		ToolsSource:   filepath.Join(inputs.EthosRoot, "go"),
		PolicyBundle: filepath.Join(
			inputs.EthosRoot,
			"build",
			"policy",
			"policy-bundle.json",
		),
		PolicyMetadata: filepath.Join(
			inputs.EthosRoot,
			"build",
			"policy",
			"policy-metadata.json",
		),
		ManagedGoBin: filepath.Join(
			inputs.ToolchainDir,
			"go-bin",
		),
		ManagedPrefixBin: filepath.Join(
			inputs.ToolchainDir,
			"prefix",
			"bin",
		),
		ManagedGitHubBin: filepath.Join(
			inputs.ToolchainDir,
			"github-bin",
		),
		ManagedManifest: filepath.Join(inputs.ToolchainDir, "manifest.tsv"),
	}
}

func resolveRuntimeGit() (string, error) {
	resolvedGit, err := gitwrap.ResolveRealGit("git")
	if err != nil {
		return "", fmt.Errorf("resolve runtime git: %w", err)
	}

	return resolvedGit, nil
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
	fmt.Fprintln(os.Stderr, err)

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		requestRuntimeExit(exitError.ExitCode())
	}

	requestRuntimeExit(1)
}
