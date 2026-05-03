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
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("create git dir: %v", err)
	}
	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"check", "pkg"},
		BackendPath: "/usr/bin/bwrap",
		Capabilities: Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"pkg"},
			WritePaths:     []string{".coding-ethos/cache", ".git/config"},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Executable != "/usr/bin/bwrap" {
		t.Fatalf("executable = %q", plan.Executable)
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
	if slices.Contains(plan.Evidence.WritePaths, ".git/config") {
		t.Fatalf(".git write evidence must be filtered: %#v", plan.Evidence)
	}
}

func TestBuildPlanAddsSeccompProfileFD(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "seccomp.bpf")
	if err := os.WriteFile(profile, []byte("profile"), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "ruff",
		Executable:  "/tools/ruff",
		BackendPath: "/usr/bin/bwrap",
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

func TestApplyCgroupLimitsReportsUnavailableCgroup(t *testing.T) {
	original := cgroupRootPath
	cgroupRootPath = func() string { return "" }
	t.Cleanup(func() { cgroupRootPath = original })

	evidence, err := ApplyCgroupLimits(1234, Evidence{
		Tool:            "ruff",
		CgroupRequested: true,
		MemoryMB:        512,
	})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("ApplyCgroupLimits() error = %v, want ErrBackendUnavailable", err)
	}
	if evidence.Reason == "" || evidence.CgroupEnabled {
		t.Fatalf("cgroup unavailable evidence mismatch: %#v", evidence)
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
	plan, err := BuildPlan(Request{
		Mode:        ModeRequired,
		Tool:        "gemini-check",
		Executable:  "/tools/gemini",
		BackendPath: "/usr/bin/bwrap",
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
