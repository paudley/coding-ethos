// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package sandbox

import (
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

func SysProcAttr(cgroup *Cgroup, evidence Evidence) *syscall.SysProcAttr {
	return cgroup.SysProcAttr()
}

func nativeNamespaceSupported() bool {
	return false
}

func (cgroup *Cgroup) Close() error {
	return nil
}
