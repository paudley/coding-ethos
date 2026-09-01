// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

type sandboxCacheEnvironment struct {
	TempDir         string
	RuntimeDir      string
	GoCache         string
	GoPath          string
	GoModCache      string
	GolangCILintDir string
	GoRoot          string
	CGOEnabled      string
	CC              string
	CompilerPath    string
	Assembler       string
	RealGit         string
	PathPrefix      string
	// Cargo resolves dependencies from the registry under CARGO_HOME and finds
	// its toolchain through RUSTUP_HOME. Neither can be rebuilt inside the
	// sandbox without network access, and an installation away from its
	// default path is invisible to a command that inherits no environment —
	// cargo is on PATH, runs, and reports no toolchain.
	CargoHome            string
	RustupHome           string
	UVProjectEnvironment string
	// Trailing so the single bool pads the struct once rather than splitting
	// the string fields around it.
	CleanupTemp bool
}

type sandboxGoCachePaths struct {
	Cache        string
	Path         string
	ModCache     string
	GolangCILint string
}

// rustHomes reports where Cargo and Rustup live, preferring the environment so
// an installation moved off its default path is still found.
func rustHomes() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	cargoHome := rustHomeDir("CARGO_HOME", home, ".cargo")
	rustupHome := rustHomeDir("RUSTUP_HOME", home, ".rustup")

	return cargoHome, rustupHome
}

// rustHomeDir resolves one Rust home, preferring the environment and falling
// back to a directory under the user's home. The result is cleaned before it is
// used as a path and is reported empty unless it is a directory that exists, so
// a stale or hostile setting becomes "not configured" rather than a path the
// sandbox would go on to mount.
func rustHomeDir(variable, home, fallback string) string {
	dir := strings.TrimSpace(os.Getenv(variable))
	if dir == "" {
		if home == "" {
			return ""
		}

		dir = filepath.Join(home, fallback)
	}

	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return ""
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}

	return dir
}

func (environment sandboxCacheEnvironment) overrides(name string) bool {
	return environment.value(name) != ""
}

func (environment sandboxCacheEnvironment) items() []string {
	items := []string{}

	for _, name := range environment.names() {
		value := environment.value(name)
		if value != "" {
			items = append(items, name+"="+value)
		}
	}

	return items
}

func (environment sandboxCacheEnvironment) value(name string) string {
	return map[string]string{
		"TMPDIR":                 environment.TempDir,
		"XDG_RUNTIME_DIR":        environment.RuntimeDir,
		"GOCACHE":                environment.GoCache,
		"GOPATH":                 environment.GoPath,
		"GOMODCACHE":             environment.GoModCache,
		"GOLANGCI_LINT_CACHE":    environment.GolangCILintDir,
		"GOROOT":                 environment.GoRoot,
		"CGO_ENABLED":            environment.CGOEnabled,
		"CC":                     environment.CC,
		"COMPILER_PATH":          environment.CompilerPath,
		"AS":                     environment.Assembler,
		"CARGO_HOME":             environment.CargoHome,
		"RUSTUP_HOME":            environment.RustupHome,
		"UV_PROJECT_ENVIRONMENT": environment.UVProjectEnvironment,
		evaluators.RealGitEnv:    environment.RealGit,
	}[name]
}

func (environment sandboxCacheEnvironment) names() []string {
	return []string{
		"TMPDIR",
		"XDG_RUNTIME_DIR",
		"GOCACHE",
		"GOPATH",
		"GOMODCACHE",
		"GOLANGCI_LINT_CACHE",
		"GOROOT",
		"CGO_ENABLED",
		"CC",
		"COMPILER_PATH",
		"AS",
		"CARGO_HOME",
		"RUSTUP_HOME",
		"UV_PROJECT_ENVIRONMENT",
		evaluators.RealGitEnv,
	}
}

func makeSandboxDir(dir, what string) error {
	err := os.MkdirAll(dir, capturedPrivateDirMode)
	if err != nil {
		return fmt.Errorf("create sandbox %s dir: %w", what, err)
	}

	return nil
}

