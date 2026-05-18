// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package sandbox builds and executes OS sandbox requests for managed tools.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	ModeOff      = "off"
	ModeAuto     = "auto"
	ModeRequired = "required"

	BackendNative = "native"

	cgroupLineParts     = 3
	toolFallbackName    = "tool"
	nativeSandboxBinary = "coding-ethos-sandbox"
	nativeProbeTimeout  = 10
)

var (
	ErrBackendUnavailable       = apperror.StaticError("sandbox backend unavailable")
	errEmptySandboxPath         = apperror.StaticError("empty path")
	errSandboxExecutable        = apperror.StaticError("sandbox executable is required")
	errSandboxWrapper           = apperror.StaticError("sandbox wrapper is required")
	errNativeSeccompUnsupported = apperror.StaticError(
		"native sandbox seccomp profiles are not implemented",
	)
)

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
	WrapperPath  string
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
	NamespaceEnforced    bool     `json:"namespace_enforced,omitempty"`
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

	evidence.Enabled = true
	evidence.NamespaceEnforced = nativeNamespaceSupported()

	if strings.TrimSpace(request.Capabilities.SeccompProfilePath) != "" {
		return deniedSeccompPlan(evidence, errNativeSeccompUnsupported)
	}

	wrapper, wrapperErr := nativeWrapperPath(request)
	if wrapperErr != nil {
		return deniedSandboxPlan(evidence, wrapperErr)
	}

	evidence.BackendPath = wrapper

	return Plan{
		Executable: wrapper,
		Args:       nativeWrapperArgs(request, evidence.WritePaths),
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
	return cause.Error()
}

func deniedSeccompPlan(evidence Evidence, cause error) (Plan, error) {
	evidence.Denied = true
	evidence.Reason = cause.Error()

	return Plan{
			Evidence: evidence,
		}, fmt.Errorf(
			"%w: %w",
			ErrBackendUnavailable,
			cause,
		)
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

// ValidateNativeRuntimeWithHelper proves the wrapper can apply native sandbox
// policy and execute a trivial command.
func ValidateNativeRuntimeWithHelper(wrapperPath string) (Evidence, error) {
	repoRoot, err := os.MkdirTemp("", "coding-ethos-sandbox-probe-")
	if err != nil {
		return Evidence{}, fmt.Errorf("create native sandbox probe repo: %w", err)
	}

	defer func() { _ = os.RemoveAll(repoRoot) }()

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "sandbox-probe",
		Executable:  "/bin/true",
		WrapperPath: wrapperPath,
		Cwd:         repoRoot,
		RepoRoot:    repoRoot,
		Capabilities: Capabilities{
			SandboxProfile: "native-probe",
			WritePaths:     []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		return plan.Evidence, err
	}

	defer func() { _ = plan.Close() }()

	ctx, cancel := CommandContext(context.Background(), nativeProbeTimeout)
	defer cancel()

	command := safeexec.CommandContext(ctx, plan.Executable, plan.Args...)
	command.Dir = repoRoot
	command.SysProcAttr = SysProcAttr(nil, plan.Evidence)

	output, runErr := command.CombinedOutput()
	if runErr != nil {
		plan.Evidence.Denied = true
		plan.Evidence.Reason = strings.TrimSpace(string(output))

		if plan.Evidence.Reason == "" {
			plan.Evidence.Reason = runErr.Error()
		}

		return plan.Evidence, fmt.Errorf("%w: %w", ErrBackendUnavailable, runErr)
	}

	return plan.Evidence, nil
}

func (request Request) evidence(mode string) Evidence {
	return Evidence{
		Mode:                 mode,
		Backend:              BackendNative,
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
		ReadOnlyRoot:      nativeNamespaceSupported(),
		NetworkIsolated:   !request.Capabilities.RequiresNetwork,
		ProcessIsolated:   nativeNamespaceSupported(),
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

func nativeWrapperPath(request Request) (string, error) {
	path := strings.TrimSpace(request.WrapperPath)
	if path == "" {
		path = strings.TrimSpace(request.BackendPath)
	}

	if path == "" {
		path = filepath.Join(filepath.Dir(request.Executable), nativeSandboxBinary)
	}

	wrapper, err := absoluteSandboxPath(path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errSandboxWrapper, err)
	}

	resolved, resolveErr := filepath.EvalSymlinks(wrapper)
	if resolveErr == nil {
		wrapper = resolved
	}

	info, statErr := os.Stat(wrapper)
	if statErr != nil {
		return "", fmt.Errorf("%w: %w", errSandboxWrapper, statErr)
	}

	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: %s is not executable", errSandboxWrapper, wrapper)
	}

	return wrapper, nil
}

func nativeWrapperArgs(request Request, writePaths []string) []string {
	args := []string{
		"--cwd",
		request.Cwd,
		"--repo-root",
		request.RepoRoot,
	}

	if request.Capabilities.RequiresNetwork {
		args = append(args, "--network")
	}

	for _, path := range request.Capabilities.ReadPaths {
		args = append(args, "--read-path", path)
	}

	for _, path := range writePaths {
		args = append(args, "--write-path", path)
	}

	args = append(args, "--", request.Executable)
	args = append(args, request.Args...)

	return args
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
