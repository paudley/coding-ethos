// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	execabs "golang.org/x/sys/execabs"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

var (
	errExternalToolCommandEmpty = apperror.StaticError("external tool command is empty")
	errExternalToolTimedOut     = apperror.StaticError("external tool timed out")
)

const externalToolCacheDirMode = 0o700

type externalToolRequest struct {
	Name           string
	Dir            string
	Command        []string
	Env            []string
	TimeoutSeconds int
}

type externalToolResult struct {
	RunnerFailure error
	Stdout        string
	Stderr        string
	Combined      string
	ExitCode      int
	DurationMS    float64
	TimedOut      bool
}

func runExternalTool(request externalToolRequest) externalToolResult {
	start := time.Now()

	if len(request.Command) == 0 {
		return externalToolResult{
			ExitCode: 1,
			RunnerFailure: fmt.Errorf(
				"%s: %w",
				request.Name,
				errExternalToolCommandEmpty,
			),
		}
	}

	timeout := externalToolTimeout(request)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	env, envErr := externalToolEnv(request.Env)
	if envErr != nil {
		return externalToolResult{
			ExitCode:      1,
			RunnerFailure: fmt.Errorf("%s: %w", request.Name, envErr),
		}
	}

	cmd := execabs.CommandContext(ctx, request.Command[0], request.Command[1:]...)
	cmd.Dir = request.Dir
	cmd.Env = env

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := debuglog.ProcessEnter(request.Command, request.Dir)
	err := cmd.Run()
	debuglog.ProcessExit(
		startedAt,
		request.Command,
		request.Dir,
		commandExitCode(err),
		err,
	)

	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())

	return completedExternalToolResult(
		request,
		start,
		timeout,
		ctx.Err(),
		err,
		stdoutText,
		stderrText,
	)
}

func completedExternalToolResult(
	request externalToolRequest,
	start time.Time,
	timeout int,
	ctxErr error,
	err error,
	stdoutText string,
	stderrText string,
) externalToolResult {
	result := externalToolResult{
		Stdout:     stdoutText,
		Stderr:     stderrText,
		Combined:   externalToolCombinedOutput(stdoutText, stderrText),
		ExitCode:   0,
		DurationMS: float64(time.Since(start).Milliseconds()),
		TimedOut:   errors.Is(ctxErr, context.DeadlineExceeded),
	}
	if result.TimedOut {
		result.ExitCode = 1
		result.RunnerFailure = fmt.Errorf(
			"%s: %w after %d seconds",
			request.Name,
			errExternalToolTimedOut,
			timeout,
		)

		return result
	}

	if err == nil {
		return result
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()

		return result
	}

	result.ExitCode = 1
	result.RunnerFailure = err

	return result
}

func externalToolTimeout(request externalToolRequest) int {
	if request.TimeoutSeconds > 0 {
		return request.TimeoutSeconds
	}

	return loadHookSettings().ToolTimeoutSeconds
}

func externalToolCombinedOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func externalToolEnv(extra []string) ([]string, error) {
	env := make([]string, 0, len(os.Environ())+len(extra))
	hasPath := false

	cacheEnv, err := externalToolCacheEnv(repoRoot())
	if err != nil {
		return nil, err
	}

	for _, item := range os.Environ() {
		if externalToolEnvBlocked(item) {
			continue
		}

		name, value, found := strings.Cut(item, "=")
		if found && name == "PATH" {
			hasPath = true

			env = append(env, name+"="+externalToolPathWithoutGitShim(value))

			continue
		}

		if found && cacheEnv.overrides(name) {
			continue
		}

		env = append(env, item)
	}

	if !hasPath {
		env = append(env, "PATH="+externalToolPathWithoutGitShim(""))
	}

	env = append(env, "GIT_OPTIONAL_LOCKS=0")
	env = append(env, "GIT_CONFIG_NOSYSTEM=1")
	env = append(env, "GIT_CONFIG_GLOBAL="+os.DevNull)
	env = append(env, "XDG_CONFIG_HOME="+os.DevNull)
	env = append(env, cacheEnv.items()...)

	return append(env, extra...), nil
}

