// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agenthookscli"
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/ca"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintelcli"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/githookcli"
	"blackcat.ca/coding-ethos/go/internal/hookcli"
	"blackcat.ca/coding-ethos/go/internal/hooklogcli"
	"blackcat.ca/coding-ethos/go/internal/lintcli"
	"blackcat.ca/coding-ethos/go/internal/mcpcli"
	"blackcat.ca/coding-ethos/go/internal/outputcli"
	"blackcat.ca/coding-ethos/go/internal/policycli"
	"blackcat.ca/coding-ethos/go/internal/policygitcli"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolchaincli"
	"blackcat.ca/coding-ethos/go/internal/webguidancecli"
)

const (
	linuxGOOS                   = "linux"
	agentShellCacheDirMode      = 0o700
	agentShellAssetMode         = 0o700
	agentShellFileMode          = 0o600
	agentShellInjectedEnv       = 3
	agentShellProtectedEntryCap = 2
	agentShellGitPathCap        = 2
)

var (
	errExecutablePathDirectory = apperror.StaticError("executable path is a directory")
	errExecutablePathNoExecBit = apperror.StaticError(
		"executable path has no executable bit",
	)
)

type runtimeExecutor interface {
	// runLint runs policy-lint and returns a status for callers that continue.
	runLint(args ...string) int
	// execLint runs policy-lint and exits with its status.
	execLint(args ...string)
	// execAgentHook runs provider hook processing against stdin/stdout/stderr.
	execAgentHook(args ...string)
	// execAgentShell runs the approved agent shell command boundary.
	execAgentShell(paths runtimePaths, command string)
	// runInternalTool runs a bundled command and returns control to the caller.
	runInternalTool(tool string, args ...string)
	// execInternalTool runs a bundled command and exits with its status.
	execInternalTool(tool string, args ...string)
	// runTool runs a managed runtime tool and returns control to the caller.
	runTool(paths runtimePaths, tool string, args ...string)
	// execTool runs a managed runtime tool and exits with its status.
	execTool(paths runtimePaths, tool string, args ...string)
	// execPath executes a resolved path and exits with its status.
	execPath(path string, args ...string)
	// execExternal executes an external binary without managed path resolution.
	execExternal(path string, args ...string)
}

type defaultRuntimeExecutor struct{}

func (defaultRuntimeExecutor) runLint(args ...string) int {
	return lintcli.Run(args)
}

func (defaultRuntimeExecutor) execLint(args ...string) {
	requestRuntimeExit(lintcli.Run(args))
}

func (defaultRuntimeExecutor) execAgentHook(args ...string) {
	requestRuntimeExit(hookcli.Run(args, os.Stdin, os.Stdout, os.Stderr))
}

func (defaultRuntimeExecutor) execAgentShell(paths runtimePaths, command string) {
	shellPath := "/usr/bin/env"
	args := []string{"bash", "-lc", command}
	executable := shellPath
	execArgs := args
	cwd := paths.InvocationCWD
	processEnv := os.Environ()
	processEvidence := sandbox.Evidence{}

	if runtime.GOOS == linuxGOOS {
		plan, agentEnv, clean, err := agentShellSandboxPlan(paths, shellPath, args)
		if err != nil {
			exitErr(err)
		}

		defer clean()
		defer func() {
			closeErr := plan.Close()
			if closeErr != nil {
				exitErr(closeErr)
			}
		}()

		executable = plan.Executable
		execArgs = plan.Args
		processEnv = agentEnv
		processEvidence = plan.Evidence
	} else {
		debuglog.Debug(
			"agent-shell.sandbox.unavailable",
			zap.String("goos", runtime.GOOS),
			zap.String("sandbox_profile", agentShellSandboxProfile(runtime.GOOS)),
			zap.Bool("sandbox_enforced", agentShellSandboxEnforced(runtime.GOOS)),
		)
	}

	process := safeexec.CommandContext(context.Background(), executable, execArgs...)
	process.Dir = paths.InvocationCWD
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	process.Env = processEnv

	if runtime.GOOS == linuxGOOS {
		process.SysProcAttr = sandbox.SysProcAttr(nil, processEvidence)
	}

	argv := append([]string{executable}, execArgs...)
	startedAt := debuglog.ProcessEnter(
		argv,
		cwd,
		zap.String("runtime_tool", "agent-shell"),
		zap.String("consumer_root", paths.Root),
		zap.String("strategic_intent", agentShellStrategicIntent()),
	)
	err := process.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		cwd,
		runtimeCommandExitCode(err),
		err,
		zap.String("runtime_tool", "agent-shell"),
		zap.String("consumer_root", paths.Root),
		zap.String("strategic_intent", agentShellStrategicIntent()),
	)

	if err != nil {
		exitErr(err)
	}

	requestRuntimeExit(0)
}

