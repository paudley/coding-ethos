// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"
)

func runCutover(paths runtimePaths, args []string) error {
	action := "verify"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "verify":
		execCutoverVerify(paths, "verify")
	case "install":
		runTool(paths, "coding-ethos-toolchain", "install-git-hooks", "--hooks-dir", paths.HooksDir, "--runner", paths.RunBinary)
		runTool(paths, "coding-ethos-agent-hooks", "sync", "--root", paths.Root)
		execCutoverVerify(paths, "install")
	default:
		return fmt.Errorf("unknown cutover action %q", action)
	}

	return nil
}

func execCutoverVerify(paths runtimePaths, action string) {
	execTool(paths, "coding-ethos-toolchain",
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
	runTool(paths, "coding-ethos-toolchain",
		"install-git-shim",
		"--dest-dir", paths.BinDir,
		"--real-git", paths.RealGit,
		"--runner", paths.RunBinary,
	)
}

func installLintToolShims(paths runtimePaths) {
	runTool(paths, "coding-ethos-lint",
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
	file, err := os.OpenFile(envFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		exitErr(fmt.Errorf("open Claude env file %s: %w", envFile, err))
	}
	_, err = fmt.Fprintf(
		file,
		"export CODING_ETHOS_REAL_GIT=%q\nexport CODING_ETHOS_RUN_GO_HOOK=%q\nexport PATH=%q:\"$PATH\"\n",
		paths.RealGit,
		paths.RunBinary,
		paths.BinDir,
	)
	if err != nil {
		_ = file.Close()
		exitErr(fmt.Errorf("write Claude env file %s: %w", envFile, err))
	}
	if err := file.Close(); err != nil {
		exitErr(fmt.Errorf("close Claude env file %s: %w", envFile, err))
	}
}
