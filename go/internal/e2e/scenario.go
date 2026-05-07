// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	commandTimeout = 30 * time.Second
	e2eDirMode     = 0o700
	e2eFileMode    = 0o600
)

// Repo is an isolated checkout copied from a checked-in reference repository.
type Repo struct {
	Root      string
	EthosRoot string
}

// CommandResult captures the observable result of a real command execution.
type CommandResult struct {
	Cwd      string
	Stdout   string
	Stderr   string
	Combined string
	Args     []string
	Code     int
}

// FromReference copies a checked-in reference repository into a temporary
// directory and initializes it as a real Git repository.
func FromReference(t *testing.T, ethosRoot, reference string) Repo {
	t.Helper()

	source := filepath.Join(ethosRoot, "examples", "reference-repos", reference)

	info, inlineErrAutoA := os.Stat(source)
	if inlineErrAutoA != nil || !info.IsDir() {
		t.Fatalf("reference repo %q is unavailable: %v", reference, inlineErrAutoA)
	}

	root := filepath.Join(t.TempDir(), reference)

	err := copyTree(root, source)
	if err != nil {
		t.Fatalf("copy reference repo: %v", err)
	}

	repo := Repo{Root: root, EthosRoot: ethosRoot}
	repo.Git(t, "init")
	repo.Git(t, "config", "user.email", "e2e@example.com")
	repo.Git(t, "config", "user.name", "E2E Test")
	repo.Git(t, "config", "commit.gpgsign", "false")
	repo.Git(t, "add", ".")
	repo.Git(t, "commit", "-m", "test(repo): initialize reference repo")

	return repo
}

// RequireRuntime skips e2e scenarios unless the caller opted into real
// workflow execution. These tests run real managed tools and repository
// commands, so broad unit-test sweeps should invoke them through make targets
// that first prepare the runtime.
func RequireRuntime(t *testing.T, ethosRoot string) {
	t.Helper()

	if testing.Short() {
		t.Skip("real workflow e2e tests are skipped in -short mode")
	}

	required := []string{
		filepath.Join(ethosRoot, "bin", "coding-ethos-run"),
		filepath.Join(ethosRoot, "bin", "coding-ethos-lint"),
		filepath.Join(ethosRoot, "bin", "coding-ethos-policy"),
		filepath.Join(ethosRoot, "bin", "coding-ethos-hook-log"),
		filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json"),
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			t.Skipf("runtime artifact missing; run make build first: %s", path)
		}
	}
}

// InstrumentedEthosRoot returns a temporary runtime root with coverage-enabled
// coding-ethos binaries when GOCOVERDIR is active. This lets real subprocess
// e2e scenarios contribute Go coverage without replacing the managed tools they
// execute.
func InstrumentedEthosRoot(t *testing.T, ethosRoot string) string {
	t.Helper()

	coverDir := strings.TrimSpace(os.Getenv("GOCOVERDIR"))
	if coverDir == "" {
		return ethosRoot
	}

	absoluteCoverageDir(t, coverDir)

	if runtime.GOOS == "windows" {
		t.Skip("instrumented e2e runtime uses POSIX symlinks")
	}

	runtimeRoot := filepath.Join(t.TempDir(), "coding-ethos-runtime")

	err := os.MkdirAll(filepath.Join(runtimeRoot, "bin"), e2eDirMode)
	if err != nil {
		t.Fatalf("create instrumented runtime bin: %v", err)
	}

	for _, entry := range []string{
		"build",
		"config.yaml",
		"coding_ethos.yml",
		"repo_ethos.yml",
	} {
		err = os.Symlink(
			filepath.Join(ethosRoot, entry),
			filepath.Join(runtimeRoot, entry),
		)
		if err != nil {
			t.Fatalf("symlink %s into instrumented runtime: %v", entry, err)
		}
	}

	err = copyRuntimePreCommit(runtimeRoot, ethosRoot)
	if err != nil {
		t.Fatalf("copy pre-commit runtime tree: %v", err)
	}

	for _, command := range []string{
		"coding-ethos-run",
		"coding-ethos-lint",
		"coding-ethos-policy",
		"coding-ethos-hook-log",
	} {
		buildInstrumentedCommand(t, ethosRoot, runtimeRoot, command)
	}

	return runtimeRoot
}