func agentShellSandboxPlan(
	paths runtimePaths,
	executable string,
	args []string,
) (sandbox.Plan, []string, func(), error) {
	realGitPath, err := agentShellRealGitPath(paths)
	if err != nil {
		return sandbox.Plan{}, nil, func() {}, err
	}

	gitWrapper, realGitBind, cleanup, err := agentShellGitWrapper(paths, realGitPath)
	if err != nil {
		return sandbox.Plan{}, nil, cleanup, err
	}

	agentWritePaths, err := agentShellWorktreeWritePaths(paths.Root)
	if err != nil {
		cleanup()

		return sandbox.Plan{}, nil, func() {}, err
	}

	agentWriteDirs, err := agentShellEnsureWriteDirs(paths.Root)
	if err != nil {
		cleanup()

		return sandbox.Plan{}, nil, func() {}, err
	}

	agentWritePaths = append(agentWritePaths, agentWriteDirs...)
	interceptEvidence := agentShellInterceptEvidence(paths)
	interceptCACertPath := agentShellInterceptCACertPath(interceptEvidence)
	envBindings := agentShellEnvBindings(interceptCACertPath)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Cwd:         paths.InvocationCWD,
		RepoRoot:    paths.Root,
		Executable:  executable,
		WrapperPath: filepath.Join(paths.BinDir, "coding-ethos-sandbox"),
		Args:        args,
		Tool:        "agent-shell",
		Capabilities: sandbox.Capabilities{
			SandboxProfile:    "agent-shell",
			StrategicIntent:   agentShellStrategicIntent(),
			WritePaths:        append(agentShellProtectedWritePaths(paths), agentWritePaths...),
			ReadOnlyPaths:     agentShellReadOnlyPaths(paths.Root),
			ReadPaths:         agentShellInterceptReadPaths(interceptEvidence),
			EnvBindings:       envBindings,
			AllowGitWrites:    true,
			RequiresGit:       true,
			RequiresNetwork:   true,
			RequiresProcesses: true,
			Tags:              []string{"agent-shell", "path-git-wrapper"},
		},
	})
	if err != nil {
		cleanup()

		return sandbox.Plan{}, nil, func() {}, fmt.Errorf(
			"build agent-shell sandbox: %w",
			err,
		)
	}

	processEnv := agentShellProcessEnv(
		paths.Root,
		gitWrapper,
		realGitBind,
		interceptCACertPath,
	)

	return plan, processEnv, cleanup, nil
}

// agentShellEnsureWriteDirs creates the agent-shell managed write directories
// under the repo root and returns their paths so the caller can fold them into
// the sandbox write set.
func agentShellEnsureWriteDirs(root string) ([]string, error) {
	dirs := []string{
		filepath.Join(root, sandbox.SandboxTempWritePath),
		filepath.Join(root, ".coding-ethos", "cache"),
		filepath.Join(root, ".coding-ethos", "state"),
		filepath.Join(root, ".coding-ethos", "lint-runs"),
	}

	for _, dir := range dirs {
		err := os.MkdirAll(dir, agentShellCacheDirMode)
		if err != nil {
			return nil, fmt.Errorf(
				"create agent-shell write directory %s: %w",
				dir,
				err,
			)
		}
	}

	return dirs, nil
}

