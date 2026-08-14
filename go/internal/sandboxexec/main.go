// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package sandboxexec applies in-namespace sandbox policy and execs a tool.
package sandboxexec

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

var errSandboxExecCommand = apperror.StaticError("sandbox exec requires command")

const (
	sandboxExecFailureExitCode = 126
	sandboxExecCommandName     = "coding-ethos-sandbox"
	sandboxExecErrorPrefix     = "coding-ethos-sandbox:"
)

type repeatedPaths []string

func (paths *repeatedPaths) String() string {
	return strings.Join(*paths, string(os.PathListSeparator))
}

func (paths *repeatedPaths) Set(value string) error {
	*paths = append(*paths, value)

	return nil
}

type options struct {
	paths          *sandboxPaths
	realGitPath    string
	realGitBind    string
	gitWrapper     string
	gitTargets     []string
	writePaths     []string
	readOnlyPaths  []string
	commandArgv    []string
	allowGitWrites bool
}

type sandboxPaths struct {
	cwd      string
	repoRoot string
}

// Run parses sandbox helper arguments, applies policy, and replaces the
// current process with the requested command.
func Run(args []string) int {
	err := run(args)
	if err != nil {
		if exitErr, ok := err.(interface{ ExitCode() int }); ok {
			return exitErr.ExitCode()
		}

		feedback.Emit(
			os.Stderr,
			feedback.Error{Message: sandboxExecErrorPrefix + " " + err.Error()},
			feedback.FormatTOON,
		)

		return sandboxExecFailureExitCode
	}

	return 0
}

func run(args []string) error {
	config, err := parseOptions(args)
	if err != nil {
		return err
	}

	err = applyFilesystemPolicy(config)
	if err != nil {
		return fmt.Errorf("apply native sandbox filesystem policy: %w", err)
	}

	err = os.Chdir(config.paths.cwd)
	if err != nil {
		return fmt.Errorf("chdir sandbox working directory: %w", err)
	}

	return execSandboxedCommand(config)
}

func parseOptions(args []string) (options, error) {
	var (
		parsed        = options{paths: &sandboxPaths{}}
		gitTargets    repeatedPaths
		writePaths    repeatedPaths
		readOnlyPaths repeatedPaths
	)

	flags := flag.NewFlagSet(sandboxExecCommandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	flags.StringVar(&parsed.paths.cwd, "cwd", "", "Sandbox working directory")
	flags.StringVar(&parsed.paths.repoRoot, "repo-root", "", "Repository root")
	flags.StringVar(&parsed.gitWrapper, "git-wrapper", "", "Managed git wrapper")
	flags.StringVar(&parsed.realGitPath, "real-git-path", "", "Real git source path")
	flags.StringVar(&parsed.realGitBind, "real-git-bind", "", "Real git bind target")
	flags.Var(&gitTargets, "git-target", "Git path to bind")
	flags.Var(&writePaths, "write-path", "Writable repository path")
	flags.Var(
		&readOnlyPaths,
		"read-only-path",
		"Repository path that must stay read-only even inside a writable parent",
	)
	flags.BoolVar(
		&parsed.allowGitWrites,
		"allow-git-writes",
		false,
		"Allow declared Git metadata write paths",
	)

	err := flags.Parse(args)
	if err != nil {
		return options{}, fmt.Errorf("parse sandbox exec flags: %w", err)
	}

	parsed.commandArgv = flags.Args()
	if len(parsed.commandArgv) == 0 {
		return options{}, errSandboxExecCommand
	}

	cwd, err := filepath.Abs(firstNonEmpty(parsed.paths.cwd, "."))
	if err != nil {
		return options{}, fmt.Errorf("resolve sandbox cwd: %w", err)
	}

	parsed.paths.cwd = filepath.Clean(cwd)

	repoRoot, err := filepath.Abs(firstNonEmpty(parsed.paths.repoRoot, parsed.paths.cwd))
	if err != nil {
		return options{}, fmt.Errorf("resolve sandbox repo root: %w", err)
	}

	parsed.paths.repoRoot = filepath.Clean(repoRoot)

	parsed.writePaths = append([]string(nil), writePaths...)
	parsed.readOnlyPaths = append([]string(nil), readOnlyPaths...)
	parsed.gitTargets = append([]string(nil), gitTargets...)

	return parsed, nil
}

func sandboxExecEnv(environ []string) []string {
	clean := make([]string, 0, len(environ))
	for _, item := range environ {
		name, _, found := strings.Cut(item, "=")
		if !found || !sandboxExecBlockedEnv(name) {
			clean = append(clean, item)
		}
	}

	return clean
}

func sandboxExecBlockedEnv(name string) bool {
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
		strings.HasPrefix(name, "GIT_CONFIG_VALUE_") ||
		name == "GIT_CONFIG_COUNT" ||
		name == "GIT_CONFIG_PARAMETERS" ||
		name == "GIT_DIR" ||
		name == "GIT_INDEX_FILE" ||
		name == "GIT_WORK_TREE" ||
		name == execguard.EnvStack
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return relative == "." ||
		(!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != "..")
}

func cleanPolicyPath(repoRoot, path string, allowGitWrites bool) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}

	if filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if allowedSystemWritePath(clean) {
			return clean, true
		}

		if allowGitWrites && allowedGitMetadataWritePath(repoRoot, clean) {
			return clean, true
		}

		return clean, pathWithin(repoRoot, clean)
	}

	clean := filepath.Clean(filepath.Join(repoRoot, path))

	return clean, pathWithin(repoRoot, clean)
}