func absoluteCoverageDir(t *testing.T, coverDir string) string {
	t.Helper()

	if filepath.IsAbs(coverDir) {
		return coverDir
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve working directory for GOCOVERDIR: %v", err)
	}

	absolute := filepath.Clean(filepath.Join(cwd, coverDir))

	err = os.MkdirAll(absolute, e2eDirMode)
	if err != nil {
		t.Fatalf("create GOCOVERDIR: %v", err)
	}

	return absolute
}

func commandEnvironment(t *testing.T) []string {
	t.Helper()

	env := os.Environ()
	for index, entry := range env {
		value, ok := strings.CutPrefix(entry, "GOCOVERDIR=")
		if ok && strings.TrimSpace(value) != "" {
			env[index] = "GOCOVERDIR=" + absoluteCoverageDir(t, value)
		}
	}

	return env
}

// Run executes a real command in the reference repository.
func (repo Repo) Run(t *testing.T, args ...string) CommandResult {
	t.Helper()

	return Run(t, repo.Root, args...)
}

// Git executes /usr/bin/git in the reference repository.
func (repo Repo) Git(t *testing.T, args ...string) CommandResult {
	t.Helper()

	return repo.Run(t, append([]string{"/usr/bin/git"}, args...)...)
}

// CodingEthosRun executes the built coding-ethos dispatcher against this repo.
func (repo Repo) CodingEthosRun(t *testing.T, args ...string) CommandResult {
	t.Helper()

	binary := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-run")
	command := append([]string{binary}, args...)
	result := repo.Run(t, command...)

	return result
}

// Run executes a real command with a bounded timeout.
func Run(t *testing.T, cwd string, args ...string) CommandResult {
	t.Helper()

	if len(args) == 0 {
		t.Fatal("missing command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := safeexec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Env = commandEnvironment(t)
	configureCommandProcessGroup(cmd)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0

	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case ctx.Err() != nil:
			terminateCommandProcessGroup(cmd)
			t.Fatalf("command timed out: %s", strings.Join(args, " "))
		case errors.As(err, &exitErr):
			code = exitErr.ExitCode()
		default:
			t.Fatalf("run command %q: %v", strings.Join(args, " "), err)
		}
	}

	return CommandResult{
		Args:     append([]string(nil), args...),
		Cwd:      cwd,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Combined: stdout.String() + stderr.String(),
		Code:     code,
	}
}

// RequireExit fails when the command exit code differs from the expected code.
func (result CommandResult) RequireExit(t *testing.T, want int) {
	t.Helper()

	if result.Code != want {
		t.Fatalf(
			"exit code = %d, want %d\ncommand: %s\nstdout:\n%s\nstderr:\n%s",
			result.Code,
			want,
			strings.Join(result.Args, " "),
			result.Stdout,
			result.Stderr,
		)
	}
}

// RequireContains fails when the combined output does not include text.
func (result CommandResult) RequireContains(t *testing.T, text string) {
	t.Helper()

	if !strings.Contains(result.Combined, text) {
		t.Fatalf(
			"output missing %q\ncommand: %s\nstdout:\n%s\nstderr:\n%s",
			text,
			strings.Join(result.Args, " "),
			result.Stdout,
			result.Stderr,
		)
	}
}

// TraceFiles returns retained lint trace files for the reference repo.
func (repo Repo) TraceFiles(t *testing.T) []string {
	t.Helper()

	matches, err := filepath.Glob(
		filepath.Join(repo.Root, ".coding-ethos", "lint-runs", "*.json"),
	)
	if err != nil {
		t.Fatalf("glob trace files: %v", err)
	}

	return matches
}