// agentShellInterceptEvidence resolves the HTTPS interception gate decision for
// this repo. A failed gate evaluation is treated as disabled (zero-value
// evidence) so a misconfigured gate yields no CA trust binding rather than
// blocking the shell.
func agentShellInterceptEvidence(paths runtimePaths) agentproxy.InterceptionEvidence {
	mode, approval := resolveProxyInterceptionConfig(paths)

	evidence, err := ca.Evaluate(ca.GateInput{
		Now:        time.Now().UTC(),
		Mode:       mode,
		CAApproval: approval,
		RepoRoot:   paths.Root,
		EnvOptIn:   agentAPIProxyInterceptOptIn(),
	})
	if err != nil {
		return agentproxy.InterceptionEvidence{}
	}

	return evidence
}

// agentShellInterceptReadPaths returns the local CA certificate as a read-only
// sandbox bind when interception is enabled, so the child can trust the proxy
// leaves. The host trust store is never touched.
func agentShellInterceptReadPaths(
	evidence agentproxy.InterceptionEvidence,
) []string {
	path := agentShellInterceptCACertPath(evidence)
	if path == "" {
		return nil
	}

	return []string{path}
}

// agentShellInterceptCACertPath returns the provisioned CA certificate path
// only when interception is enabled, and an empty string otherwise.
func agentShellInterceptCACertPath(
	evidence agentproxy.InterceptionEvidence,
) string {
	if evidence.Enabled && evidence.CACertPath != "" {
		return evidence.CACertPath
	}

	return ""
}

func agentShellProtectedWritePaths(paths runtimePaths) []string {
	writePaths := agentShellGitWritePaths(paths)
	writePaths = append(writePaths, agentShellSigningWritePaths()...)

	return writePaths
}

func agentShellGitWritePaths(paths runtimePaths) []string {
	seen := map[string]bool{}
	writePaths := make([]string, 0, agentShellGitPathCap)

	for _, path := range []string{paths.GitDir, paths.GitCommonDir} {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}

		seen[path] = true
		writePaths = append(writePaths, path)
	}

	return writePaths
}

func agentShellSigningWritePaths() []string {
	writePaths := make([]string, 0, agentShellGitPathCap)

	if gpgHome := strings.TrimSpace(os.Getenv("GNUPGHOME")); gpgHome != "" {
		writePaths = appendExistingGPGHomeWritePaths(writePaths, gpgHome)
	} else {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			writePaths = appendExistingGPGHomeWritePaths(
				writePaths,
				filepath.Join(home, ".gnupg"),
			)
		}
	}

	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		writePaths = append(writePaths, filepath.Join(runtimeDir, "gnupg"))
	}

	writePaths = append(writePaths, agentShellTerminalWritePaths()...)

	return writePaths
}

func appendExistingGPGHomeWritePaths(writePaths []string, gpgHome string) []string {
	cleanGPGHome := filepath.Clean(strings.TrimSpace(gpgHome))
	if cleanGPGHome == "." || !filepath.IsAbs(cleanGPGHome) {
		return writePaths
	}

	info, err := os.Stat(cleanGPGHome)
	if err != nil || !info.IsDir() {
		return writePaths
	}

	// Bind the fully resolved gpg home rather than a configured symlink. The
	// native sandbox rejects symlinked write paths to prevent symlink escapes,
	// so a symlinked gpg home (a common dotfiles layout) would otherwise block
	// signed commits. Resolving here keeps the writable bind target a real
	// directory while preserving access to the gpg keyring.
	resolvedGPGHome, err := filepath.EvalSymlinks(cleanGPGHome)
	if err != nil {
		resolvedGPGHome = cleanGPGHome
	}

	writePaths = append(writePaths, resolvedGPGHome)
	writePaths = append(
		writePaths,
		agentShellResolvedGPGHomeWritePaths(resolvedGPGHome)...,
	)

	return writePaths
}

func agentShellResolvedGPGHomeWritePaths(gpgHome string) []string {
	gpgHome = filepath.Clean(strings.TrimSpace(gpgHome))
	if gpgHome == "" {
		return nil
	}

	paths := []string{}
	seen := map[string]bool{}
	appendPath := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || seen[path] {
			return
		}

		seen[path] = true
		paths = append(paths, path)
	}

	resolvedHome, err := filepath.EvalSymlinks(gpgHome)
	if err == nil {
		appendPath(resolvedHome)
	}

	agentShellAppendResolvedGPGSymlinkPaths(gpgHome, appendPath)
	agentShellAppendResolvedGPGSymlinkPaths(
		filepath.Join(gpgHome, "private-keys-v1.d"),
		appendPath,
	)

	return paths
}