func sandboxCacheEnv(
	ctx context.Context,
	request captureRequest,
) (sandboxCacheEnvironment, error) {
	root := firstCaptureNonEmpty(request.TraceRoot, request.Cwd)
	if strings.TrimSpace(root) == "" {
		return sandboxCacheEnvironment{}, nil
	}

	tempDir := filepath.Join(root, sandbox.SandboxTempWritePath)
	runtimeDir := ""
	cleanupTemp := false

	if request.Tool == goTestTool {
		tempDir = resolvedGoTestSandboxTempDir(root)
		runtimeDir = filepath.Join(tempDir, "runtime")
		cleanupTemp = true
	}

	err := makeSandboxDir(tempDir, "temp")
	if err != nil {
		return sandboxCacheEnvironment{}, err
	}

	if runtimeDir != "" {
		err = makeSandboxDir(runtimeDir, "runtime")
		if err != nil {
			return sandboxCacheEnvironment{}, err
		}
	}

	realGit, pathPrefix, err := managedSubprocessGitEnv(ctx, tempDir)
	if err != nil {
		return sandboxCacheEnvironment{}, err
	}

	goCaches, err := prepareSandboxGoCachePaths(root)
	if err != nil {
		return sandboxCacheEnvironment{}, err
	}

	cCompiler := managedCCompiler()
	assembler := managedAssembler()

	cargoHome, rustupHome := rustHomes()

	uvProjectEnvironment, err := sandboxUVProjectEnvironment(root, request)
	if err != nil {
		return sandboxCacheEnvironment{}, err
	}

	return sandboxCacheEnvironment{
		TempDir:              tempDir,
		RuntimeDir:           runtimeDir,
		CleanupTemp:          cleanupTemp,
		GoCache:              goCaches.Cache,
		GoPath:               goCaches.Path,
		GoModCache:           goCaches.ModCache,
		GolangCILintDir:      goCaches.GolangCILint,
		GoRoot:               managedGoRoot(ctx),
		CGOEnabled:           "1",
		CC:                   cCompiler,
		CompilerPath:         managedCompilerPath(cCompiler, assembler),
		Assembler:            assembler,
		RealGit:              realGit,
		PathPrefix:           pathPrefix,
		CargoHome:            cargoHome,
		RustupHome:           rustupHome,
		UVProjectEnvironment: uvProjectEnvironment,
	}, nil
}

func sandboxUVProjectEnvironment(root string, request captureRequest) (string, error) {
	project := strings.TrimSpace(request.UVProject)
	if project == "" {
		return "", nil
	}

	canonical, err := filepath.Abs(project)
	if err != nil {
		return "", fmt.Errorf("resolve uv project path %s: %w", project, err)
	}

	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", fmt.Errorf("resolve uv project symlinks %s: %w", project, err)
	}

	digest := sha256.Sum256([]byte(canonical))
	environment := filepath.Join(
		root,
		".coding-ethos",
		"cache",
		"uv-project-env",
		hex.EncodeToString(digest[:]),
	)

	err = makeSandboxDir(environment, "UV project environment")
	if err != nil {
		return "", err
	}

	return environment, nil
}

func prepareSandboxGoCachePaths(root string) (sandboxGoCachePaths, error) {
	paths := sandboxGoCachePaths{
		Cache:        filepath.Join(root, sandbox.SandboxGoCachePath),
		Path:         filepath.Join(root, sandbox.SandboxGoPath),
		ModCache:     filepath.Join(root, sandbox.SandboxGoModCachePath),
		GolangCILint: filepath.Join(root, sandbox.SandboxGolangCIPath),
	}

	for _, dir := range []struct {
		path string
		what string
	}{
		{paths.Cache, "Go cache"},
		{paths.Path, "Go path"},
		{paths.ModCache, "Go module cache"},
		{paths.GolangCILint, "golangci-lint cache"},
	} {
		err := makeSandboxDir(dir.path, dir.what)
		if err != nil {
			return sandboxGoCachePaths{}, err
		}
	}

	return paths, nil
}

func resolvedManagedSubprocessGit(ctx context.Context) (string, error) {
	envGit := strings.TrimSpace(os.Getenv(evaluators.RealGitEnv))
	if envGit != "" {
		self, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve managed subprocess executable: %w", err)
		}

		resolvedSelf, err := filepath.EvalSymlinks(self)
		if err != nil {
			resolvedSelf = self
		}

		if realgit.UsableCandidate(resolvedSelf, envGit) {
			return envGit, nil
		}
	}

	realGit, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return "", fmt.Errorf("resolve managed subprocess git: %w", err)
	}

	return realGit, nil
}

func cleanupSandboxCacheEnv(environment sandboxCacheEnvironment) {
	if environment.CleanupTemp {
		_ = os.RemoveAll(environment.TempDir)
	}
}
