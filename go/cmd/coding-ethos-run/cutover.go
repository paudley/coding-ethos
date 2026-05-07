// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const agentEnvFileMode = 0o600

func runCutover(paths runtimePaths, args []string) error {
	action := "verify"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "verify":
		execCutoverVerify(paths, "verify")
	case "install":
		runtimeRunTool(
			paths,
			"coding-ethos-toolchain",
			"install-git-hooks",
			"--hooks-dir",
			paths.HooksDir,
			"--runner",
			paths.RunBinary,
		)
		runtimeRunTool(paths, "coding-ethos-agent-hooks", "sync", "--root", paths.Root)
		execCutoverVerify(paths, "install")
	default:
		return apperror.Wrapf(
			apperror.StaticError("unknown cutover action %q"),
			"unknown cutover action %q",
			action,
		)
	}

	return nil
}

func execCutoverVerify(paths runtimePaths, action string) {
	runtimeExecTool(paths, "coding-ethos-toolchain",
		"cutover-verify",
		"--action", action,
		"--root", paths.Root,
		"--runner", paths.RunBinary,
		"--hooks-dir", paths.HooksDir,
		"--real-git", paths.RealGit,
		"--bundle-root", paths.BundleRoot,
	)
}

func installGitWrapperShim(paths runtimePaths) {
	runtimeRunTool(paths, "coding-ethos-toolchain",
		"install-git-shim",
		"--dest-dir", paths.BinDir,
		"--real-git", paths.RealGit,
		"--runner", paths.RunBinary,
	)
}

func installLintToolShims(paths runtimePaths) {
	runtimeRunLint(paths,
		"--install-shims",
		"--tools-bin-dir", paths.BinDir,
		"--runner", paths.RunBinary,
		"--ethos-root", paths.EthosRoot,
	)
}

func persistAgentEnvironment(paths runtimePaths) {
	envFile := strings.TrimSpace(os.Getenv("CLAUDE_ENV_FILE"))
	if envFile == "" {
		return
	}

	installGitWrapperShim(paths)

	file, err := openAgentEnvFile(envFile)
	if err != nil {
		exitErr(fmt.Errorf("open Claude env file %s: %w", envFile, err))
	}

	_, err = fmt.Fprintf(
		file,
		"export CODING_ETHOS_REAL_GIT=%q\n"+
			"export CODING_ETHOS_RUN_GO_HOOK=%q\n"+
			"export PATH=%q:\"$PATH\"\n",
		paths.RealGit,
		paths.RunBinary,
		paths.BinDir,
	)
	if err != nil {
		_ = file.Close()

		exitErr(fmt.Errorf("write Claude env file %s: %w", envFile, err))
	}

	inlineErr0 := file.Close()
	if inlineErr0 != nil {
		exitErr(fmt.Errorf("close Claude env file %s: %w", envFile, inlineErr0))
	}
}

func openAgentEnvFile(envFile string) (*os.File, error) {
	rootPath := filepath.Dir(envFile)

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", rootPath, err)
	}
	defer root.Close()

	file, err := root.OpenFile(
		filepath.Base(envFile),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		agentEnvFileMode,
	)
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}

	return file, nil
}