func agentShellAppendResolvedGPGSymlinkPaths(
	dir string,
	appendPath func(string),
) {
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

		agentShellAppendResolvedGPGPath(resolved, appendPath)
	}
}

func agentShellAppendResolvedGPGPath(
	resolved string,
	appendPath func(string),
) {
	resolvedInfo, err := os.Stat(resolved)
	if err == nil && resolvedInfo.IsDir() {
		appendPath(resolved)

		return
	}

	appendPath(filepath.Dir(resolved))
}

func agentShellTerminalWritePaths() []string {
	writePaths := []string{}
	seen := map[string]bool{}

	for _, fd := range []string{"0", "1", "2"} {
		path, err := os.Readlink(filepath.Join("/proc/self/fd", fd))
		if err != nil || !agentShellTerminalPath(path) || seen[path] {
			continue
		}

		seen[path] = true
		writePaths = append(writePaths, path)
	}

	return writePaths
}

func agentShellTerminalPath(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))

	return strings.HasPrefix(path, "/dev/pts/") || path == "/dev/tty"
}

// agentShellWorktreeWritePaths reports the worktree paths the agent may write.
//
// The root itself leads, and it has to. Creating, deleting and renaming a file
// are rights over the directory holding it, not over the file, so a set built
// only from the entries beneath the root leaves the root itself ungranted --
// and then no top-level file can be replaced, only edited in place. That is
// what git does on every merge, and it failed as "unable to unlink old
// 'Makefile': Permission denied" on files that were plainly writable, which
// read as though those particular files were protected. Nothing about them
// was: they were simply the top-level ones main had touched. Two lanes stopped
// there and neither could be finished.
//
// The entries are still listed, because a rule on the root does not describe
// what is beneath it once a mount intervenes, and .git and .coding-ethos are
// held read-only by exactly such a mount -- see agentShellReadOnlyPaths.
func agentShellWorktreeWritePaths(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read agent-shell worktree root %s: %w", root, err)
	}

	paths := make([]string, 0, len(entries)+1)
	paths = append(paths, root)

	for _, entry := range entries {
		name := entry.Name()
		if protectedAgentShellWorktreeEntry(name) {
			continue
		}

		symlink, err := agentShellWorktreeEntryIsSymlink(entry)
		if err != nil {
			return nil, fmt.Errorf(
				"classify agent-shell worktree entry %s: %w",
				filepath.Join(root, name), err,
			)
		}

		if symlink {
			continue
		}

		paths = append(paths, filepath.Join(root, name))
	}

	return paths, nil
}

// agentShellWorktreeEntryIsSymlink reports whether a worktree directory entry
// is a symbolic link, excluding such entries from the sandbox write set.
//
// It trusts the readdir type bits when they conclusively identify the entry,
// and otherwise falls back to an lstat. Go reports an unknown d_type as
// ^FileMode(0) (all bits set), so a bare entry.Type()&os.ModeSymlink check
// would silently exclude every entry on filesystems that omit d_type. The
// lstat fallback classifies those entries precisely instead, and a stat
// failure is surfaced as an error rather than degrading silently.
func agentShellWorktreeEntryIsSymlink(entry os.DirEntry) (bool, error) {
	mode := entry.Type()

	// The readdir type bits are conclusive only for these single known types;
	// an unknown d_type is reported as ^FileMode(0) and falls through to lstat.
	if mode == 0 || mode == os.ModeDir || mode == os.ModeSymlink {
		return mode == os.ModeSymlink, nil
	}

	info, err := entry.Info()
	if err != nil {
		return false, fmt.Errorf("stat worktree entry %s: %w", entry.Name(), err)
	}

	return info.Mode()&os.ModeSymlink != 0, nil
}

func protectedAgentShellWorktreeEntry(name string) bool {
	return name == ".git" ||
		name == ".coding-ethos"
}

