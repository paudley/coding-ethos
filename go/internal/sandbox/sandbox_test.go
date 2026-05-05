// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestBuildPlanOffReturnsOriginalCommand(t *testing.T) {
	plan, err := BuildPlan(Request{
		Mode:         ModeOff,
		Tool:         "ruff",
		Executable:   "/tools/ruff",
		Args:         []string{"check", "pkg"},
		Capabilities: Capabilities{SandboxProfile: "lint-offline"},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Executable != "/tools/ruff" {
		t.Fatalf("executable = %q", plan.Executable)
	}
	if !slices.Equal(plan.Args, []string{"check", "pkg"}) {
		t.Fatalf("args = %#v", plan.Args)
	}
	if plan.Evidence.Enabled {
		t.Fatalf("sandbox should be disabled: %#v", plan.Evidence)
	}
}

func TestBuildPlanRequiredWithoutBackendDenies(t *testing.T) {
	plan, err := BuildPlan(Request{
		Mode:         ModeRequired,
		Tool:         "ruff",
		Executable:   "/tools/ruff",
		BackendPath:  filepath.Join(t.TempDir(), "missing-bwrap"),
		Capabilities: Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("BuildPlan() error = %v, want ErrBackendUnavailable", err)
	}
	if !plan.Evidence.Denied {
		t.Fatalf("denial evidence missing: %#v", plan.Evidence)
	}
	if plan.Evidence.Reason == "" {
		t.Fatalf("denial reason missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanAutoWithoutBackendFallsBackWithEvidence(t *testing.T) {
	plan, err := BuildPlan(Request{
		Mode:         ModeAuto,
		Tool:         "ruff",
		Executable:   "/tools/ruff",
		Args:         []string{"check"},
		BackendPath:  filepath.Join(t.TempDir(), "missing-bwrap"),
		Capabilities: Capabilities{SandboxProfile: "lint-offline"},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Executable != "/tools/ruff" || !slices.Equal(plan.Args, []string{"check"}) {
		t.Fatalf("fallback command mismatch: %#v", plan)
	}
	if plan.Evidence.Enabled || plan.Evidence.Denied {
		t.Fatalf("auto fallback should not claim enforcement or denial: %#v", plan.Evidence)
	}
	if plan.Evidence.Reason == "" {
		t.Fatalf("auto fallback should record backend reason: %#v", plan.Evidence)
	}
}

func TestBuildPlanRequiredOnUnsupportedPlatformFailsClosed(t *testing.T) {
	original := supportsBubblewrap
	supportsBubblewrap = func() bool { return false }
	t.Cleanup(func() { supportsBubblewrap = original })

	plan, err := BuildPlan(Request{
		Mode:         ModeRequired,
		Tool:         "ruff",
		Executable:   "/tools/ruff",
		Capabilities: Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("BuildPlan() error = %v, want ErrBackendUnavailable", err)
	}
	if !plan.Evidence.Denied {
		t.Fatalf("required sandbox should fail closed: %#v", plan.Evidence)
	}
	if plan.Evidence.Reason == "" {
		t.Fatalf("unsupported platform reason missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanAutoOnUnsupportedPlatformFallsBackWithEvidence(t *testing.T) {
	original := supportsBubblewrap
	supportsBubblewrap = func() bool { return false }
	t.Cleanup(func() { supportsBubblewrap = original })

	plan, err := BuildPlan(Request{
		Mode:         ModeAuto,
		Tool:         "ruff",
		Executable:   "/tools/ruff",
		Args:         []string{"check"},
		Capabilities: Capabilities{SandboxProfile: "lint-offline"},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Executable != "/tools/ruff" || !slices.Equal(plan.Args, []string{"check"}) {
		t.Fatalf("fallback command mismatch: %#v", plan)
	}
	if plan.Evidence.Enabled || plan.Evidence.Denied {
		t.Fatalf("auto unsupported platform should degrade without denial: %#v", plan.Evidence)
	}
	if plan.Evidence.Reason == "" {
		t.Fatalf("unsupported platform reason missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanUsesBubblewrapAndDisablesNetworkByDefault(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("create git dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, "pkg"), 0o700); err != nil {
		t.Fatalf("create read path: %v", err)
	}
	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"check", "pkg"},
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"pkg"},
			WritePaths:     []string{".coding-ethos/cache", ".git/config"},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Executable != backend {
		t.Fatalf("executable = %q", plan.Executable)
	}
	for _, want := range []string{
		"--die-with-parent",
		"--unshare-pid",
		"--unshare-net",
		"--dev",
		"/dev",
		"--ro-bind",
		"/",
		"--tmpfs",
		"/home",
		"--tmpfs",
		"/root",
		"--ro-bind",
		filepath.Join(repo, "pkg"),
		"--ro-bind",
		filepath.Join(repo, ".git"),
		"--bind",
		filepath.Join(repo, ".coding-ethos/cache"),
		"/tools/ruff",
		"check",
		"pkg",
	} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}
	if !plan.Evidence.Enabled || plan.Evidence.Backend != BackendBubblewrap {
		t.Fatalf("evidence mismatch: %#v", plan.Evidence)
	}
	if !plan.Evidence.ReadOnlyRoot || !plan.Evidence.GitReadOnly {
		t.Fatalf("mount evidence mismatch: %#v", plan.Evidence)
	}
	if !plan.Evidence.NetworkIsolated || !plan.Evidence.ProcessIsolated {
		t.Fatalf("namespace evidence mismatch: %#v", plan.Evidence)
	}
	if !slices.Equal(plan.Evidence.HiddenCredentialDirs, []string{"/home", "/root"}) {
		t.Fatalf("hidden credential dirs = %#v", plan.Evidence.HiddenCredentialDirs)
	}
	if slices.Contains(plan.Args, filepath.Join(repo, ".git/config")) {
		t.Fatalf(".git write bind must be filtered: %#v", plan.Args)
	}
	if slices.Contains(plan.Args, "--dev-bind") {
		t.Fatalf("full device tree must not be exposed: %#v", plan.Args)
	}
	if sandboxArgIndex(plan.Args, "/tools/ruff") <= sandboxArgIndex(plan.Args, "--unshare-net") {
		t.Fatalf("tool executable must be inside bwrap argument boundary: %#v", plan.Args)
	}
	if slices.Contains(plan.Evidence.WritePaths, ".git/config") {
		t.Fatalf(".git write evidence must be filtered: %#v", plan.Evidence)
	}
	if _, err := os.Stat(filepath.Join(repo, ".coding-ethos/cache")); err != nil {
		t.Fatalf("write bind path should be created before bwrap args: %v", err)
	}
}

func TestBuildPlanConstrainsChildrenAndStaticBinaries(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	executable := filepath.Join(repo, "tools", "static-tool")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/sh -c 'true'\n"), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "static-tool",
		Executable:  executable,
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
			TimeoutSeconds: 30,
			MemoryMB:       512,
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	for _, want := range []string{
		"--die-with-parent",
		"--unshare-pid",
		"--unshare-net",
		"--ro-bind",
		"/",
		"--tmpfs",
		"/home",
		"--tmpfs",
		"/root",
	} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}
	if sandboxArgIndex(plan.Args, executable) <= sandboxArgIndex(plan.Args, "--chdir") {
		t.Fatalf("tool executable must be launched after sandbox setup: %#v", plan.Args)
	}
	if !plan.Evidence.NetworkIsolated || !plan.Evidence.ProcessIsolated {
		t.Fatalf("child isolation evidence missing: %#v", plan.Evidence)
	}
	if !plan.Evidence.ReadOnlyRoot || !plan.Evidence.TimeoutEnforced ||
		!plan.Evidence.CgroupRequested {
		t.Fatalf("runtime containment evidence missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanSkipsMissingReadPath(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"missing"},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if slices.Contains(plan.Args, filepath.Join(repo, "missing")) {
		t.Fatalf("missing read bind should not be emitted: %#v", plan.Args)
	}
}

func TestBuildPlanAddsSeccompProfileFD(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "seccomp.bpf")
	if err := os.WriteFile(profile, []byte("profile"), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	backend := fakeBubblewrap(t)

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile:     "lint-offline",
			SeccompProfile:     "deny-privilege",
			SeccompProfilePath: profile,
		},
	})
	t.Cleanup(func() {
		if err := plan.Close(); err != nil {
			t.Fatalf("close plan: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	for _, want := range []string{"--seccomp", "3"} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}
	if len(plan.ExtraFiles) != 1 {
		t.Fatalf("extra files = %d, want 1", len(plan.ExtraFiles))
	}
	if !plan.Evidence.SeccompEnabled ||
		plan.Evidence.SeccompProfile != "deny-privilege" {
		t.Fatalf("seccomp evidence mismatch: %#v", plan.Evidence)
	}
}

func TestBuildPlanNormalizesRelativeSandboxPaths(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	executable := filepath.Join(repo, "tools", "ruff")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	previousCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousCwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  filepath.Join("tools", "ruff"),
		Cwd:         ".",
		RepoRoot:    ".",
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if !filepath.IsAbs(plan.Evidence.Command[0]) {
		t.Fatalf("command executable should be absolute: %#v", plan.Evidence.Command)
	}
	if !slices.Contains(plan.Args, executable) {
		t.Fatalf("sandbox args missing absolute executable %q: %#v", executable, plan.Args)
	}
}

func TestBuildPlanDoesNotCreateMissingAbsoluteWritePath(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	missing := filepath.Join(t.TempDir(), "external", "cache")

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{missing},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if pathExists(missing) {
		t.Fatalf("missing absolute write path should not be created: %s", missing)
	}
	if slices.Contains(plan.Args, missing) {
		t.Fatalf("missing absolute write path should not be bound: %#v", plan.Args)
	}
}

func TestBuildPlanCreatesParentDirsOnlyUnderTmpfsMounts(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{
		{path: filepath.Join(string(os.PathSeparator), "home", "runner", "work", "repo"), want: true},
		{path: "/root/project", want: true},
		{path: "/tmp/project", want: true},
		{path: "/opt/project", want: false},
	} {
		if got := sandboxDirRequired(test.path); got != test.want {
			t.Fatalf("sandboxDirRequired(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func TestPrepareCgroupLimitsReportsUnavailableCgroup(t *testing.T) {
	original := cgroupRootPath
	cgroupRootPath = func() string { return "" }
	t.Cleanup(func() { cgroupRootPath = original })

	cgroup, evidence, err := PrepareCgroupLimits(Evidence{
		Tool:            "ruff",
		CgroupRequested: true,
		MemoryMB:        512,
	})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("PrepareCgroupLimits() error = %v, want ErrBackendUnavailable", err)
	}
	if cgroup != nil {
		t.Fatalf("unexpected cgroup on unavailable filesystem: %#v", cgroup)
	}
	if evidence.Reason == "" || evidence.CgroupEnabled {
		t.Fatalf("cgroup unavailable evidence mismatch: %#v", evidence)
	}
}

func TestPrepareCgroupLimitsCleansDelegatedDirectory(t *testing.T) {
	root := t.TempDir()
	original := cgroupRootPath
	cgroupRootPath = func() string { return root }
	t.Cleanup(func() { cgroupRootPath = original })

	cgroup, evidence, err := PrepareCgroupLimits(Evidence{
		Tool:            "ruff",
		CgroupRequested: true,
		MemoryMB:        512,
		CPUQuotaPercent: 50,
	})
	if err != nil {
		t.Fatalf("PrepareCgroupLimits() error = %v", err)
	}
	if cgroup == nil || !evidence.CgroupEnabled || evidence.CgroupPath == "" {
		t.Fatalf("cgroup evidence mismatch: cgroup=%#v evidence=%#v", cgroup, evidence)
	}
	if _, err := os.Stat(evidence.CgroupPath); err != nil {
		t.Fatalf("cgroup path missing before cleanup: %v", err)
	}
	if err := cgroup.Close(); err != nil {
		t.Fatalf("close cgroup: %v", err)
	}
	if _, err := os.Stat(evidence.CgroupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cgroup path should be removed, stat error = %v", err)
	}
}

func TestCommandContextAppliesTimeout(t *testing.T) {
	ctx, cancel := CommandContext(context.Background(), 1)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("CommandContext() did not set deadline")
	}
	if deadline.Before(time.Now()) {
		t.Fatalf("deadline is in the past: %v", deadline)
	}
}

func TestBuildPlanPreservesNetworkWhenDeclared(t *testing.T) {
	backend := fakeBubblewrap(t)
	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "gemini-check",
		Executable:  "/tools/gemini",
		BackendPath: backend,
		Capabilities: Capabilities{
			SandboxProfile:  "agent-network",
			RequiresNetwork: true,
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if slices.Contains(plan.Args, "--unshare-net") {
		t.Fatalf("network-capable tool should not get --unshare-net: %#v", plan.Args)
	}
	if !plan.Evidence.RequiresNetwork || plan.Evidence.NetworkIsolated {
		t.Fatalf("network capability not recorded: %#v", plan.Evidence)
	}
}

func TestExecuteReturnsPlanEvidenceAndRunsCommandCallback(t *testing.T) {
	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	var gotExecutable string
	var gotArgs []string
	var gotEvidence Evidence

	evidence, err := Execute(
		context.Background(),
		Request{
			Mode:        ModeRequired,
			Tool:        "ruff",
			Executable:  "/tools/ruff",
			Cwd:         repo,
			RepoRoot:    repo,
			Args:        []string{"check", "."},
			BackendPath: backend,
			Capabilities: Capabilities{
				SandboxProfile: "lint-offline",
			},
		},
		func(executable string, args []string, evidence Evidence) error {
			gotExecutable = executable
			gotArgs = append([]string(nil), args...)
			gotEvidence = evidence

			return nil
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if evidence.Mode != gotEvidence.Mode ||
		evidence.BackendPath != gotEvidence.BackendPath ||
		evidence.Tool != gotEvidence.Tool ||
		gotExecutable != backend {
		t.Fatalf(
			"callback did not receive planned execution: executable=%q evidence=%#v got=%#v",
			gotExecutable,
			evidence,
			gotEvidence,
		)
	}
	if sandboxArgIndex(gotArgs, "/tools/ruff") == -1 ||
		sandboxArgIndex(gotArgs, "check") == -1 {
		t.Fatalf("callback args missing tool command: %#v", gotArgs)
	}
}

func TestExecuteSupportsDryPlanWithoutCallback(t *testing.T) {
	evidence, err := Execute(context.Background(), Request{
		Mode:       ModeOff,
		Tool:       "ruff",
		Executable: "/tools/ruff",
		Args:       []string{"check"},
	}, nil)
	if err != nil {
		t.Fatalf("Execute() dry-plan error = %v", err)
	}
	if evidence.Enabled || evidence.Tool != "ruff" ||
		!slices.Equal(evidence.Command, []string{"/tools/ruff", "check"}) {
		t.Fatalf("dry-plan evidence mismatch: %#v", evidence)
	}
}

func sandboxArgIndex(args []string, value string) int {
	for index, arg := range args {
		if arg == value {
			return index
		}
	}

	return -1
}

func fakeBubblewrap(t *testing.T) string {
	t.Helper()

	backend := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(backend, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}

	return backend
}
