// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package sandbox builds and executes OS sandbox requests for managed tools.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	ModeOff      = "off"
	ModeAuto     = "auto"
	ModeRequired = "required"

	BackendBubblewrap = "bubblewrap"

	cgroupLineParts     = 3
	firstExtraFileFD    = 3
	privateDirMode      = 0o700
	sandboxRootDir      = "/"
	toolFallbackName    = "tool"
	virtualDeviceDir    = "/dev"
	virtualHomeDir      = "/home"
	virtualProcDir      = "/proc"
	virtualRootHomeDir  = "/root"
	virtualTemporaryDir = "/tmp"
)

var (
	ErrBackendUnavailable = apperror.StaticError("sandbox backend unavailable")
	errBubblewrapNotFound = apperror.StaticError("bubblewrap executable not found")
	errBubblewrapPlatform = apperror.StaticError(
		"bubblewrap sandbox requires Linux namespace support",
	)
	errEmptySandboxPath  = apperror.StaticError("empty path")
	errSandboxExecutable = apperror.StaticError("sandbox executable is required")
)

func supportsBubblewrap() bool {
	return runtime.GOOS == "linux"
}

func cgroupRootPath() string {
	for _, path := range cgroupRootCandidates() {
		if writableCgroupRoot(path) {
			return path
		}
	}

	return ""
}

func cgroupRootCandidates() []string {
	candidates := []string{}

	data, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			parts := strings.SplitN(line, ":", cgroupLineParts)
			if len(parts) == cgroupLineParts && parts[0] == "0" {
				candidates = append(
					candidates,
					filepath.Join("/sys/fs/cgroup", parts[2]),
				)
			}
		}
	}

	uid := os.Getuid()
	if uid >= 0 {
		candidates = append(
			candidates,
			filepath.Join(
				"/sys/fs/cgroup/user.slice",
				fmt.Sprintf("user-%d.slice", uid),
				fmt.Sprintf("user@%d.service", uid),
			),
		)
	}

	candidates = append(candidates, "/sys/fs/cgroup")

	return candidates
}

func writableCgroupRoot(path string) bool {
	_, err := os.Stat(filepath.Join(path, "cgroup.controllers"))
	if err != nil {
		return false
	}

	probe, err := os.MkdirTemp(path, "coding-ethos-probe-")
	if err != nil {
		return false
	}

	_ = os.Remove(probe)

	return true
}