// agentShellReadOnlyPaths reports what must stay read-only inside the writable
// worktree root.
//
// Granting the root is what lets git replace top-level files, and Landlock
// grants reach everything beneath -- it is allow-only, with no way to exclude
// a subtree. So the same entries that used to be protected by being left out
// of the write set are now pinned by a read-only mount, which does hold
// against a writable parent. An agent that could write .git/config or install
// a hook would be outside the git wrapper altogether, so this is not optional:
// the sandbox refuses to start if a pin cannot be established.
func agentShellReadOnlyPaths(root string) []string {
	paths := make([]string, 0, agentShellProtectedEntryCap)
	for _, name := range [...]string{".git", ".coding-ethos"} {
		paths = append(paths, filepath.Join(root, name))
	}

	return paths
}

func agentShellProcessEnv(
	root, gitWrapper, realGitBind, interceptCACertPath string,
) []string {
	env := os.Environ()
	proxyEnv := agentAPIProxyRoutingEnv()
	wrapperDir := filepath.Dir(gitWrapper)
	pathValue := wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH")
	tempDir := filepath.Join(root, sandbox.SandboxTempWritePath)
	gpgTTY := agentShellGPGTTY()
	// Inherited CA trust variables are only dropped when interception binds a
	// replacement CA; otherwise they are preserved so a configured custom trust
	// bundle continues to apply inside the agent shell.
	replaceCAEnv := strings.TrimSpace(interceptCACertPath) != ""
	replaceProxyEnv := len(proxyEnv) > 0
	filterOptions := agentShellEnvFilterOptions{
		ReplaceCAEnv:    replaceCAEnv,
		ReplaceProxyEnv: replaceProxyEnv,
	}

	filtered := make([]string, 0, len(env)+agentShellInjectedEnv)
	for _, item := range env {
		if agentShellFilteredEnv(item, filterOptions) {
			continue
		}

		filtered = append(filtered, item)
	}

	filtered = append(
		filtered,
		"PATH="+pathValue,
		realgit.Env+"="+realGitBind,
		"CODING_ETHOS_AGENT_SHELL_SANDBOX=1",
		"CODING_ETHOS_SANDBOX_ACTIVE=1",
		"CODING_ETHOS_SANDBOX_ROOT="+root,
		"TMPDIR="+tempDir,
	)
	if gpgTTY != "" {
		filtered = append(filtered, "GPG_TTY="+gpgTTY)
	}

	if replaceCAEnv {
		filtered = append(
			filtered,
			agentShellInterceptCAEnv(interceptCACertPath)...,
		)
	}

	if replaceProxyEnv {
		filtered = append(filtered, agentShellProxyEnv(proxyEnv)...)
	}

	return filtered
}

type agentShellEnvFilterOptions struct {
	ReplaceCAEnv    bool
	ReplaceProxyEnv bool
}

func agentShellFilteredEnv(item string, options agentShellEnvFilterOptions) bool {
	return agentShellFilteredRuntimeEnv(item) ||
		(options.ReplaceCAEnv && agentShellFilteredCAEnv(item)) ||
		(options.ReplaceProxyEnv && agentShellFilteredProxyEnv(item)) ||
		agentShellFilteredGUIEnv(item)
}

func agentShellFilteredRuntimeEnv(item string) bool {
	return strings.HasPrefix(item, "PATH=") ||
		strings.HasPrefix(item, realgit.Env+"=") ||
		strings.HasPrefix(item, "CODING_ETHOS_AGENT_SHELL_SANDBOX=") ||
		strings.HasPrefix(item, "GPG_TTY=") ||
		strings.HasPrefix(item, "TMPDIR=")
}

// agentShellFilteredCAEnv reports whether an inherited env entry names one of
// the CA trust variables the sandbox replaces, so a host value never leaks past
// the injected interception trust binding.
func agentShellFilteredCAEnv(item string) bool {
	return strings.HasPrefix(item, "SSL_CERT_FILE=") ||
		strings.HasPrefix(item, "REQUESTS_CA_BUNDLE=") ||
		strings.HasPrefix(item, "NODE_EXTRA_CA_CERTS=")
}

