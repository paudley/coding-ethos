// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//go:build !linux

package sandbox

import "os/exec"

type Cgroup struct{}

func PrepareCgroupLimits(evidence Evidence) (*Cgroup, Evidence, error) {
	if !evidence.CgroupRequested {
		return nil, evidence, nil
	}
	evidence.Reason = "cgroup v2 filesystem is unavailable on this platform"

	return nil, evidence, ErrBackendUnavailable
}

func (cgroup *Cgroup) ConfigureCommand(command *exec.Cmd) {}

func (cgroup *Cgroup) Close() error {
	return nil
}