func allowedGitMetadataWritePath(repoRoot, path string) bool {
	for _, root := range gitMetadataRoots(repoRoot) {
		if pathWithin(root, path) {
			return true
		}
	}

	return false
}

func gitMetadataRoots(repoRoot string) []string {
	dotGit := filepath.Join(repoRoot, ".git")

	info, err := os.Stat(dotGit)
	if err == nil && info.IsDir() {
		return []string{dotGit}
	}

	content, err := os.ReadFile(dotGit)
	if err != nil {
		return nil
	}

	gitDir, found := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir:")
	if !found {
		return nil
	}

	gitDir = strings.TrimSpace(gitDir)
	if gitDir == "" {
		return nil
	}

	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}

	gitDir = filepath.Clean(gitDir)

	roots := []string{gitDir}
	if strings.Contains(filepath.ToSlash(gitDir), "/.git/worktrees/") {
		for current := gitDir; current != filepath.Dir(current); {
			if filepath.Base(current) == "worktrees" {
				roots = append(roots, filepath.Dir(current))

				break
			}

			current = filepath.Dir(current)
		}
	}

	return roots
}

func allowedSystemWritePath(path string) bool {
	return path == os.DevNull ||
		allowedTerminalWritePath(path) ||
		allowedManagedTempWritePath(path) ||
		allowedGPGHomeWritePath(path) ||
		allowedGPGRuntimeWritePath(path)
}

func allowedTerminalWritePath(path string) bool {
	for _, fd := range []string{"0", "1", "2"} {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", fd))
		if err != nil {
			continue
		}

		if filepath.Clean(target) == path {
			return true
		}
	}

	return false
}

func allowedManagedTempWritePath(path string) bool {
	tempRoot := resolvedTempRoot()
	if !pathWithin(tempRoot, path) {
		return false
	}

	return strings.HasPrefix(filepath.Base(path), "coding-ethos-go-test-")
}

func resolvedTempRoot() string {
	tempRoot := filepath.Clean(os.TempDir())

	resolved, err := filepath.EvalSymlinks(tempRoot)
	if err == nil {
		return filepath.Clean(resolved)
	}

	return tempRoot
}

func allowedGPGRuntimeWritePath(path string) bool {
	defaultRoot := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	if pathWithin(filepath.Join(defaultRoot, "gnupg"), path) {
		return true
	}

	runtimeRoot := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeRoot == "" {
		return false
	}

	return pathWithin(filepath.Join(runtimeRoot, "gnupg"), path)
}

func allowedGPGHomeWritePath(path string) bool {
	gpgHome := strings.TrimSpace(os.Getenv("GNUPGHOME"))
	if gpgHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return false
		}

		gpgHome = filepath.Join(home, ".gnupg")
	}

	gpgHome = filepath.Clean(gpgHome)
	if pathWithin(gpgHome, path) {
		return true
	}

	for _, root := range resolvedGPGHomeWriteRoots(gpgHome) {
		if pathWithin(root, path) {
			return true
		}
	}

	return false
}

func resolvedGPGHomeWriteRoots(gpgHome string) []string {
	roots := []string{}
	seen := map[string]bool{}
	appendRoot := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || seen[path] {
			return
		}

		seen[path] = true
		roots = append(roots, path)
	}

	resolvedHome, err := filepath.EvalSymlinks(gpgHome)
	if err == nil {
		appendRoot(resolvedHome)
	}

	appendResolvedGPGSymlinkRoots(gpgHome, appendRoot)
	appendResolvedGPGSymlinkRoots(filepath.Join(gpgHome, "private-keys-v1.d"), appendRoot)

	return roots
}

func appendResolvedGPGSymlinkRoots(dir string, appendRoot func(string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		info, err := os.Lstat(entryPath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		resolved, err := filepath.EvalSymlinks(entryPath)
		if err != nil {
			continue
		}

		appendResolvedGPGPath(resolved, appendRoot)
	}
}

func appendResolvedGPGPath(resolved string, appendRoot func(string)) {
	resolvedInfo, err := os.Stat(resolved)
	if err == nil && resolvedInfo.IsDir() {
		appendRoot(resolved)

		return
	}

	appendRoot(filepath.Dir(resolved))
}

func joinPolicyErrors(errs ...error) error {
	return errors.Join(errs...)
}