type Capabilities struct {
	SandboxProfile     string   `json:"sandbox_profile,omitempty"`
	SeccompProfilePath string   `json:"seccomp_profile_path,omitempty"`
	SeccompProfile     string   `json:"seccomp_profile,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ReadPaths          []string `json:"read_paths,omitempty"`
	WritePaths         []string `json:"write_paths,omitempty"`
	CPUQuotaPercent    int      `json:"cpu_quota_percent,omitempty"`
	MemoryMB           int      `json:"memory_mb,omitempty"`
	TimeoutSeconds     int      `json:"timeout_seconds,omitempty"`
	RequiresNetwork    bool     `json:"requires_network,omitempty"`
	RequiresGit        bool     `json:"requires_git,omitempty"`
	RequiresEnv        bool     `json:"requires_env,omitempty"`
	RequiresProcesses  bool     `json:"requires_processes,omitempty"`
}

type Request struct {
	BackendPath  string
	Cwd          string
	Executable   string
	Mode         string
	RepoRoot     string
	Tool         string
	Args         []string
	Capabilities Capabilities
}

type Plan struct {
	Executable string
	Args       []string
	ExtraFiles []*os.File
	Evidence   Evidence
}

type Evidence struct {
	CgroupPath           string   `json:"cgroup_path,omitempty"`
	Backend              string   `json:"backend,omitempty"`
	BackendPath          string   `json:"backend_path,omitempty"`
	Profile              string   `json:"profile,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	SeccompProfile       string   `json:"seccomp_profile,omitempty"`
	Mode                 string   `json:"mode,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	ReadPaths            []string `json:"read_paths,omitempty"`
	WritePaths           []string `json:"write_paths,omitempty"`
	HiddenCredentialDirs []string `json:"hidden_credential_dirs,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Command              []string `json:"command,omitempty"`
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`
	MemoryMB             int      `json:"memory_mb,omitempty"`
	CPUQuotaPercent      int      `json:"cpu_quota_percent,omitempty"`
	RequiresGit          bool     `json:"requires_git,omitempty"`
	GitReadOnly          bool     `json:"git_read_only,omitempty"`
	ReadOnlyRoot         bool     `json:"read_only_root,omitempty"`
	NetworkIsolated      bool     `json:"network_isolated,omitempty"`
	ProcessIsolated      bool     `json:"process_isolated,omitempty"`
	TimeoutEnforced      bool     `json:"timeout_enforced,omitempty"`
	CgroupRequested      bool     `json:"cgroup_requested,omitempty"`
	CgroupEnabled        bool     `json:"cgroup_enabled,omitempty"`
	RequiresProcesses    bool     `json:"requires_processes,omitempty"`
	RequiresEnv          bool     `json:"requires_env,omitempty"`
	SeccompEnabled       bool     `json:"seccomp_enabled,omitempty"`
	Enabled              bool     `json:"enabled"`
	Denied               bool     `json:"denied,omitempty"`
	RequiresNetwork      bool     `json:"requires_network,omitempty"`
}

func BuildPlan(request Request) (Plan, error) {
	mode := normalizedMode(request.Mode)

	if mode != ModeOff && strings.TrimSpace(request.Capabilities.SandboxProfile) != "" {
		var normalizeErr error

		request, normalizeErr = normalizeSandboxRequest(request)
		if normalizeErr != nil {
			return deniedSandboxPlan(request.evidence(mode), normalizeErr)
		}
	}

	evidence := request.evidence(mode)
	if mode == ModeOff || strings.TrimSpace(request.Capabilities.SandboxProfile) == "" {
		return unsandboxedPlan(request, evidence), nil
	}

	if !supportsBubblewrap() {
		return deniedSandboxPlan(evidence, errBubblewrapPlatform)
	}

	backendPath, err := resolveBackendPath(request.BackendPath)
	if err != nil {
		return deniedSandboxPlan(evidence, err)
	}

	evidence.Enabled = true
	evidence.BackendPath = backendPath
	args := bubblewrapArgs(request)

	extraFiles, seccompArgs, seccompEnabled, seccompErr := seccompPlanFiles(
		request.Capabilities.SeccompProfilePath,
	)
	if seccompErr != nil {
		return deniedSeccompPlan(evidence, seccompErr)
	}

	evidence.SeccompEnabled = seccompEnabled

	args = append(args, seccompArgs...)
	args = append(args, request.Executable)
	args = append(args, request.Args...)

	return Plan{
		Executable: backendPath,
		Args:       args,
		ExtraFiles: extraFiles,
		Evidence:   evidence,
	}, nil
}

func unsandboxedPlan(request Request, evidence Evidence) Plan {
	return Plan{
		Executable: request.Executable,
		Args:       append([]string(nil), request.Args...),
		Evidence:   evidence,
	}
}

func deniedSandboxPlan(evidence Evidence, cause error) (Plan, error) {
	evidence.Denied = true
	evidence.Reason = backendEvidenceReason(cause)

	return Plan{
			Evidence: evidence,
		}, fmt.Errorf(
			"%w: %w",
			ErrBackendUnavailable,
			cause,
		)
}

func backendEvidenceReason(cause error) string {
	if errors.Is(cause, errBubblewrapNotFound) {
		return errBubblewrapNotFound.Error()
	}

	return cause.Error()
}

func deniedSeccompPlan(evidence Evidence, cause error) (Plan, error) {
	evidence.Denied = true
	evidence.Reason = "seccomp profile could not be opened"

	return Plan{
			Evidence: evidence,
		}, fmt.Errorf(
			"%w: %w",
			ErrBackendUnavailable,
			cause,
		)
}

func seccompPlanFiles(profilePath string) ([]*os.File, []string, bool, error) {
	if strings.TrimSpace(profilePath) == "" {
		return nil, nil, false, nil
	}

	profile, err := os.Open(filepath.Clean(profilePath))
	if err != nil {
		return nil, nil, false, fmt.Errorf("open seccomp profile: %w", err)
	}

	return []*os.File{profile},
		[]string{"--seccomp", strconv.Itoa(firstExtraFileFD)},
		true,
		nil
}

func resolveBackendPath(configuredPath string) (string, error) {
	backendPath := strings.TrimSpace(configuredPath)
	if backendPath == "" {
		resolvedPath, err := exec.LookPath("bwrap")
		if err != nil {
			return "", fmt.Errorf("%w: %w", errBubblewrapNotFound, err)
		}

		return resolvedPath, nil
	}

	info, err := os.Stat(backendPath)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errBubblewrapNotFound, err)
	}

	if info.IsDir() {
		return "", errBubblewrapNotFound
	}

	return backendPath, nil
}

func (plan Plan) Close() error {
	var errs []error

	for _, file := range plan.ExtraFiles {
		if file == nil {
			continue
		}

		err := file.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func Execute(
	ctx context.Context,
	request Request,
	run func(executable string, args []string, evidence Evidence) error,
) (Evidence, error) {
	plan, err := BuildPlan(request)
	if err != nil {
		return plan.Evidence, err
	}

	if run == nil {
		return plan.Evidence, nil
	}

	return plan.Evidence, run(plan.Executable, plan.Args, plan.Evidence)
}

func bubblewrapArgs(request Request) []string {
	repoRoot := filepath.Clean(firstNonEmpty(request.RepoRoot, request.Cwd, "."))
	cwd := filepath.Clean(firstNonEmpty(request.Cwd, repoRoot))
	gitDir := filepath.Join(repoRoot, ".git")

	args := baseBubblewrapArgs(request.Capabilities.RequiresNetwork)
	args = append(args, sandboxParentDirArgs(repoRoot)...)
	args = append(args, sandboxRepoArgs(repoRoot, gitDir)...)
	args = append(
		args,
		sandboxReadBindArgs(repoRoot, request.Capabilities.ReadPaths)...)
	args = append(
		args,
		sandboxWriteBindArgs(repoRoot, gitDir, request.Capabilities.WritePaths)...,
	)
	args = append(args, sandboxExecutableArgs(request.Executable)...)

	return append(args, "--chdir", cwd)
}

func baseBubblewrapArgs(requiresNetwork bool) []string {
	args := []string{
		"--die-with-parent",
		"--unshare-pid",
		"--proc", virtualProcDir,
		"--dev", virtualDeviceDir,
		"--ro-bind", sandboxRootDir, sandboxRootDir,
		"--tmpfs", virtualTemporaryDir,
		"--tmpfs", virtualRootHomeDir,
		"--tmpfs", virtualHomeDir,
	}
	if !requiresNetwork {
		args = append(args, "--unshare-net")
	}

	return args
}

func sandboxParentDirArgs(repoRoot string) []string {
	args := []string{}

	for _, dir := range destinationParentDirs(repoRoot) {
		if sandboxDirRequired(dir) {
			args = append(args, "--dir", dir)
		}
	}

	return args
}

func sandboxRepoArgs(repoRoot, gitDir string) []string {
	args := []string{"--ro-bind", repoRoot, repoRoot}
	if pathExists(gitDir) {
		args = append(args, "--ro-bind", gitDir, gitDir)
	}

	return args
}

func sandboxReadBindArgs(repoRoot string, paths []string) []string {
	args := []string{}

	for _, path := range paths {
		bind := normalizedBindPath(repoRoot, path)
		if bind != "" && bind != repoRoot && pathExists(bind) {
			args = append(args, "--ro-bind", bind, bind)
		}
	}

	return args
}

func sandboxWriteBindArgs(repoRoot, gitDir string, paths []string) []string {
	args := []string{}

	for _, path := range paths {
		bind := normalizedBindPath(repoRoot, path)
		if writableSandboxBind(repoRoot, gitDir, path, bind) {
			args = append(args, "--bind", bind, bind)
		}
	}

	return args
}

func sandboxExecutableArgs(executable string) []string {
	if strings.TrimSpace(executable) == "" || !pathExists(executable) {
		return nil
	}

	args := []string{}

	for _, dir := range destinationParentDirs(executable) {
		if sandboxDirRequired(dir) {
			args = append(args, "--dir", dir)
		}
	}

	return append(args, "--ro-bind", executable, executable)
}

func writableSandboxBind(repoRoot, gitDir, requestedPath, bind string) bool {
	return bind != "" &&
		!isWithinPath(bind, gitDir) &&
		ensureBindPath(repoRoot, requestedPath, bind)
}

func (request Request) evidence(mode string) Evidence {
	return Evidence{
		Mode:                 mode,
		Backend:              BackendBubblewrap,
		Profile:              request.Capabilities.SandboxProfile,
		Tool:                 request.Tool,
		Command:              append([]string{request.Executable}, request.Args...),
		Tags:                 append([]string(nil), request.Capabilities.Tags...),
		HiddenCredentialDirs: []string{"/home", "/root"},
		ReadPaths:            append([]string(nil), request.Capabilities.ReadPaths...),
		WritePaths: sandboxWritePaths(
			request.RepoRoot,
			request.Cwd,
			request.Capabilities.WritePaths,
		),
		TimeoutSeconds:    request.Capabilities.TimeoutSeconds,
		MemoryMB:          request.Capabilities.MemoryMB,
		CPUQuotaPercent:   request.Capabilities.CPUQuotaPercent,
		RequiresNetwork:   request.Capabilities.RequiresNetwork,
		RequiresGit:       request.Capabilities.RequiresGit,
		RequiresEnv:       request.Capabilities.RequiresEnv,
		RequiresProcesses: request.Capabilities.RequiresProcesses,
		SeccompProfile:    request.Capabilities.SeccompProfile,
		GitReadOnly:       true,
		ReadOnlyRoot:      true,
		NetworkIsolated:   !request.Capabilities.RequiresNetwork,
		ProcessIsolated:   true,
		TimeoutEnforced:   request.Capabilities.TimeoutSeconds > 0,
		CgroupRequested: request.Capabilities.MemoryMB > 0 ||
			request.Capabilities.CPUQuotaPercent > 0,
	}
}

func CommandContext(
	ctx context.Context,
	timeoutSeconds int,
) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
}

func safeCgroupName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return toolFallbackName
	}

	value = strings.Map(safeCgroupRune, value)

	return value
}

func safeCgroupRune(character rune) rune {
	if (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') ||
		character == '-' ||
		character == '_' {
		return character
	}

	return '-'
}

func sandboxWritePaths(repoRoot, cwd string, paths []string) []string {
	root := filepath.Clean(firstNonEmpty(repoRoot, cwd, "."))
	gitDir := filepath.Join(root, ".git")
	writable := []string{}

	for _, path := range paths {
		bind := normalizedBindPath(root, path)
		if bind == "" || isWithinPath(bind, gitDir) {
			continue
		}

		writable = append(writable, path)
	}

	return writable
}

func normalizedMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModeOff:
		return ModeOff
	case ModeAuto:
		return ModeAuto
	case ModeRequired:
		return ModeRequired
	default:
		return ModeRequired
	}
}

func normalizedBindPath(repoRoot, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Clean(filepath.Join(repoRoot, path))
}

func normalizeSandboxRequest(request Request) (Request, error) {
	repoRoot, err := absoluteSandboxPath(
		firstNonEmpty(request.RepoRoot, request.Cwd, "."),
	)
	if err != nil {
		return request, fmt.Errorf(
			"sandbox repo root must resolve to an absolute path: %w",
			err,
		)
	}

	request.RepoRoot = repoRoot

	cwd, err := absoluteSandboxPath(firstNonEmpty(request.Cwd, repoRoot))
	if err != nil {
		return request, fmt.Errorf(
			"sandbox working directory must resolve to an absolute path: %w",
			err,
		)
	}

	request.Cwd = cwd

	if strings.TrimSpace(request.Executable) == "" {
		return request, errSandboxExecutable
	}

	executable, err := absoluteSandboxPath(request.Executable)
	if err != nil {
		return request, fmt.Errorf(
			"sandbox executable must resolve to an absolute path: %w",
			err,
		)
	}

	resolvedExecutable, resolveErr := filepath.EvalSymlinks(executable)
	if resolveErr == nil {
		executable = resolvedExecutable
	}

	request.Executable = executable

	return request, nil
}

func absoluteSandboxPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errEmptySandboxPath
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute sandbox path: %w", err)
	}

	return filepath.Clean(absolute), nil
}

func destinationParentDirs(path string) []string {
	cleaned := filepath.Clean(path)

	dirs := []string{}
	for dir := filepath.Dir(cleaned); dir != "." &&
		dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
	}

	for left, right := 0, len(dirs)-1; left < right; left, right = left+1, right-1 {
		dirs[left], dirs[right] = dirs[right], dirs[left]
	}

	return dirs
}

func sandboxDirRequired(path string) bool {
	roots := []string{virtualHomeDir, virtualRootHomeDir, virtualTemporaryDir}
	for _, root := range roots {
		if path == root || isWithinPath(path, root) {
			return true
		}
	}

	return false
}

func isWithinPath(path, parent string) bool {
	cleanPath := filepath.Clean(path)

	cleanParent := filepath.Clean(parent)
	if cleanPath == cleanParent {
		return true
	}

	relative, err := filepath.Rel(cleanParent, cleanPath)
	if err != nil {
		return false
	}

	return relative != "." &&
		relative != "" &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		relative != ".."
}

func pathExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func ensureBindPath(repoRoot, requestedPath, path string) bool {
	if pathExists(path) {
		return true
	}

	if filepath.IsAbs(requestedPath) || !isWithinPath(path, repoRoot) {
		return false
	}

	return os.MkdirAll(path, privateDirMode) == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
