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
)

const (
	ModeOff      = "off"
	ModeAuto     = "auto"
	ModeRequired = "required"

	BackendBubblewrap = "bubblewrap"
)

var ErrBackendUnavailable = errors.New("sandbox backend unavailable")
var supportsBubblewrap = func() bool { return runtime.GOOS == "linux" }
var cgroupRootPath = func() string {
	for _, path := range cgroupRootCandidates() {
		if writableCgroupRoot(path) {
			return path
		}
	}

	return ""
}

func cgroupRootCandidates() []string {
	candidates := []string{}
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) == 3 && parts[0] == "0" {
				candidates = append(candidates, filepath.Join("/sys/fs/cgroup", parts[2]))
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
	if _, err := os.Stat(filepath.Join(path, "cgroup.controllers")); err != nil {
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
	Tags               []string `json:"tags,omitempty"`
	ReadPaths          []string `json:"read_paths,omitempty"`
	WritePaths         []string `json:"write_paths,omitempty"`
	SandboxProfile     string   `json:"sandbox_profile,omitempty"`
	TimeoutSeconds     int      `json:"timeout_seconds,omitempty"`
	MemoryMB           int      `json:"memory_mb,omitempty"`
	CPUQuotaPercent    int      `json:"cpu_quota_percent,omitempty"`
	RequiresNetwork    bool     `json:"requires_network,omitempty"`
	RequiresGit        bool     `json:"requires_git,omitempty"`
	RequiresEnv        bool     `json:"requires_env,omitempty"`
	RequiresProcesses  bool     `json:"requires_processes,omitempty"`
	SeccompProfile     string   `json:"seccomp_profile,omitempty"`
	SeccompProfilePath string   `json:"seccomp_profile_path,omitempty"`
}

type Request struct {
	Mode        string
	Tool        string
	Executable  string
	Cwd         string
	RepoRoot    string
	Args        []string
	BackendPath string
	Capabilities
}

type Plan struct {
	Executable string
	Args       []string
	ExtraFiles []*os.File
	Evidence   Evidence
}

type Evidence struct {
	Mode                 string   `json:"mode,omitempty"`
	Backend              string   `json:"backend,omitempty"`
	BackendPath          string   `json:"backend_path,omitempty"`
	Profile              string   `json:"profile,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	Command              []string `json:"command,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	HiddenCredentialDirs []string `json:"hidden_credential_dirs,omitempty"`
	ReadPaths            []string `json:"read_paths,omitempty"`
	WritePaths           []string `json:"write_paths,omitempty"`
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`
	MemoryMB             int      `json:"memory_mb,omitempty"`
	CPUQuotaPercent      int      `json:"cpu_quota_percent,omitempty"`
	RequiresNetwork      bool     `json:"requires_network,omitempty"`
	RequiresGit          bool     `json:"requires_git,omitempty"`
	RequiresEnv          bool     `json:"requires_env,omitempty"`
	RequiresProcesses    bool     `json:"requires_processes,omitempty"`
	GitReadOnly          bool     `json:"git_read_only,omitempty"`
	ReadOnlyRoot         bool     `json:"read_only_root,omitempty"`
	NetworkIsolated      bool     `json:"network_isolated,omitempty"`
	ProcessIsolated      bool     `json:"process_isolated,omitempty"`
	TimeoutEnforced      bool     `json:"timeout_enforced,omitempty"`
	CgroupRequested      bool     `json:"cgroup_requested,omitempty"`
	CgroupEnabled        bool     `json:"cgroup_enabled,omitempty"`
	CgroupPath           string   `json:"cgroup_path,omitempty"`
	SeccompProfile       string   `json:"seccomp_profile,omitempty"`
	SeccompEnabled       bool     `json:"seccomp_enabled,omitempty"`
	Enabled              bool     `json:"enabled"`
	Denied               bool     `json:"denied,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

func BuildPlan(request Request) (Plan, error) {
	mode := normalizedMode(request.Mode)
	var normalizeErr error
	if mode != ModeOff && strings.TrimSpace(request.SandboxProfile) != "" {
		request, normalizeErr = normalizeSandboxRequest(request)
	}
	evidence := request.evidence(mode)
	if normalizeErr != nil {
		evidence.Denied = mode == ModeRequired
		evidence.Reason = normalizeErr.Error()
		if mode == ModeRequired {
			return Plan{Evidence: evidence}, fmt.Errorf("%w: %v", ErrBackendUnavailable, normalizeErr)
		}

		return Plan{
			Executable: request.Executable,
			Args:       append([]string(nil), request.Args...),
			Evidence:   evidence,
		}, nil
	}
	if mode == ModeOff || strings.TrimSpace(request.SandboxProfile) == "" {
		return Plan{
			Executable: request.Executable,
			Args:       append([]string(nil), request.Args...),
			Evidence:   evidence,
		}, nil
	}
	if !supportsBubblewrap() {
		evidence.Denied = mode == ModeRequired
		evidence.Reason = "bubblewrap sandbox requires Linux namespace support"
		if mode == ModeRequired {
			return Plan{Evidence: evidence}, ErrBackendUnavailable
		}

		return Plan{
			Executable: request.Executable,
			Args:       append([]string(nil), request.Args...),
			Evidence:   evidence,
		}, nil
	}

	backendPath := strings.TrimSpace(request.BackendPath)
	if backendPath != "" {
		if info, err := os.Stat(backendPath); err != nil || info.IsDir() {
			evidence.Denied = mode == ModeRequired
			evidence.Reason = "bubblewrap executable not found"
			if mode == ModeRequired {
				return Plan{Evidence: evidence}, ErrBackendUnavailable
			}

			return Plan{
				Executable: request.Executable,
				Args:       append([]string(nil), request.Args...),
				Evidence:   evidence,
			}, nil
		}
	} else {
		var err error
		backendPath, err = exec.LookPath("bwrap")
		if err != nil {
			evidence.Denied = mode == ModeRequired
			evidence.Reason = "bubblewrap executable not found"
			if mode == ModeRequired {
				return Plan{Evidence: evidence}, ErrBackendUnavailable
			}

			return Plan{
				Executable: request.Executable,
				Args:       append([]string(nil), request.Args...),
				Evidence:   evidence,
			}, nil
		}
	}

	evidence.Enabled = true
	evidence.BackendPath = backendPath
	args := bubblewrapArgs(request)
	extraFiles := []*os.File{}
	if strings.TrimSpace(request.SeccompProfilePath) != "" {
		profile, err := os.Open(filepath.Clean(request.SeccompProfilePath))
		if err != nil {
			evidence.Denied = mode == ModeRequired
			evidence.Reason = "seccomp profile could not be opened"
			if mode == ModeRequired {
				return Plan{Evidence: evidence}, fmt.Errorf("%w: %v", ErrBackendUnavailable, err)
			}
		} else {
			fd := 3 + len(extraFiles)
			extraFiles = append(extraFiles, profile)
			evidence.SeccompEnabled = true
			args = append(args, "--seccomp", strconv.Itoa(fd))
		}
	}
	args = append(args, request.Executable)
	args = append(args, request.Args...)

	return Plan{Executable: backendPath, Args: args, ExtraFiles: extraFiles, Evidence: evidence}, nil
}

func (plan Plan) Close() error {
	var errs []error
	for _, file := range plan.ExtraFiles {
		if file == nil {
			continue
		}
		if err := file.Close(); err != nil {
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

	args := []string{
		"--die-with-parent",
		"--unshare-pid",
		"--proc", "/proc",
		"--dev", "/dev",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--tmpfs", "/root",
		"--tmpfs", "/home",
	}
	if !request.RequiresNetwork {
		args = append(args, "--unshare-net")
	}

	for _, dir := range destinationParentDirs(repoRoot) {
		if sandboxDirRequired(dir) {
			args = append(args, "--dir", dir)
		}
	}
	args = append(args, "--ro-bind", repoRoot, repoRoot)
	if pathExists(gitDir) {
		args = append(args, "--ro-bind", gitDir, gitDir)
	}
	for _, path := range request.ReadPaths {
		if bind := normalizedBindPath(repoRoot, path); bind != "" && bind != repoRoot {
			if pathExists(bind) {
				args = append(args, "--ro-bind", bind, bind)
			}
		}
	}
	for _, path := range request.WritePaths {
		if bind := normalizedBindPath(repoRoot, path); bind != "" && !isWithinPath(bind, gitDir) {
			if ensureBindPath(repoRoot, path, bind) {
				args = append(args, "--bind", bind, bind)
			}
		}
	}

	return append(args, "--chdir", cwd)
}

func (request Request) evidence(mode string) Evidence {
	return Evidence{
		Mode:                 mode,
		Backend:              BackendBubblewrap,
		Profile:              request.SandboxProfile,
		Tool:                 request.Tool,
		Command:              append([]string{request.Executable}, request.Args...),
		Tags:                 append([]string(nil), request.Tags...),
		HiddenCredentialDirs: []string{"/home", "/root"},
		ReadPaths:            append([]string(nil), request.ReadPaths...),
		WritePaths:           sandboxWritePaths(request.RepoRoot, request.Cwd, request.WritePaths),
		TimeoutSeconds:       request.TimeoutSeconds,
		MemoryMB:             request.MemoryMB,
		CPUQuotaPercent:      request.CPUQuotaPercent,
		RequiresNetwork:      request.RequiresNetwork,
		RequiresGit:          request.RequiresGit,
		RequiresEnv:          request.RequiresEnv,
		RequiresProcesses:    request.RequiresProcesses,
		SeccompProfile:       request.SeccompProfile,
		GitReadOnly:          true,
		ReadOnlyRoot:         true,
		NetworkIsolated:      !request.RequiresNetwork,
		ProcessIsolated:      true,
		TimeoutEnforced:      request.TimeoutSeconds > 0,
		CgroupRequested:      request.MemoryMB > 0 || request.CPUQuotaPercent > 0,
	}
}

func CommandContext(ctx context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
}

func safeCgroupName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' ||
			r == '_' {
			return r
		}

		return '-'
	}, value)

	return value
}

func sandboxWritePaths(repoRoot string, cwd string, paths []string) []string {
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

func normalizedBindPath(repoRoot string, path string) string {
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
	repoRoot, err := absoluteSandboxPath(firstNonEmpty(request.RepoRoot, request.Cwd, "."))
	if err != nil {
		return request, fmt.Errorf("sandbox repo root must resolve to an absolute path: %w", err)
	}
	request.RepoRoot = repoRoot

	cwd, err := absoluteSandboxPath(firstNonEmpty(request.Cwd, repoRoot))
	if err != nil {
		return request, fmt.Errorf("sandbox working directory must resolve to an absolute path: %w", err)
	}
	request.Cwd = cwd

	if strings.TrimSpace(request.Executable) == "" {
		return request, errors.New("sandbox executable is required")
	}
	executable, err := absoluteSandboxPath(request.Executable)
	if err != nil {
		return request, fmt.Errorf("sandbox executable must resolve to an absolute path: %w", err)
	}
	request.Executable = executable

	return request, nil
}

func absoluteSandboxPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return filepath.Clean(absolute), nil
}

func destinationParentDirs(path string) []string {
	cleaned := filepath.Clean(path)
	dirs := []string{}
	for dir := filepath.Dir(cleaned); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
	}

	for left, right := 0, len(dirs)-1; left < right; left, right = left+1, right-1 {
		dirs[left], dirs[right] = dirs[right], dirs[left]
	}

	return dirs
}

func sandboxDirRequired(path string) bool {
	for _, root := range []string{"/home", "/root", "/tmp"} {
		if path == root || isWithinPath(path, root) {
			return true
		}
	}

	return false
}

func isWithinPath(path string, parent string) bool {
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

func ensureBindPath(repoRoot string, requestedPath string, path string) bool {
	if pathExists(path) {
		return true
	}
	if filepath.IsAbs(requestedPath) || !isWithinPath(path, repoRoot) {
		return false
	}

	return os.MkdirAll(path, 0o700) == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