// SingleTrace returns the only retained lint trace content.
func (repo Repo) SingleTrace(t *testing.T) string {
	t.Helper()

	matches := repo.TraceFiles(t)
	if len(matches) != 1 {
		t.Fatalf("trace files = %#v, want exactly one", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace %s: %v", matches[0], err)
	}

	return string(content)
}

// ResetTraces removes retained lint traces between scenarios.
func (repo Repo) ResetTraces(t *testing.T) {
	t.Helper()

	err := os.RemoveAll(filepath.Join(repo.Root, ".coding-ethos"))
	if err != nil {
		t.Fatalf("remove trace directory: %v", err)
	}
}

// Touch rewrites a file so tests can create controlled changes in the repo.
func (repo Repo) Touch(t *testing.T, path, content string) {
	t.Helper()

	fullPath := filepath.Join(repo.Root, filepath.FromSlash(path))

	err := os.MkdirAll(filepath.Dir(fullPath), e2eDirMode)
	if err != nil {
		t.Fatalf("create parent directory: %v", err)
	}

	err = os.WriteFile(fullPath, []byte(content), e2eFileMode)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func copyTree(destination, source string) error {
	return copyTreeFiltered(destination, source, nil)
}

func copyRuntimePreCommit(runtimeRoot, ethosRoot string) error {
	return copyTreeFiltered(
		filepath.Join(runtimeRoot, "pre-commit"),
		filepath.Join(ethosRoot, "pre-commit"),
		isGeneratedRuntimeState,
	)
}

func isGeneratedRuntimeState(rel string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		switch part {
		case ".venv", ".ruff_cache", "__pycache__":
			return true
		}
	}

	return false
}

func copyTreeFiltered(
	destination string,
	source string,
	skip func(string) bool,
) error {
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return fmt.Errorf("open reference fixture source %s: %w", source, err)
	}
	defer sourceRoot.Close()

	err = os.MkdirAll(destination, e2eDirMode)
	if err != nil {
		return fmt.Errorf("create reference fixture destination %s: %w", destination, err)
	}

	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open reference fixture destination %s: %w", destination, err)
	}
	defer destinationRoot.Close()

	err = filepath.WalkDir(
		source,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk reference fixture path %s: %w", path, walkErr)
			}

			return copyTreeEntry(
				path,
				source,
				sourceRoot,
				destinationRoot,
				entry,
				skip,
			)
		},
	)
	if err != nil {
		return fmt.Errorf("copy reference fixture tree from %s: %w", source, err)
	}

	return nil
}

func copyTreeEntry(
	path string,
	source string,
	sourceRoot *os.Root,
	destinationRoot *os.Root,
	entry fs.DirEntry,
	skip func(string) bool,
) error {
	rel, err := filepath.Rel(source, path)
	if err != nil {
		return fmt.Errorf("relativize reference fixture path %s: %w", path, err)
	}

	if rel != "." && skip != nil && skip(rel) {
		if entry.IsDir() {
			return filepath.SkipDir
		}

		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("inspect reference fixture path %s: %w", path, err)
	}

	if entry.IsDir() {
		err = destinationRoot.MkdirAll(rel, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("create reference fixture directory %s: %w", path, err)
		}

		return nil
	}

	if !info.Mode().IsRegular() {
		return apperror.Wrapf(
			apperror.StaticError("unsupported reference entry %s"),
			"unsupported reference entry %s",
			path,
		)
	}

	content, err := sourceRoot.ReadFile(rel)
	if err != nil {
		return fmt.Errorf("read reference fixture path %s: %w", path, err)
	}

	err = destinationRoot.WriteFile(rel, content, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("write reference fixture path %s: %w", path, err)
	}

	return nil
}

func buildInstrumentedCommand(t *testing.T, ethosRoot, runtimeRoot, command string) {
	t.Helper()

	args := []string{
		"build",
		"-cover",
		"-coverpkg=./...",
		"-buildvcs=false",
		"-o",
		filepath.Join(runtimeRoot, "bin", command),
		"./cmd/" + command,
	}
	result := Run(t, filepath.Join(ethosRoot, "go"), append([]string{"go"}, args...)...)
	result.RequireExit(t, 0)
}
