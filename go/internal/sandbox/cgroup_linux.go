// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	cgroupCPUPeriodMicros = 100_000
	cgroupCPUQuotaFactor  = 1_000
	cgroupFileMode        = 0o600
	bytesPerMegabyte      = 1024 * 1024
)

type Cgroup struct {
	dir  *os.File
	path string
}

func PrepareCgroupLimits(evidence Evidence) (*Cgroup, Evidence, error) {
	if !evidence.CgroupRequested {
		return nil, evidence, nil
	}

	root := cgroupRootPath()
	if root == "" {
		evidence.Reason = "delegated cgroup v2 filesystem is unavailable"

		return nil, evidence, ErrBackendUnavailable
	}

	path, err := os.MkdirTemp(root, "coding-ethos-"+safeCgroupName(evidence.Tool)+"-")
	if err != nil {
		evidence.Reason = "delegated cgroup directory could not be created"

		return nil, evidence, fmt.Errorf("create delegated cgroup directory: %w", err)
	}

	cgroup := &Cgroup{path: path}

	err = applyCgroupLimitFiles(path, evidence)
	if err != nil {
		_ = cgroup.Close()
		evidence.Reason = err.Error()

		return nil, evidence, err
	}

	dir, err := os.Open(path)
	if err != nil {
		_ = cgroup.Close()
		evidence.Reason = "delegated cgroup directory could not be opened"

		return nil, evidence, fmt.Errorf("open delegated cgroup directory: %w", err)
	}

	cgroup.dir = dir
	evidence.CgroupEnabled = true
	evidence.CgroupPath = path

	return cgroup, evidence, nil
}

func (cgroup *Cgroup) ConfigureCommand(command *exec.Cmd) {
	if cgroup == nil || cgroup.dir == nil {
		return
	}

	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}

	command.SysProcAttr.UseCgroupFD = true
	command.SysProcAttr.CgroupFD = cgroupFileDescriptor(cgroup.dir)
}

func (cgroup *Cgroup) SysProcAttr() *syscall.SysProcAttr {
	if cgroup == nil || cgroup.dir == nil {
		return nil
	}

	return &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    cgroupFileDescriptor(cgroup.dir),
	}
}

func (cgroup *Cgroup) Close() error {
	if cgroup == nil {
		return nil
	}

	var err error
	if cgroup.dir != nil {
		err = cgroup.dir.Close()
		cgroup.dir = nil
	}

	removeErr := os.RemoveAll(cgroup.path)
	if removeErr != nil && err == nil {
		err = removeErr
	}

	return err
}

func applyCgroupLimitFiles(path string, evidence Evidence) error {
	if evidence.MemoryMB > 0 {
		limit := strconv.FormatInt(int64(evidence.MemoryMB)*bytesPerMegabyte, 10)

		err := os.WriteFile(
			filepath.Join(path, "memory.max"),
			[]byte(limit),
			cgroupFileMode,
		)
		if err != nil {
			return fmt.Errorf("cgroup memory limit could not be applied: %w", err)
		}
	}

	if evidence.CPUQuotaPercent > 0 {
		quota := max(1, evidence.CPUQuotaPercent) * cgroupCPUQuotaFactor

		err := os.WriteFile(
			filepath.Join(path, "cpu.max"),
			fmt.Appendf(nil, "%d %d", quota, cgroupCPUPeriodMicros),
			cgroupFileMode,
		)
		if err != nil {
			return fmt.Errorf("cgroup CPU limit could not be applied: %w", err)
		}
	}

	return nil
}

func cgroupFileDescriptor(file *os.File) int {
	descriptor, err := strconv.Atoi(strconv.FormatUint(uint64(file.Fd()), 10))
	if err != nil {
		return -1
	}

	return descriptor
}