func agentShellFilteredProxyEnv(item string) bool {
	for _, name := range agentShellProxyEnvNames() {
		if strings.HasPrefix(item, name+"=") {
			return true
		}
	}

	return false
}

func agentShellProxyEnv(proxyEnv map[string]string) []string {
	items := []string{}

	for _, name := range agentShellProxyEnvNames() {
		value := strings.TrimSpace(proxyEnv[name])
		if value != "" {
			items = append(items, name+"="+value)
		}
	}

	return items
}

func agentShellEnvBindings(interceptCACertPath string) []string {
	bindings := []string{}
	proxyEnv := agentAPIProxyRoutingEnv()

	for _, name := range agentShellProxyEnvNames() {
		if strings.TrimSpace(proxyEnv[name]) != "" {
			bindings = append(bindings, name)
		}
	}

	if strings.TrimSpace(interceptCACertPath) != "" {
		bindings = append(bindings, agentShellCAEnvNames()...)
	}

	return bindings
}

func agentShellProxyEnvNames() []string {
	return []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"}
}

func agentShellCAEnvNames() []string {
	return []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"}
}

// agentShellInterceptCAEnv returns the CA trust variables that point the
// sandboxed child at the local interception CA, or nil when interception is
// disabled and no cert path was bound.
func agentShellInterceptCAEnv(interceptCACertPath string) []string {
	path := strings.TrimSpace(interceptCACertPath)
	if path == "" {
		return nil
	}

	items := make([]string, 0, len(agentShellCAEnvNames()))
	for _, name := range agentShellCAEnvNames() {
		items = append(items, name+"="+path)
	}

	return items
}

func agentShellGPGTTY() string {
	for _, fd := range []string{"0", "1", "2"} {
		path, err := os.Readlink(filepath.Join("/proc/self/fd", fd))
		if err != nil || !agentShellTerminalPath(path) {
			continue
		}

		return filepath.Clean(path)
	}

	return ""
}

func agentShellFilteredGUIEnv(item string) bool {
	return strings.HasPrefix(item, "DISPLAY=") ||
		strings.HasPrefix(item, "WAYLAND_DISPLAY=") ||
		strings.HasPrefix(item, "XAUTHORITY=")
}

func agentShellRealGitPath(paths runtimePaths) (string, error) {
	if realgit.UsableCandidate(paths.RunBinary, paths.RealGit) {
		return paths.RealGit, nil
	}

	for _, candidate := range []string{
		"/usr/bin/git",
		"/bin/git",
		"/usr/local/bin/git",
		"/opt/homebrew/bin/git",
	} {
		if realgit.UsableCandidate(paths.RunBinary, candidate) {
			return candidate, nil
		}
	}

	return "", apperror.StaticError(
		"agent-shell could not resolve a non-wrapper host git executable",
	)
}

func agentShellGitWrapper(
	paths runtimePaths,
	realGitPath string,
) (string, string, func(), error) {
	tempRoot := filepath.Join(paths.Root, ".coding-ethos", "cache", "agent-shell")

	err := os.MkdirAll(tempRoot, agentShellCacheDirMode)
	if err != nil {
		return "", "", func() {}, fmt.Errorf(
			"create agent-shell sandbox cache %s: %w",
			tempRoot,
			err,
		)
	}

	tempDir, err := os.MkdirTemp(tempRoot, "run-")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("create agent-shell sandbox assets: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(tempDir) }
	realGitBind := filepath.Join(tempDir, "real-git")
	wrapper := filepath.Join(tempDir, "git")

	err = copyExecutableFile(realGitPath, realGitBind, agentShellAssetMode)
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf("create real git bind target: %w", err)
	}

	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"export " + realgit.Env + "=" + shellSingleQuote(realGitBind) + "\n" +
		"export CODING_ETHOS_AGENT_SHELL_SANDBOX=1\n" +
		"exec " + shellSingleQuote(paths.RunBinary) + " policy-git \"$@\"\n"

	err = writeExecutableFile(wrapper, []byte(script))
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf("write agent-shell git wrapper: %w", err)
	}

	err = validateExecutablePath(wrapper)
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf(
			"agent-shell git wrapper is not executable: %w",
			err,
		)
	}

	return wrapper, realGitBind, cleanup, nil
}

func copyExecutableFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source executable %s: %w", source, err)
	}

	defer func() {
		_ = input.Close()
	}()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open destination executable %s: %w", destination, err)
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()

	if copyErr != nil {
		return fmt.Errorf("copy executable %s to %s: %w", source, destination, copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close destination executable %s: %w", destination, closeErr)
	}

	return nil
}

func agentShellSandboxProfile(goos string) string {
	if goos == linuxGOOS {
		return "agent-shell"
	}

	return "none"
}

func agentShellSandboxEnforced(goos string) bool {
	return goos == linuxGOOS
}

func validateExecutablePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable path %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%w: %s", errExecutablePathDirectory, path)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: %s", errExecutablePathNoExecBit, path)
	}

	return nil
}

func writeExecutableFile(path string, content []byte) error {
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		agentShellFileMode,
	)
	if err != nil {
		return fmt.Errorf("create executable file %s: %w", path, err)
	}

	_, writeErr := file.Write(content)
	closeErr := file.Close()

	if writeErr != nil {
		return fmt.Errorf("write executable file %s: %w", path, writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close executable file %s: %w", path, closeErr)
	}

	err = os.Chmod(path, agentShellAssetMode)
	if err != nil {
		return fmt.Errorf("mark executable file %s executable: %w", path, err)
	}

	return nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func requirePolicyBundle(paths runtimePaths) {
	requireRuntimeFile(paths.PolicyBundle, "compiled policy bundle")
}

func requireRuntimeFile(path, description string) {
	info, err := statPathWithRoot(path)
	if err != nil || info.IsDir() {
		runtimeFailure(fmt.Sprintf("missing %s: %s", description, path))
	}
}

func requireRuntimeBinary(path, description string) {
	info, err := statPathWithRoot(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		runtimeFailure(
			fmt.Sprintf("missing or non-executable %s: %s", description, path),
		)
	}
}

func statPathWithRoot(path string) (os.FileInfo, error) {
	rootPath := filepath.Dir(path)

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", rootPath, err)
	}
	defer root.Close()

	info, err := root.Stat(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	return info, nil
}

func runtimeFailure(problem string) {
	feedback.Emit(
		os.Stderr,
		feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("status", "fatal"),
				feedback.S("summary", "coding-ethos hook runtime is missing or invalid"),
				feedback.S("problem", problem),
				feedback.S(
					"action",
					"run make build, or ask an admin to repair the coding-ethos checkout",
				),
			},
		},
		feedback.FormatTOON,
	)
	requestRuntimeExit(exitMissing)
}

func gitOutput(realGit, dir string, args ...string) (string, error) {
	output, err := gitOutputRaw(realGit, dir, args...)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(output), nil
}

func gitOutputRaw(realGit, dir string, args ...string) (string, error) {
	command := realgit.CommandFor(context.Background(), realGit, false, args...)
	if dir != "" {
		command.Dir = dir
	}

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}

func runtimeRunTool(paths runtimePaths, tool string, args ...string) {
	if isInternalRuntimeTool(tool) {
		paths.executor().runInternalTool(tool, args...)

		return
	}

	paths.executor().runTool(paths, tool, args...)
}

func runtimeExecTool(paths runtimePaths, tool string, args ...string) {
	if isInternalRuntimeTool(tool) {
		paths.executor().execInternalTool(tool, args...)

		return
	}

	paths.executor().execTool(paths, tool, args...)
}

func runtimeRunLint(paths runtimePaths, args ...string) int {
	return paths.executor().runLint(args...)
}

func runtimeExecLint(paths runtimePaths, args ...string) {
	paths.executor().execLint(args...)
}

func runtimeExecExternal(paths runtimePaths, path string, args ...string) {
	paths.executor().execExternal(path, args...)
}

