// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCgroupSysProcAttrUsesCgroupFD(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	descriptor, err := unix.Open(path, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open cgroup fixture descriptor: %v", err)
	}
	defer unix.Close(descriptor)

	cgroup := &Cgroup{fd: descriptor, path: path}
	attributes := cgroup.SysProcAttr()

	if attributes == nil {
		t.Fatal("SysProcAttr() = nil")
	}
	if !attributes.UseCgroupFD {
		t.Fatalf("UseCgroupFD = false")
	}
	if attributes.CgroupFD != descriptor {
		t.Fatalf("CgroupFD = %d, want %d", attributes.CgroupFD, descriptor)
	}
}

func TestProcessEnabledSandboxWithReadOnlyPinsRetainsFilesystemNamespaces(
	t *testing.T,
) {
	t.Parallel()

	attributes := SysProcAttr(nil, Evidence{
		Enabled:           true,
		RequiresProcesses: true,
		RequiresNetwork:   true,
		ReadOnlyPaths:     []string{"/workspace/.git"},
	})
	if attributes == nil {
		t.Fatal("SysProcAttr() = nil")
	}

	for _, namespace := range []uintptr{
		syscall.CLONE_NEWUSER,
		syscall.CLONE_NEWNS,
	} {
		if attributes.Cloneflags&namespace == 0 {
			t.Fatalf(
				"filesystem namespace flag %#x missing from %#x",
				namespace,
				attributes.Cloneflags,
			)
		}
	}
	for _, preserved := range []uintptr{
		syscall.CLONE_NEWNET,
		syscall.CLONE_NEWUTS,
		syscall.CLONE_NEWIPC,
	} {
		if attributes.Cloneflags&preserved != 0 {
			t.Fatalf("process-enabled sandbox unexpectedly isolated namespace %#x", preserved)
		}
	}
	if len(attributes.UidMappings) != 1 || len(attributes.GidMappings) != 1 {
		t.Fatalf("user namespace mappings missing: %#v", attributes)
	}
	if attributes.UidMappings[0].ContainerID != os.Getuid() ||
		attributes.GidMappings[0].ContainerID != os.Getgid() {
		t.Fatalf("namespace changed the caller's numeric identity: %#v", attributes)
	}
	if len(attributes.AmbientCaps) != 1 ||
		attributes.AmbientCaps[0] != uintptr(unix.CAP_SYS_ADMIN) {
		t.Fatalf("mount helper capability missing or overbroad: %#v", attributes.AmbientCaps)
	}
}

func TestReusedActiveAgentShellDoesNotCreateNestedNamespaces(t *testing.T) {
	t.Parallel()

	attributes := SysProcAttr(nil, Evidence{
		Enabled:           true,
		NamespaceEnforced: true,
		Reason:            activeAgentShellReuseReason,
	})
	if attributes == nil {
		t.Fatal("SysProcAttr() = nil")
	}
	if !attributes.Setpgid {
		t.Fatal("reused active sandbox lost process-group isolation")
	}
	if attributes.Cloneflags != 0 {
		t.Fatalf(
			"reused active sandbox created nested namespaces: %#x",
			attributes.Cloneflags,
		)
	}
	if len(attributes.UidMappings) != 0 || len(attributes.GidMappings) != 0 {
		t.Fatalf("reused active sandbox changed identity: %#v", attributes)
	}
	if len(attributes.AmbientCaps) != 0 {
		t.Fatalf("reused active sandbox gained capabilities: %#v", attributes.AmbientCaps)
	}
}

func TestProcessEnabledSandboxWithoutReadOnlyPinsPreservesHostNamespaces(t *testing.T) {
	t.Parallel()

	attributes := SysProcAttr(nil, Evidence{
		Enabled:           true,
		RequiresProcesses: true,
		RequiresNetwork:   true,
	})
	if attributes == nil {
		t.Fatal("SysProcAttr() = nil")
	}
	if attributes.Cloneflags != 0 {
		t.Fatalf(
			"ordinary process-enabled sandbox unexpectedly isolated namespaces: %#x",
			attributes.Cloneflags,
		)
	}
	if len(attributes.UidMappings) != 0 || len(attributes.GidMappings) != 0 {
		t.Fatalf(
			"ordinary process-enabled sandbox unexpectedly changed identity: %#v",
			attributes,
		)
	}
	if len(attributes.AmbientCaps) != 0 {
		t.Fatalf(
			"ordinary process-enabled sandbox unexpectedly gained capabilities: %#v",
			attributes.AmbientCaps,
		)
	}
}

func TestCgroupStartAttachmentSkipsNamespaceIsolatedTools(t *testing.T) {
	t.Parallel()

	if cgroupStartAttachmentAllowed(Evidence{
		Enabled:           true,
		NamespaceEnforced: true,
	}) {
		t.Fatal("namespace-isolated tool requested start-time cgroup attach")
	}

	if !cgroupStartAttachmentAllowed(Evidence{
		Enabled:           true,
		NamespaceEnforced: true,
		RequiresProcesses: true,
	}) {
		t.Fatal("process-enabled tool should keep start-time cgroup attach")
	}
}

func TestPrepareCgroupLimitsSkipsUnavailableStartAttachment(t *testing.T) {
	root := t.TempDir()
	err := os.WriteFile(
		filepath.Join(root, "cgroup.controllers"),
		[]byte("cpu memory"),
		cgroupFileMode,
	)
	if err != nil {
		t.Fatalf("write cgroup.controllers fixture: %v", err)
	}

	rootPath := func() string {
		return root
	}
	startCheck := func(_ context.Context, _ *Cgroup) error {
		return errors.New("permission denied")
	}

	cgroup, evidence, err := prepareCgroupLimits(Evidence{
		Enabled:           true,
		CgroupRequested:   true,
		Tool:              "go-test",
		RequiresProcesses: true,
	}, rootPath, startCheck)
	if err != nil {
		t.Fatalf("PrepareCgroupLimits returned error: %v", err)
	}

	if cgroup != nil {
		t.Fatal("PrepareCgroupLimits returned cgroup for unavailable start attachment")
	}
	if evidence.CgroupEnabled {
		t.Fatalf("CgroupEnabled = true: %#v", evidence)
	}
	if evidence.CgroupPath != "" {
		t.Fatalf("CgroupPath = %q, want empty", evidence.CgroupPath)
	}
	if !strings.Contains(evidence.Reason, "delegated start attachment is unavailable") {
		t.Fatalf("Reason = %q", evidence.Reason)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read cgroup root fixture: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("temporary cgroup directory was not removed: %s", entry.Name())
		}
	}
}
