// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package sandbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
	toolRuffPath    = "/tools/ruff"
)

func TestBuildPlanOffReturnsOriginalCommand(t *testing.T) {
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:         sandbox.ModeOff,
		Tool:         "ruff",
		Executable:   toolRuffPath,
		Args:         []string{"check", "pkg"},
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if plan.Executable != toolRuffPath {
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
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:         sandbox.ModeRequired,
		Tool:         "ruff",
		Executable:   toolRuffPath,
		BackendPath:  filepath.Join(t.TempDir(), "missing-bwrap"),
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf(
			"sandbox.BuildPlan() error = %v, want sandbox.ErrBackendUnavailable",
			err,
		)
	}

	if !plan.Evidence.Denied {
		t.Fatalf("denial evidence missing: %#v", plan.Evidence)
	}

	if plan.Evidence.Reason == "" {
		t.Fatalf("denial reason missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanAutoWithoutBackendDenies(t *testing.T) {
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:         sandbox.ModeAuto,
		Tool:         "ruff",
		Executable:   toolRuffPath,
		Args:         []string{"check"},
		BackendPath:  filepath.Join(t.TempDir(), "missing-bwrap"),
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf(
			"sandbox.BuildPlan() error = %v, want sandbox.ErrBackendUnavailable",
			err,
		)
	}

	if !plan.Evidence.Denied {
		t.Fatalf("auto sandbox backend failure must deny execution: %#v", plan.Evidence)
	}

	if plan.Evidence.Reason == "" {
		t.Fatalf("denial reason missing: %#v", plan.Evidence)
	}
}

func TestBuildPlanUsesBubblewrapAndDisablesNetworkByDefault(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	backend := fakeBubblewrap(t)
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustMkdir(t, filepath.Join(repo, "pkg"))

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "ruff",
		Executable:  toolRuffPath,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"check", "pkg"},
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"pkg"},
			WritePaths:     []string{".coding-ethos/cache", ".git/config"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if plan.Executable != backend {
		t.Fatalf("executable = %q", plan.Executable)
	}

	assertSandboxArgs(t, plan.Args, repo)
	assertSandboxEvidence(t, plan, repo)
}

func assertSandboxArgs(t *testing.T, args []string, repo string) {
	t.Helper()

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
		toolRuffPath,
		"check",
		"pkg",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}

	if slices.Contains(args, filepath.Join(repo, ".git/config")) {
		t.Fatalf(".git write bind must be filtered: %#v", args)
	}

	if slices.Contains(args, "--dev-bind") {
		t.Fatalf("full device tree must not be exposed: %#v", args)
	}

	if sandboxArgIndex(args, toolRuffPath) <= sandboxArgIndex(args, "--unshare-net") {
		t.Fatalf("tool executable must be inside bwrap argument boundary: %#v", args)
	}
}

func assertSandboxEvidence(t *testing.T, plan sandbox.Plan, repo string) {
	t.Helper()

	if !plan.Evidence.Enabled || plan.Evidence.Backend != sandbox.BackendBubblewrap {
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

	if slices.Contains(plan.Evidence.WritePaths, ".git/config") {
		t.Fatalf(".git write evidence must be filtered: %#v", plan.Evidence)
	}

	mustStat(t, filepath.Join(repo, ".coding-ethos/cache"))
}

func TestBuildPlanConstrainsChildrenAndStaticBinaries(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	backend := fakeBubblewrap(t)

	executable := filepath.Join(repo, "tools", "static-tool")
	writeExecutable(t, executable, "#!/bin/sh\nexec /bin/sh -c 'true'\n")

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "static-tool",
		Executable:  executable,
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			TimeoutSeconds: 30,
			MemoryMB:       512,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
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

	if sandboxLastArgIndex(
		plan.Args,
		executable,
	) <= sandboxArgIndex(
		plan.Args,
		"--chdir",
	) {
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
	t.Parallel()

	repo := t.TempDir()
	backend := fakeBubblewrap(t)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "ruff",
		Executable:  toolRuffPath,
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"missing"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if slices.Contains(plan.Args, filepath.Join(repo, "missing")) {
		t.Fatalf("missing read bind should not be emitted: %#v", plan.Args)
	}
}

func TestBuildPlanAddsSeccompProfileFD(t *testing.T) {
	t.Parallel()

	profile := filepath.Join(t.TempDir(), "seccomp.bpf")
	writePrivateFile(t, profile, "profile")

	backend := fakeBubblewrap(t)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "ruff",
		Executable:  toolRuffPath,
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile:     "lint-offline",
			SeccompProfile:     "deny-privilege",
			SeccompProfilePath: profile,
		},
	})

	t.Cleanup(func() {
		closeErr := plan.Close()
		if closeErr != nil {
			t.Fatalf("close plan: %v", closeErr)
		}
	})

	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
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