func (defaultRuntimeExecutor) runTool(paths runtimePaths, tool string, args ...string) {
	requireExternalRuntimeTool(tool)

	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	command := safeexec.CommandContext(context.Background(), toolPath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	argv := append([]string{toolPath}, args...)
	startedAt := debuglog.ProcessEnter(
		argv,
		paths.InvocationCWD,
		zap.String("runtime_tool", tool),
	)
	err := command.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		paths.InvocationCWD,
		runtimeCommandExitCode(err),
		err,
		zap.String("runtime_tool", tool),
	)

	if err != nil {
		exitErr(err)
	}
}

func (executor defaultRuntimeExecutor) execTool(
	paths runtimePaths,
	tool string,
	args ...string,
) {
	requireExternalRuntimeTool(tool)

	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	executor.execPath(toolPath, args...)
}

func requireExternalRuntimeTool(tool string) {
	if strings.HasPrefix(filepath.Base(tool), "coding-ethos-") {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}
}

func isInternalRuntimeTool(tool string) bool {
	_, ok := internalToolHandlers()[tool]

	return ok
}

func (defaultRuntimeExecutor) runInternalTool(tool string, args ...string) {
	handler, ok := internalToolHandlers()[tool]
	if !ok {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}

	code := handler(args)
	if code != 0 {
		requestRuntimeExit(code)
	}
}

func (defaultRuntimeExecutor) execInternalTool(tool string, args ...string) {
	handler, ok := internalToolHandlers()[tool]
	if !ok {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}

	requestRuntimeExit(handler(args))
}

type internalToolHandler func([]string) int

func internalToolHandlers() map[string]internalToolHandler {
	return map[string]internalToolHandler{
		"coding-ethos-agent-hooks":  agenthookscli.Run,
		"coding-ethos-code-intel":   runCodeIntelCLI,
		"coding-ethos-git":          runPolicyGitCLI,
		"coding-ethos-git-hook":     githookcli.Run,
		"coding-ethos-hook":         runHookCLI,
		"coding-ethos-hook-log":     runHookLogCLI,
		"coding-ethos-mcp":          runMCPCLI,
		"coding-ethos-output":       runOutputCLI,
		"coding-ethos-policy":       policycli.Run,
		"coding-ethos-toolchain":    toolchaincli.Run,
		"coding-ethos-web-guidance": runWebGuidanceCLI,
	}
}

func runCodeIntelCLI(args []string) int {
	err := codeintelcli.Run(context.Background(), args)
	if err != nil {
		emitRuntimeError(err)

		return 1
	}

	return 0
}

func runOutputCLI(args []string) int {
	err := outputcli.Run(context.Background(), args)
	if err != nil {
		emitRuntimeError(err)

		return 1
	}

	return 0
}

func runWebGuidanceCLI(args []string) int {
	err := webguidancecli.Run(context.Background(), args)
	if err != nil {
		emitRuntimeError(err)

		return 1
	}

	return 0
}

func runPolicyGitCLI(args []string) int {
	err := policygitcli.Run(args)
	if err != nil {
		exitErr(err)
	}

	return 0
}

func runHookCLI(args []string) int {
	return hookcli.Run(args, os.Stdin, os.Stdout, os.Stderr)
}

func runHookLogCLI(args []string) int {
	err := hooklogcli.Run(args, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		exitErr(err)
	}

	return 0
}

func runMCPCLI(args []string) int {
	err := mcpcli.Run(args, os.Stdin, os.Stdout)
	if err != nil {
		emitRuntimeError(err)

		return 1
	}

	return 0
}

func emitRuntimeError(err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
}

func (executor defaultRuntimeExecutor) execPath(path string, args ...string) {
	executor.execExternal(path, args...)
}

func (defaultRuntimeExecutor) execExternal(path string, args ...string) {
	command := safeexec.CommandContext(context.Background(), path, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	argv := append([]string{path}, args...)
	startedAt := debuglog.ProcessEnter(argv, "", zap.Bool("runtime_external", true))
	err := command.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		"",
		runtimeCommandExitCode(err),
		err,
		zap.Bool("runtime_external", true),
	)

	if err != nil {
		exitErr(err)
	}

	requestRuntimeExit(0)
}

func runtimeCommandExitCode(err error) int {
	return processstatus.ExitCode(err, 1)
}