type externalToolCacheEnvironment struct {
	GoTemp          string
	GoCache         string
	GolangCILintDir string
	UVCache         string
	UVProjectEnv    string
	// CargoTarget is per-repository build output, like GoCache.
	CargoTarget string
	// CargoHome and RustupHome are the operator's, not the repository's. Cargo
	// resolves dependencies from the registry under CARGO_HOME and finds its
	// toolchain through RUSTUP_HOME, and neither can be rebuilt inside a
	// repository cache without network access. An installation that does not
	// sit at its default path — which is why this was needed at all — is
	// invisible to a tool that inherits no environment.
	CargoHome  string
	RustupHome string
}

// rustHomes reports where Cargo and Rustup live for the operator running this.
//
// Taken from the environment first, because an installation moved off its
// default path is exactly the case that fails silently otherwise: cargo is on
// PATH, runs, and cannot find a toolchain.
func rustHomes() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return rustHomeDir("CARGO_HOME", home, ".cargo"),
		rustHomeDir("RUSTUP_HOME", home, ".rustup")
}

// rustHomeDir resolves one Rust home, preferring the environment and falling
// back to a directory under the user's home. The result is cleaned before it is
// used as a path and is reported empty unless it is a directory that exists, so
// a stale or hostile setting becomes "not configured" rather than a path the
// caller would go on to trust.
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

func externalToolCacheEnv(root string) (externalToolCacheEnvironment, error) {
	if strings.TrimSpace(root) == "" || root == "." {
		return externalToolCacheEnvironment{}, nil
	}

	goTemp := filepath.Join(root, ".coding-ethos", "cache", "go-tmp")
	goCache := filepath.Join(root, sandbox.SandboxGoCachePath)
	golangCILintDir := filepath.Join(root, sandbox.SandboxGolangCIPath)
	uvCache := filepath.Join(root, ".coding-ethos", "cache", "uv")
	uvProjectEnv := filepath.Join(root, ".coding-ethos", "cache", "uv-project-env")
	cargoTarget := filepath.Join(root, ".coding-ethos", "cache", "cargo-target")

	for _, dir := range []string{
		goTemp,
		goCache,
		golangCILintDir,
		uvCache,
		uvProjectEnv,
		cargoTarget,
	} {
		err := os.MkdirAll(dir, externalToolCacheDirMode)
		if err != nil {
			return externalToolCacheEnvironment{}, fmt.Errorf(
				"create external tool cache directory %s: %w",
				dir,
				err,
			)
		}
	}

	cargoHome, rustupHome := rustHomes()

	return externalToolCacheEnvironment{
		GoTemp:          goTemp,
		GoCache:         goCache,
		GolangCILintDir: golangCILintDir,
		UVCache:         uvCache,
		UVProjectEnv:    uvProjectEnv,
		CargoTarget:     cargoTarget,
		CargoHome:       cargoHome,
		RustupHome:      rustupHome,
	}, nil
}

