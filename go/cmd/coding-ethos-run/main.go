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
	if len(os.Args) > 1 && os.Args[1] != "cutover" && os.Getenv("CODE_ETHOS_HOOK_LOGGING_ACTIVE") == "" {
		execTool(paths, "coding-ethos-hook-log", append([]string{
			"--root", paths.Root,
			"--bundle-root", paths.BundleRoot,
			"--git", paths.RealGit,
			"--", paths.RunBinary,
		}, os.Args[1:]...)...)
	}

	if err := run(paths, os.Args[1:]); err != nil {
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
	hooksDir, err := gitOutput(realGit, root, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
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
		"INVOCATION_CWD":             paths.InvocationCWD,
		"CODE_ETHOS_PRECOMMIT_ROOT":  paths.BundleRoot,
		"CODE_ETHOS_CONSUMER_ROOT":   paths.Root,
		"CODING_ETHOS_RUN_GO_HOOK":   paths.RunBinary,
		"GIT_HOOK_SRC_DIR":           filepath.Join(paths.BundleRoot, "hooks", "go-hooks"),
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

func run(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
		execPath(paths.GitHookRunner)
	}
	command := args[0]
	rest := args[1:]
	switch command {
	case "agent-hook":
		requirePolicyBundle(paths)
		installGitWrapperShim(paths)
		installLintToolShims(paths)
		persistAgentEnvironment(paths)
		_ = os.Setenv("CODING_ETHOS_GIT_SHIM_DIR", paths.BinDir)
		execTool(paths, "coding-ethos-hook", append([]string{"--bundle", paths.PolicyBundle, "--json"}, rest...)...)
	case "git-hook":
		return runGitHook(paths, rest)
	case "agent-hooks":
		installGitWrapperShim(paths)
		installLintToolShims(paths)
		_ = os.Setenv("CODE_ETHOS_CONSUMER_ROOT", rootFlagValue(rest, paths.Root))
		execTool(paths, "coding-ethos-agent-hooks", withDefaultHookCommand(paths, rest)...)
	case "cutover":
		return runCutover(paths, rest)
	case "policy-lint":
		requirePolicyBundle(paths)
		execTool(paths, "coding-ethos-lint", append([]string{"--bundle", paths.PolicyBundle}, rest...)...)
	case "policy":
		execTool(paths, "coding-ethos-policy", rest...)
	case "policy-tool":
		if len(rest) == 0 {
			return errors.New("policy-tool requires a tool name")
		}
		requirePolicyBundle(paths)
		toolName := rest[0]
		toolArgs := rest[1:]
		execTool(paths, "coding-ethos-lint", policyToolLintArgs(paths, toolName, toolArgs)...)
	case "policy-git":
		requirePolicyBundle(paths)
		installGitWrapperShim(paths)
		execTool(paths, "coding-ethos-git", append([]string{"--bundle", paths.PolicyBundle}, rest...)...)
	case "mcp":
		requirePolicyBundle(paths)
		requireRuntimeBinary(filepath.Join(paths.BinDir, "coding-ethos-lint"), "coding-ethos-lint")
		execTool(paths, "coding-ethos-mcp", append([]string{
			"--bundle", paths.PolicyBundle,
			"--ethos-root", paths.EthosRoot,
			"--consumer-root", paths.Root,
			"--invocation-cwd", paths.InvocationCWD,
			"--lint-binary", filepath.Join(paths.BinDir, "coding-ethos-lint"),
		}, rest...)...)
	default:
		requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
		execPath(paths.GitHookRunner, args...)
	}

	return nil
}

func policyToolLintArgs(paths runtimePaths, toolName string, toolArgs []string) []string {
	lintArgs := []string{
		"--bundle", paths.PolicyBundle,
		"--managed-capture-tool", toolName,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
	}
	if sandboxMode := policyToolSandboxModeFromEnv(); sandboxMode != "" {
		lintArgs = append(lintArgs, "--sandbox-mode", sandboxMode)
	}
	lintArgs = append(lintArgs, "--")
	lintArgs = append(lintArgs, toolArgs...)

	return lintArgs
}

func policyToolSandboxModeFromEnv() string {
	if strings.TrimSpace(os.Getenv("CODING_ETHOS_POLICY_TOOL_SHIM")) == "" {
		return ""
	}

	return strings.TrimSpace(os.Getenv("CODING_ETHOS_SANDBOX_MODE"))
}

func withDefaultHookCommand(paths runtimePaths, args []string) []string {
	if hasFlag(args, "--hook-command") {
		return args
	}

	next := append([]string(nil), args...)
	next = append(next, "--hook-command", paths.RunBinary+" agent-hook")

	return next
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}

	return false
}

func rootFlagValue(args []string, fallback string) string {
	for index, arg := range args {
		if arg == "--root" && index+1 < len(args) {
			return args[index+1]
		}
		if value, ok := strings.CutPrefix(arg, "--root="); ok {
			return value
		}
	}

	return fallback
}

func runGitHook(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		return errors.New("git-hook requires a hook name")
	}
	requirePolicyBundle(paths)
	if args[0] == "validate" {
		requireRuntimeFile(paths.PolicyMetadata, "compiled policy metadata")
		runTool(paths, "coding-ethos-policy", "validate-metadata", "--metadata", paths.PolicyMetadata)
	}
	switch args[0] {
	case "pre-commit", "pre-push", "commit-msg", "validate":
	default:
		return fmt.Errorf("unknown git hook %q", args[0])
	}
	requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
	installLintToolShims(paths)
	execTool(paths, "coding-ethos-git-hook", append([]string{
		"--bundle", paths.PolicyBundle,
		"--runner", paths.GitHookRunner,
		"--cwd", paths.Root,
	}, args...)...)

	return nil
}

func runCutover(paths runtimePaths, args []string) error {
	action := "verify"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "verify":
		execCutoverVerify(paths, "verify")
	case "install":
		runTool(paths, "coding-ethos-toolchain", "install-git-hooks", "--hooks-dir", paths.HooksDir, "--source-dir", filepath.Join(paths.BundleRoot, "hooks"))
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
		"--source-dir", filepath.Join(paths.BundleRoot, "hooks"),
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

func requirePolicyBundle(paths runtimePaths) {
	requireRuntimeFile(paths.PolicyBundle, "compiled policy bundle")
}

func requireRuntimeFile(path string, description string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		runtimeFailure(fmt.Sprintf("missing %s: %s", description, path))
	}
}

func requireRuntimeBinary(path string, description string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		runtimeFailure(fmt.Sprintf("missing or non-executable %s: %s", description, path))
	}
}

func runtimeFailure(problem string) {
	fmt.Fprintln(os.Stderr, "FATAL: coding-ethos hook runtime is missing or invalid")
	fmt.Fprintln(os.Stderr, "This is not caused by the files being committed.")
	fmt.Fprintf(os.Stderr, "problem: %s\n", problem)
	fmt.Fprintln(os.Stderr, "action: run make build, or ask an admin to repair the coding-ethos checkout.")
	os.Exit(exitMissing)
}

func gitOutput(realGit string, dir string, args ...string) (string, error) {
	command := exec.Command(realGit, args...)
	if dir != "" {
		command.Dir = dir
	}
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func runTool(paths runtimePaths, tool string, args ...string) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	command := exec.Command(toolPath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		exitErr(err)
	}
}

func execTool(paths runtimePaths, tool string, args ...string) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	execPath(toolPath, args...)
}

func execPath(path string, args ...string) {
	command := exec.Command(path, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		exitErr(err)
	}
	os.Exit(0)
}

func exitErr(err error) {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