func TestBuildPlanDoesNotCreateMissingAbsoluteWritePath(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	backend := fakeBubblewrap(t)
	missing := filepath.Join(t.TempDir(), "external", "cache")

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "ruff",
		Executable:  toolRuffPath,
		Cwd:         repo,
		RepoRoot:    repo,
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{missing},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	_, statErr := os.Stat(missing)
	if statErr == nil {
		t.Fatalf("missing absolute write path should not be created: %s", missing)
	}

	if slices.Contains(plan.Args, missing) {
		t.Fatalf("missing absolute write path should not be bound: %#v", plan.Args)
	}
}

func TestCommandContextAppliesTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := sandbox.CommandContext(context.Background(), 1)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("sandbox.CommandContext() did not set deadline")
	}

	if deadline.Before(time.Now()) {
		t.Fatalf("deadline is in the past: %v", deadline)
	}
}

func TestBuildPlanPreservesNetworkWhenDeclared(t *testing.T) {
	t.Parallel()

	backend := fakeBubblewrap(t)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
		Tool:        "gemini-check",
		Executable:  "/tools/gemini",
		BackendPath: backend,
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-network",
			RequiresNetwork: true,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if slices.Contains(plan.Args, "--unshare-net") {
		t.Fatalf("network-capable tool should not get --unshare-net: %#v", plan.Args)
	}

	if !plan.Evidence.RequiresNetwork || plan.Evidence.NetworkIsolated {
		t.Fatalf("network capability not recorded: %#v", plan.Evidence)
	}
}

func TestExecuteReturnsPlanEvidenceAndRunsCommandCallback(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	backend := fakeBubblewrap(t)

	var (
		gotExecutable string
		gotArgs       []string
		gotEvidence   sandbox.Evidence
	)

	evidence, err := sandbox.Execute(
		context.Background(),
		sandbox.Request{
			Mode:        sandbox.ModeRequired,
			Tool:        "ruff",
			Executable:  toolRuffPath,
			Cwd:         repo,
			RepoRoot:    repo,
			Args:        []string{"check", "."},
			BackendPath: backend,
			Capabilities: sandbox.Capabilities{
				SandboxProfile: "lint-offline",
			},
		},
		func(executable string, args []string, evidence sandbox.Evidence) error {
			gotExecutable = executable

			gotArgs = append([]string(nil), args...)
			gotEvidence = evidence

			return nil
		},
	)
	if err != nil {
		t.Fatalf("sandbox.Execute() error = %v", err)
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

	if sandboxArgIndex(gotArgs, toolRuffPath) == -1 ||
		sandboxArgIndex(gotArgs, "check") == -1 {
		t.Fatalf("callback args missing tool command: %#v", gotArgs)
	}
}

func TestExecuteSupportsDryPlanWithoutCallback(t *testing.T) {
	t.Parallel()

	evidence, err := sandbox.Execute(context.Background(), sandbox.Request{
		Mode:       sandbox.ModeOff,
		Tool:       "ruff",
		Executable: toolRuffPath,
		Args:       []string{"check"},
	}, nil)
	if err != nil {
		t.Fatalf("sandbox.Execute() dry-plan error = %v", err)
	}

	if evidence.Enabled || evidence.Tool != "ruff" ||
		!slices.Equal(evidence.Command, []string{toolRuffPath, "check"}) {
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

func sandboxLastArgIndex(args []string, value string) int {
	for index := len(args) - 1; index >= 0; index-- {
		if args[index] == value {
			return index
		}
	}

	return -1
}

func fakeBubblewrap(t *testing.T) string {
	t.Helper()

	backend := filepath.Join(t.TempDir(), "bwrap")
	writeExecutable(t, backend, "#!/bin/sh\nexit 0\n")

	return backend
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	err := os.Mkdir(path, privateDirMode)
	if err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	err := os.MkdirAll(path, privateDirMode)
	if err != nil {
		t.Fatalf("create directory tree %s: %v", path, err)
	}
}

func mustStat(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func writePrivateFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), privateFileMode)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	mustMkdirAll(t, filepath.Dir(path))
	writePrivateFile(t, path, content)

	err := os.Chmod(path, privateDirMode)
	if err != nil {
		t.Fatalf("mark %s executable: %v", path, err)
	}
}
