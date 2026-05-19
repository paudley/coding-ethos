// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux && !windows

package sandbox

import (
	"os"
	"os/exec"
	"syscall"
)

type Cgroup struct{}

func PrepareCgroupLimits(evidence Evidence) (*Cgroup, Evidence, error) {
	if !evidence.CgroupRequested {
		return nil, evidence, nil
	}
	evidence.Reason = "cgroup v2 filesystem is unavailable on this platform"

	return nil, evidence, ErrBackendUnavailable
}

func (cgroup *Cgroup) ConfigureCommand(command *exec.Cmd) {}

func (cgroup *Cgroup) SysProcAttr() *syscall.SysProcAttr {
	return nil
}

func (cgroup *Cgroup) AssignProcess(process *os.Process) error {
	return nil
}

func SysProcAttr(cgroup *Cgroup, evidence Evidence) *syscall.SysProcAttr {
	attributes := cgroup.SysProcAttr()
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	}

	attributes.Setpgid = true

	return attributes
}

func nativeNamespaceSupported() bool {
	return false
}

func (cgroup *Cgroup) Close() error {
	return nil
}
