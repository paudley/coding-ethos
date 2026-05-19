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
	paths       *sandboxPaths
	writePaths  []string
	commandArgv []string
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

		fmt.Fprintln(os.Stderr, sandboxExecErrorPrefix, err)

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
		parsed     = options{paths: &sandboxPaths{}}
		writePaths repeatedPaths
	)

	flags := flag.NewFlagSet(sandboxExecCommandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	flags.StringVar(&parsed.paths.cwd, "cwd", "", "Sandbox working directory")
	flags.StringVar(&parsed.paths.repoRoot, "repo-root", "", "Repository root")
	flags.Var(&writePaths, "write-path", "Writable repository path")

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
		name == "GIT_WORK_TREE"
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

func cleanPolicyPath(repoRoot, path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}

	if filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if allowedSystemWritePath(clean) {
			return clean, true
		}

		return clean, pathWithin(repoRoot, clean)
	}

	clean := filepath.Clean(filepath.Join(repoRoot, path))

	return clean, pathWithin(repoRoot, clean)
}

func allowedSystemWritePath(path string) bool {
	return path == os.DevNull ||
		allowedManagedTempWritePath(path) ||
		allowedGPGRuntimeWritePath(path)
}

func allowedManagedTempWritePath(path string) bool {
	tempRoot := filepath.Clean(os.TempDir())
	if !pathWithin(tempRoot, path) {
		return false
	}

	return strings.HasPrefix(filepath.Base(path), "coding-ethos-go-test-")
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

func joinPolicyErrors(errs ...error) error {
	return errors.Join(errs...)
}
