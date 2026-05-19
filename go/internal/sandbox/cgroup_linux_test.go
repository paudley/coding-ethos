// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandbox

import (
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
