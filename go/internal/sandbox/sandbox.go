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
	"runtime"
	"strings"
	"syscall"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/sandboxexec"
)

const (
	ModeRequired = "required"

	BackendNative = "native"

	cgroupLineParts             = 3
	toolFallbackName            = "tool"
	nativeSandboxBinary         = "coding-ethos-sandbox"
	nativeProbeTimeout          = 10
	nativeProbeDirMode          = 0o700
	nativeProbeFileMode         = 0o700
	nativeProbeWriteMode        = 0o600
	nativeProbeSandboxProfile   = "native-probe"
	activeAgentShellReuseReason = "reusing active agent-shell sandbox"
	SandboxTempWritePath        = ".coding-ethos/cache/sandbox-tmp"
	SandboxGoCachePath          = ".coding-ethos/cache/go-build"
	SandboxGoPath               = ".coding-ethos/cache/go-path"
	SandboxGoModCachePath       = SandboxGoPath + "/pkg/mod"
	SandboxGolangCIPath         = ".coding-ethos/cache/golangci-lint"
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
	StrategicIntent    string   `json:"strategic_intent,omitempty"`
	GitWrapperPath     string   `json:"git_wrapper_path,omitempty"`
	RealGitPath        string   `json:"real_git_path,omitempty"`
	RealGitBindPath    string   `json:"real_git_bind_path,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	GitTargetPaths     []string `json:"git_target_paths,omitempty"`
	ReadPaths          []string `json:"read_paths,omitempty"`
	WritePaths         []string `json:"write_paths,omitempty"`
	// ReadOnlyPaths must stay read-only even when a parent of theirs is
	// writable. Landlock cannot express that, so they are held by mounts.
	ReadOnlyPaths     []string `json:"read_only_paths,omitempty"`
	EnvBindings       []string `json:"env_bindings,omitempty"`
	CPUQuotaPercent   int      `json:"cpu_quota_percent,omitempty"`
	MemoryMB          int      `json:"memory_mb,omitempty"`
	TimeoutSeconds    int      `json:"timeout_seconds,omitempty"`
	AllowGitWrites    bool     `json:"allow_git_writes,omitempty"`
	RequiresNetwork   bool     `json:"requires_network,omitempty"`
	RequiresGit       bool     `json:"requires_git,omitempty"`
	RequiresEnv       bool     `json:"requires_env,omitempty"`
	RequiresProcesses bool     `json:"requires_processes,omitempty"`
}

type Request struct {
	BackendPath  string
	Cwd          string
	Executable   string
	RepoRoot     string
	Tool         string
	WrapperPath  string
	Args         []string
	Capabilities Capabilities
}

type Plan struct {
	Executable string
	ExtraFiles []*os.File
	Args       []string
	Evidence   Evidence
}

type Evidence struct {
	CgroupPath           string   `json:"cgroup_path,omitempty"`
	Backend              string   `json:"backend,omitempty"`
	BackendPath          string   `json:"backend_path,omitempty"`
	Profile              string   `json:"profile,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	SeccompProfile       string   `json:"seccomp_profile,omitempty"`
	StrategicIntent      string   `json:"strategic_intent,omitempty"`
	Mode                 string   `json:"mode,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	ReadPaths            []string `json:"read_paths,omitempty"`
	WritePaths           []string `json:"write_paths,omitempty"`
	ReadOnlyPaths        []string `json:"read_only_paths,omitempty"`
	EnvBindings          []string `json:"env_bindings,omitempty"`
	HiddenCredentialDirs []string `json:"hidden_credential_dirs,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Command              []string `json:"command,omitempty"`
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`
	MemoryMB             int      `json:"memory_mb,omitempty"`
	CPUQuotaPercent      int      `json:"cpu_quota_percent,omitempty"`
	RequiresGit          bool     `json:"requires_git,omitempty"`
	GitReadOnly          bool     `json:"git_read_only,omitempty"`
	RepoReadOnly         bool     `json:"repo_read_only,omitempty"`
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
	if !sandboxRequired(request) {
		return unsandboxedPlan(request, Evidence{}), nil
	}

	var normalizeErr error

	request, normalizeErr = normalizeSandboxRequest(request)
	if normalizeErr != nil {
		return deniedSandboxPlan(request.evidence(), normalizeErr)
	}

	evidence := request.evidence()
	evidence.Enabled = true
	evidence.NamespaceEnforced = !request.Capabilities.RequiresProcesses

	if strings.TrimSpace(request.Capabilities.SeccompProfilePath) != "" {
		return deniedSeccompPlan(evidence, errNativeSeccompUnsupported)
	}

	if verifiedActiveAgentShellSandboxCovers(request) &&
		activeAgentShellSandboxReusable(request) {
		evidence.Reason = activeAgentShellReuseReason
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

func sandboxRequired(request Request) bool {
	return runtime.GOOS == "linux" &&
		strings.TrimSpace(request.Capabilities.SandboxProfile) != ""
}

func activeAgentShellSandboxReusable(request Request) bool {
	return request.Capabilities.SandboxProfile != nativeProbeSandboxProfile &&
		!gitBindingRequested(
			request.Capabilities,
			normalizedGitTargetPaths(request.Capabilities.GitTargetPaths),
		)
}

func verifiedActiveAgentShellSandboxCovers(request Request) bool {
	if os.Getenv("CODING_ETHOS_AGENT_SHELL_SANDBOX") != "1" ||
		os.Getenv("CODING_ETHOS_SANDBOX_ACTIVE") != "1" {
		return false
	}

	root := filepath.Clean(strings.TrimSpace(os.Getenv("CODING_ETHOS_SANDBOX_ROOT")))

	realGit := filepath.Clean(strings.TrimSpace(os.Getenv("CODING_ETHOS_REAL_GIT")))
	if root == "." || realGit == "." {
		return false
	}

	repoRoot := filepath.Clean(strings.TrimSpace(request.RepoRoot))
	if repoRoot == "." || repoRoot != root ||
		!isWithinPath(request.Cwd, root) ||
		!isWithinPath(realGit, root) {
		return false
	}

	info, err := os.Stat(realGit)

	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
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
	wrapper, wrapperErr := nativeWrapperPath(Request{WrapperPath: wrapperPath})
	if wrapperErr != nil {
		evidence := nativeRuntimeEvidence()
		evidence.Denied = true
		evidence.Reason = backendEvidenceReason(wrapperErr)

		return evidence, fmt.Errorf(
			"%w: %w",
			ErrBackendUnavailable,
			wrapperErr,
		)
	}

	activeEvidence, active, activeErr := validateActiveAgentShellNativeRuntime(wrapper)
	if active {
		return activeEvidence, activeErr
	}

	repoRoot, err := os.MkdirTemp("", "coding-ethos-sandbox-probe-")
	if err != nil {
		return Evidence{}, fmt.Errorf("create native sandbox probe repo: %w", err)
	}

	defer func() { _ = os.RemoveAll(repoRoot) }()

	err = prepareNativeProbeRepo(repoRoot)
	if err != nil {
		return Evidence{}, err
	}

	plan, err := BuildPlan(Request{
		Tool:        "sandbox-probe",
		Executable:  "/bin/sh",
		WrapperPath: wrapper,
		Cwd:         repoRoot,
		RepoRoot:    repoRoot,
		Args:        nativeProbeArgs(),
		Capabilities: Capabilities{
			SandboxProfile: nativeProbeSandboxProfile,
			WritePaths:     []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		return plan.Evidence, err
	}

	evidence, err := runNativeRuntimeProbe(repoRoot, plan)
	if err != nil {
		return evidence, fmt.Errorf("run native filesystem probe: %w", err)
	}

	evidence, err = validateNativeProbeSideEffects(repoRoot, evidence)
	if err != nil {
		return evidence, err
	}

	evidence, err = validateNativeGitBindProbe(repoRoot, wrapper, evidence)
	if err != nil {
		return evidence, fmt.Errorf("run native git bind probe: %w", err)
	}

	return evidence, nil
}

func validateActiveAgentShellNativeRuntime(wrapper string) (Evidence, bool, error) {
	if !activeAgentShellNativeRuntime() {
		return Evidence{}, false, nil
	}

	evidence := nativeRuntimeEvidence()
	evidence.BackendPath = wrapper
	evidence.Enabled = true
	evidence.NamespaceEnforced = true

	root, validRoot := activeAgentShellSandboxRoot()
	if !validRoot {
		return deniedActiveAgentShellEvidence(
			evidence,
			"active agent-shell sandbox evidence is incomplete",
		)
	}

	protectedRoot, reason := activeAgentShellProtectedRoot(root)
	if reason != "" {
		return deniedActiveAgentShellEvidence(
			evidence,
			reason,
		)
	}

	reason = activeAgentShellCacheProbe(protectedRoot)
	if reason != "" {
		return deniedActiveAgentShellEvidence(
			evidence,
			reason,
		)
	}

	reason = activeAgentShellProtectedRootProbe(protectedRoot)
	if reason != "" {
		return deniedActiveAgentShellEvidence(
			evidence,
			reason,
		)
	}

	evidence.Reason = "validated active agent-shell native sandbox"

	return evidence, true, nil
}

func activeAgentShellNativeRuntime() bool {
	return os.Getenv("CODING_ETHOS_AGENT_SHELL_SANDBOX") == "1" &&
		os.Getenv("CODING_ETHOS_SANDBOX_ACTIVE") == "1"
}

func activeAgentShellSandboxRoot() (string, bool) {
	root := filepath.Clean(strings.TrimSpace(os.Getenv("CODING_ETHOS_SANDBOX_ROOT")))
	valid := root != "." && filepath.IsAbs(root) &&
		verifiedActiveAgentShellSandboxCovers(Request{RepoRoot: root, Cwd: root})

	return root, valid
}

func activeAgentShellProtectedRoot(root string) (string, string) {
	mountInfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Sprintf("read active agent-shell mount info: %v", err)
	}

	protectedRoot := filepath.Join(root, ".coding-ethos")
	if !sandboxexec.ReadOnlyMountInfoForPath(string(mountInfo), protectedRoot) {
		return "", "active agent-shell .coding-ethos root is not an exact read-only mount"
	}

	return protectedRoot, ""
}

func activeAgentShellCacheProbe(protectedRoot string) string {
	cacheRoot := filepath.Join(protectedRoot, "cache")

	probe, err := os.CreateTemp(cacheRoot, "native-runtime-active-probe-")
	if err != nil {
		return fmt.Sprintf("create active agent-shell cache probe: %v", err)
	}

	probePath := probe.Name()
	closeErr := probe.Close()

	removeErr := os.Remove(probePath)
	if closeErr != nil || removeErr != nil {
		return fmt.Sprintf(
			"clean active agent-shell cache probe: close=%v remove=%v",
			closeErr,
			removeErr,
		)
	}

	return ""
}

func activeAgentShellProtectedRootProbe(protectedRoot string) string {
	blockedPath := filepath.Join(protectedRoot, "native-runtime-active-probe-blocked")

	blocked, blockedErr := os.OpenFile(
		blockedPath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		nativeProbeWriteMode,
	)
	if blockedErr == nil {
		_ = blocked.Close()
		_ = os.Remove(blockedPath)

		return "active agent-shell protected root accepted an undeclared write"
	}

	if !errors.Is(blockedErr, os.ErrPermission) &&
		!errors.Is(blockedErr, syscall.EROFS) {
		return fmt.Sprintf("probe active agent-shell protected root: %v", blockedErr)
	}

	return ""
}

func deniedActiveAgentShellEvidence(
	evidence Evidence,
	reason string,
) (Evidence, bool, error) {
	evidence.Denied = true
	evidence.Reason = reason

	return evidence, true, fmt.Errorf("%w: %s", ErrBackendUnavailable, reason)
}

func prepareNativeProbeRepo(repoRoot string) error {
	writeRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")

	err := os.MkdirAll(writeRoot, nativeProbeDirMode)
	if err != nil {
		return fmt.Errorf("create native sandbox probe write path: %w", err)
	}

	return nil
}

func sandboxProbeExitCode(err error) int {
	return processstatus.ExitCode(err, 1)
}

func runNativeRuntimeProbe(repoRoot string, plan Plan) (Evidence, error) {
	defer func() { _ = plan.Close() }()

	ctx, cancel := CommandContext(context.Background(), nativeProbeTimeout)
	defer cancel()

	command := safeexec.CommandContext(ctx, plan.Executable, plan.Args...)
	command.Dir = repoRoot
	command.SysProcAttr = SysProcAttr(nil, plan.Evidence)

	argv := append([]string{plan.Executable}, plan.Args...)
	startedAt := debuglog.ProcessEnter(argv, repoRoot)
	output, runErr := command.CombinedOutput()
	debuglog.ProcessExit(startedAt, argv, repoRoot, sandboxProbeExitCode(runErr), runErr)

	if runErr != nil {
		plan.Evidence.Denied = true
		plan.Evidence.Reason = strings.TrimSpace(string(output))

		if plan.Evidence.Reason == "" {
			plan.Evidence.Reason = runErr.Error()
		}

		return plan.Evidence, fmt.Errorf(
			"%w: %s: %w",
			ErrBackendUnavailable,
			plan.Evidence.Reason,
			runErr,
		)
	}

	return plan.Evidence, nil
}

func nativeProbeArgs() []string {
	script := "printf ok > .coding-ethos/cache/probe && " +
		"if /bin/sh -c ': > blocked'; then exit 99; else exit 0; fi"

	return []string{"-c", script}
}

func validateNativeProbeSideEffects(
	repoRoot string,
	evidence Evidence,
) (Evidence, error) {
	allowedProbe := filepath.Join(repoRoot, ".coding-ethos", "cache", "probe")
	allowedContent, readErr := os.ReadFile(allowedProbe)

	if readErr != nil || strings.TrimSpace(string(allowedContent)) != "ok" {
		evidence.Denied = true
		evidence.Reason = "native sandbox did not permit declared write path"

		return evidence, fmt.Errorf(
			"%w: %s",
			ErrBackendUnavailable,
			evidence.Reason,
		)
	}

	blockedProbe := filepath.Join(repoRoot, "blocked")

	_, statErr := os.Stat(blockedProbe)
	if statErr == nil {
		evidence.Denied = true
		evidence.Reason = "native sandbox permitted undeclared repository write"

		return evidence, fmt.Errorf(
			"%w: %s",
			ErrBackendUnavailable,
			evidence.Reason,
		)
	}

	if !os.IsNotExist(statErr) {
		evidence.Denied = true
		evidence.Reason = statErr.Error()

		return evidence, fmt.Errorf(
			"%w: inspect blocked write probe: %w",
			ErrBackendUnavailable,
			statErr,
		)
	}

	return evidence, nil
}

func validateNativeGitBindProbe(
	repoRoot string,
	wrapper string,
	evidence Evidence,
) (Evidence, error) {
	probe, err := nativeGitBindProbe(repoRoot)
	if err != nil {
		evidence.Denied = true
		evidence.Reason = err.Error()

		return evidence, fmt.Errorf("prepare native git bind probe: %w", err)
	}

	plan, err := BuildPlan(Request{
		Tool:        "sandbox-git-bind-probe",
		Executable:  probe.targetGit,
		WrapperPath: wrapper,
		Cwd:         repoRoot,
		RepoRoot:    repoRoot,
		Args:        []string{sandboxexec.NativeGitBindProbeMode},
		Capabilities: Capabilities{
			SandboxProfile:  "native-git-bind-probe",
			GitWrapperPath:  wrapper,
			RealGitPath:     wrapper,
			RealGitBindPath: probe.realGitBind,
			GitTargetPaths:  []string{probe.targetGit},
			WritePaths:      []string{".coding-ethos/cache"},
			RequiresGit:     true,
		},
	})
	if err != nil {
		return plan.Evidence, err
	}

	probeEvidence, err := runNativeRuntimeProbe(repoRoot, plan)
	if err != nil {
		return probeEvidence, err
	}

	return validateNativeGitBindSideEffects(repoRoot, probeEvidence)
}

type nativeGitBindProbeFiles struct {
	realGitBind string
	targetGit   string
}

func nativeGitBindProbe(repoRoot string) (nativeGitBindProbeFiles, error) {
	cacheRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")

	err := os.MkdirAll(cacheRoot, nativeProbeDirMode)
	if err != nil {
		return nativeGitBindProbeFiles{}, fmt.Errorf("create probe cache: %w", err)
	}

	files := nativeGitBindProbeFiles{
		realGitBind: filepath.Join(cacheRoot, "real-git-bind-probe"),
		targetGit:   filepath.Join(cacheRoot, "target-git-probe"),
	}

	for _, path := range []string{files.realGitBind, files.targetGit} {
		err = writeNativeProbeExecutable(path, nil)
		if err != nil {
			return nativeGitBindProbeFiles{}, fmt.Errorf("write %s: %w", path, err)
		}
	}

	return files, nil
}

func writeNativeProbeExecutable(path string, content []byte) error {
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		nativeProbeWriteMode,
	)
	if err != nil {
		return fmt.Errorf("create native probe executable %s: %w", path, err)
	}

	_, writeErr := file.Write(content)
	closeErr := file.Close()

	if writeErr != nil {
		return fmt.Errorf("write native probe executable %s: %w", path, writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close native probe executable %s: %w", path, closeErr)
	}

	err = os.Chmod(path, nativeProbeFileMode)
	if err != nil {
		return fmt.Errorf("mark native probe executable %s executable: %w", path, err)
	}

	return nil
}

func validateNativeGitBindSideEffects(
	repoRoot string,
	evidence Evidence,
) (Evidence, error) {
	cacheRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")
	wrapperProbe := filepath.Join(cacheRoot, "git-wrapper")

	wrapperContent, readErr := os.ReadFile(wrapperProbe)
	if readErr != nil || strings.TrimSpace(string(wrapperContent)) != "wrapper" {
		evidence.Denied = true
		evidence.Reason = "native sandbox did not route git target through wrapper bind"

		return evidence, fmt.Errorf("%w: %s", ErrBackendUnavailable, evidence.Reason)
	}

	for marker, reason := range map[string]string{
		filepath.Join(cacheRoot, "git-target"): "native sandbox executed unwrapped " +
			"git target",
		filepath.Join(
			cacheRoot,
			"git-bind-not-read-only",
		): "native sandbox did not remount git wrapper bind read-only",
	} {
		_, statErr := os.Stat(marker)
		if statErr == nil {
			evidence.Denied = true
			evidence.Reason = reason

			return evidence, fmt.Errorf("%w: %s", ErrBackendUnavailable, reason)
		}

		if !os.IsNotExist(statErr) {
			evidence.Denied = true
			evidence.Reason = statErr.Error()

			return evidence, fmt.Errorf(
				"%w: inspect native git bind probe: %w",
				ErrBackendUnavailable,
				statErr,
			)
		}
	}

	return evidence, nil
}

func (request Request) evidence() Evidence {
	writePaths := sandboxWritePaths(
		request.RepoRoot,
		request.Cwd,
		request.Capabilities.WritePaths,
		request.Capabilities.AllowGitWrites,
	)
	writePaths = append(writePaths, SandboxTempWritePath)
	writePaths = append(writePaths, SandboxGoCachePath)
	writePaths = append(writePaths, SandboxGoPath)
	writePaths = append(writePaths, SandboxGolangCIPath)
	writePaths = append(writePaths, nativeSystemWritePaths()...)
	readPaths := append([]string(nil), request.Capabilities.ReadPaths...)
	readPaths = append(readPaths, writePaths...)

	return Evidence{
		Mode:              ModeRequired,
		Backend:           BackendNative,
		Profile:           request.Capabilities.SandboxProfile,
		Tool:              request.Tool,
		Command:           append([]string{request.Executable}, request.Args...),
		Tags:              append([]string(nil), request.Capabilities.Tags...),
		ReadPaths:         readPaths,
		WritePaths:        writePaths,
		ReadOnlyPaths:     request.Capabilities.ReadOnlyPaths,
		EnvBindings:       append([]string(nil), request.Capabilities.EnvBindings...),
		TimeoutSeconds:    request.Capabilities.TimeoutSeconds,
		MemoryMB:          request.Capabilities.MemoryMB,
		CPUQuotaPercent:   request.Capabilities.CPUQuotaPercent,
		RequiresNetwork:   request.Capabilities.RequiresNetwork,
		RequiresGit:       request.Capabilities.RequiresGit,
		RequiresEnv:       request.Capabilities.RequiresEnv,
		RequiresProcesses: request.Capabilities.RequiresProcesses,
		SeccompProfile:    request.Capabilities.SeccompProfile,
		StrategicIntent:   request.Capabilities.StrategicIntent,
		GitReadOnly:       !request.Capabilities.AllowGitWrites,
		RepoReadOnly:      true,
		NetworkIsolated: !request.Capabilities.RequiresProcesses &&
			!request.Capabilities.RequiresNetwork,
		ProcessIsolated: !request.Capabilities.RequiresProcesses,
		TimeoutEnforced: request.Capabilities.TimeoutSeconds > 0,
		CgroupRequested: request.Capabilities.MemoryMB > 0 ||
			request.Capabilities.CPUQuotaPercent > 0,
	}
}

func nativeSystemWritePaths() []string {
	return []string{os.DevNull}
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

func sandboxWritePaths(
	repoRoot, cwd string,
	paths []string,
	allowGitWrites bool,
) []string {
	root := filepath.Clean(firstNonEmpty(repoRoot, cwd, "."))
	gitDir := filepath.Join(root, ".git")
	writable := []string{}

	for _, path := range paths {
		bind := normalizedBindPath(root, path)
		if bind == "" || (!allowGitWrites && isWithinPath(bind, gitDir)) {
			continue
		}

		writable = append(writable, path)
	}

	return writable
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
		path = filepath.Join(filepath.Dir(request.Executable), nativeSandboxBinaryName())
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

	if info.IsDir() ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		return "", fmt.Errorf("%w: %s is not executable", errSandboxWrapper, wrapper)
	}

	return wrapper, nil
}

func nativeSandboxBinaryName() string {
	if runtime.GOOS == "windows" {
		return nativeSandboxBinary + ".exe"
	}

	return nativeSandboxBinary
}

func nativeWrapperArgs(request Request, writePaths []string) []string {
	gitTargets := normalizedGitTargetPaths(request.Capabilities.GitTargetPaths)
	args := make(
		[]string,
		0,
		4+2*len(writePaths)+2+2*len(gitTargets)+2+len(request.Args),
	)
	args = append(args,
		"--cwd",
		request.Cwd,
		"--repo-root",
		request.RepoRoot,
	)

	for _, path := range writePaths {
		args = append(args, "--write-path", path)
	}

	for _, path := range request.Capabilities.ReadOnlyPaths {
		args = append(args, "--read-only-path", path)
	}

	// Network stays reachable unless both process and network isolation apply;
	// only then must the helper re-present /etc/ssh for the mapped identity.
	if request.Capabilities.RequiresNetwork || request.Capabilities.RequiresProcesses {
		args = append(args, "--requires-network")
	}

	if request.Capabilities.RequiresGit {
		if gitBindingRequested(request.Capabilities, gitTargets) {
			args = append(args, "--git-wrapper", request.Capabilities.GitWrapperPath)

			args = append(
				args,
				"--real-git-path",
				request.Capabilities.RealGitPath,
				"--real-git-bind",
				request.Capabilities.RealGitBindPath,
			)
			for _, path := range gitTargets {
				args = append(args, "--git-target", path)
			}
		}

		if request.Capabilities.AllowGitWrites {
			args = append(args, "--allow-git-writes")
		}
	}

	args = append(args, "--", request.Executable)
	args = append(args, request.Args...)

	return args
}

func gitBindingRequested(capabilities Capabilities, gitTargets []string) bool {
	return strings.TrimSpace(capabilities.GitWrapperPath) != "" ||
		strings.TrimSpace(capabilities.RealGitPath) != "" ||
		strings.TrimSpace(capabilities.RealGitBindPath) != "" ||
		len(gitTargets) > 0
}

func normalizedGitTargetPaths(paths []string) []string {
	targets := []string{}
	seen := map[string]struct{}{}

	for _, path := range paths {
		normalized := strings.TrimSpace(path)
		if normalized == "" {
			continue
		}

		cleaned := filepath.Clean(normalized)

		resolved, err := filepath.EvalSymlinks(cleaned)
		if err == nil {
			cleaned = filepath.Clean(resolved)
		}

		if _, found := seen[cleaned]; found {
			continue
		}

		seen[cleaned] = struct{}{}
		targets = append(targets, cleaned)
	}

	return targets
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
