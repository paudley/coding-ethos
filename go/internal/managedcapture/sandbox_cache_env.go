// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
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
	GolangCILintDir string
	GoRoot          string
	CGOEnabled      string
	CC              string
	CompilerPath    string
	Assembler       string
	RealGit         string
	PathPrefix      string
	CleanupTemp     bool
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
	switch name {
	case "TMPDIR":
		return environment.TempDir
	case "XDG_RUNTIME_DIR":
		return environment.RuntimeDir
	case "GOCACHE":
		return environment.GoCache
	case "GOLANGCI_LINT_CACHE":
		return environment.GolangCILintDir
	case "GOROOT":
		return environment.GoRoot
	case "CGO_ENABLED":
		return environment.CGOEnabled
	case "CC":
		return environment.CC
	case "COMPILER_PATH":
		return environment.CompilerPath
	case "AS":
		return environment.Assembler
	case evaluators.RealGitEnv:
		return environment.RealGit
	default:
		return ""
	}
}

func (environment sandboxCacheEnvironment) names() []string {
	return []string{
		"TMPDIR",
		"XDG_RUNTIME_DIR",
		"GOCACHE",
		"GOLANGCI_LINT_CACHE",
		"GOROOT",
		"CGO_ENABLED",
		"CC",
		"COMPILER_PATH",
		"AS",
		evaluators.RealGitEnv,
	}
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

	err := os.MkdirAll(tempDir, capturedPrivateDirMode)
	if err != nil {
		return sandboxCacheEnvironment{}, fmt.Errorf(
			"create sandbox temp dir: %w",
			err,
		)
	}

	if runtimeDir != "" {
		err = os.MkdirAll(runtimeDir, capturedPrivateDirMode)
		if err != nil {
			return sandboxCacheEnvironment{}, fmt.Errorf(
				"create sandbox runtime dir: %w",
				err,
			)
		}
	}

	realGit, pathPrefix, err := managedSubprocessGitEnv(ctx, tempDir)
	if err != nil {
		return sandboxCacheEnvironment{}, err
	}

	goCache := filepath.Join(root, sandbox.SandboxGoCachePath)

	err = os.MkdirAll(goCache, capturedPrivateDirMode)
	if err != nil {
		return sandboxCacheEnvironment{}, fmt.Errorf(
			"create sandbox Go cache dir: %w",
			err,
		)
	}

	golangCILintDir := filepath.Join(root, sandbox.SandboxGolangCIPath)

	err = os.MkdirAll(golangCILintDir, capturedPrivateDirMode)
	if err != nil {
		return sandboxCacheEnvironment{}, fmt.Errorf(
			"create sandbox golangci-lint cache dir: %w",
			err,
		)
	}

	cCompiler := managedCCompiler()
	assembler := managedAssembler()

	return sandboxCacheEnvironment{
		TempDir:         tempDir,
		RuntimeDir:      runtimeDir,
		CleanupTemp:     cleanupTemp,
		GoCache:         goCache,
		GolangCILintDir: golangCILintDir,
		GoRoot:          managedGoRoot(ctx),
		CGOEnabled:      "1",
		CC:              cCompiler,
		CompilerPath:    managedCompilerPath(cCompiler, assembler),
		Assembler:       assembler,
		RealGit:         realGit,
		PathPrefix:      pathPrefix,
	}, nil
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