// prepareHookProcessCacheEnvironment projects the consumer-owned cache roots
// onto the hook runner itself. Nested pre-commit languages can start before an
// individual external-tool request is constructed; setting these variables at
// the command boundary prevents uv, Go, Cargo, and linters from falling back
// to an unwritable operator cache. The returned closure restores the exact
// caller environment for in-process tests and embedded invocations.
func prepareHookProcessCacheEnvironment(root string) (func(), error) {
	environment, err := externalToolCacheEnv(root)
	if err != nil {
		return nil, err
	}

	type priorValue struct {
		value   string
		existed bool
	}

	prior := map[string]priorValue{}

	for _, item := range environment.items() {
		name, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}

		previous, existed := os.LookupEnv(name)
		prior[name] = priorValue{value: previous, existed: existed}

		setErr := os.Setenv(name, value)
		if setErr != nil {
			for restoreName, restoreValue := range prior {
				if restoreValue.existed {
					_ = os.Setenv(restoreName, restoreValue.value)
				} else {
					_ = os.Unsetenv(restoreName)
				}
			}

			return nil, fmt.Errorf("set hook process cache environment %s: %w", name, setErr)
		}
	}

	return func() {
		for name, previous := range prior {
			if previous.existed {
				_ = os.Setenv(name, previous.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}, nil
}

func (environment externalToolCacheEnvironment) overrides(name string) bool {
	return environment.value(name) != ""
}

func (environment externalToolCacheEnvironment) items() []string {
	items := []string{}

	for _, name := range []string{
		"TMPDIR",
		"GOTMPDIR",
		"GOCACHE",
		"GOLANGCI_LINT_CACHE",
		"UV_CACHE_DIR",
		"UV_PROJECT_ENVIRONMENT",
		"CARGO_TARGET_DIR",
		"CARGO_HOME",
		"RUSTUP_HOME",
	} {
		value := environment.value(name)
		if value != "" {
			items = append(items, name+"="+value)
		}
	}

	return items
}

func (environment externalToolCacheEnvironment) value(name string) string {
	switch name {
	case "TMPDIR":
		return environment.GoTemp
	case "GOTMPDIR":
		return environment.GoTemp
	case "GOCACHE":
		return environment.GoCache
	case "GOLANGCI_LINT_CACHE":
		return environment.GolangCILintDir
	case "UV_CACHE_DIR":
		return environment.UVCache
	case "UV_PROJECT_ENVIRONMENT":
		return environment.UVProjectEnv
	case "CARGO_TARGET_DIR":
		return environment.CargoTarget
	case "CARGO_HOME":
		return environment.CargoHome
	case "RUSTUP_HOME":
		return environment.RustupHome
	default:
		return ""
	}
}

func externalToolPathWithoutGitShim(pathValue string) string {
	kept := []string{}

	for _, entry := range filepath.SplitList(pathValue) {
		if entry == "" || externalToolPathEntryHasCodingEthosGitShim(entry) {
			continue
		}

		kept = append(kept, entry)
	}

	for _, entry := range externalToolSystemPathCandidates() {
		if slices.Contains(kept, entry) {
			continue
		}

		kept = append(kept, entry)
	}

	return strings.Join(kept, string(os.PathListSeparator))
}

func externalToolSystemPathCandidates() []string {
	return []string{
		"/usr/local/sbin",
		"/usr/local/bin",
		"/usr/sbin",
		"/usr/bin",
		"/sbin",
		"/bin",
	}
}

func externalToolPathEntryHasCodingEthosGitShim(entry string) bool {
	payload, err := os.ReadFile(filepath.Join(entry, "git"))
	if err != nil {
		return false
	}

	text := string(payload)

	return strings.Contains(text, "coding-ethos-run") &&
		strings.Contains(text, "policy-git")
}

func externalToolEnvBlocked(item string) bool {
	name, _, found := strings.Cut(item, "=")
	if !found {
		return false
	}

	if strings.HasPrefix(name, "CODE_ETHOS_") ||
		(strings.HasPrefix(name, "CODING_ETHOS_") &&
			!externalToolAllowedCodingEthosEnv(name)) {
		return true
	}

	if name == consumerRootEnv ||
		name == precommitRootEnv ||
		name == hookGroupChildEnv ||
		name == hookGroupResultPathEnv {
		return true
	}

	return slices.Contains(gitHookLocalEnvNames(), name) ||
		strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
		strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

func externalToolAllowedCodingEthosEnv(name string) bool {
	switch name {
	case "CODING_ETHOS_AGENT_SHELL_SANDBOX",
		"CODING_ETHOS_REAL_GIT",
		"CODING_ETHOS_SANDBOX_ACTIVE",
		"CODING_ETHOS_SANDBOX_ROOT":
		return true
	default:
		return false
	}
}

func gitHookLocalEnvNames() []string {
	return []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE",
	}
}

func commandExitCode(err error) int {
	return processstatus.ExitCode(err, 1)
}
