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
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
}

func (cgroup *Cgroup) SysProcAttr() *syscall.SysProcAttr {
	return nil
}

func SysProcAttr(cgroup *Cgroup, evidence Evidence) *syscall.SysProcAttr {
	attributes := cgroup.SysProcAttr()
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	}

	attributes.Setpgid = true

	if evidence.Enabled {
		if evidence.RequiresProcesses || !evidence.NamespaceEnforced {
			return attributes
		}

		attributes.Cloneflags |= (syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC)

		if !evidence.RequiresNetwork {
			attributes.Cloneflags |= syscall.CLONE_NEWNET
		}

		attributes.UidMappings = []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getuid(),
			Size:        1,
		}}
		attributes.GidMappings = []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getgid(),
			Size:        1,
		}}
		attributes.GidMappingsEnableSetgroups = false
	}

	return attributes
}

func nativeNamespaceSupported() bool {
	process, err := os.StartProcess(
		"/bin/true",
		[]string{"/bin/true"},
		&os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
			Sys:   nativeNamespaceSysProcAttr(),
		},
	)
	if err != nil {
		return false
	}

	state, err := process.Wait()

	return err == nil && state.Success()
}

func nativeNamespaceSysProcAttr() *syscall.SysProcAttr {
	attributes := &syscall.SysProcAttr{
		Setpgid: true,
		Cloneflags: syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getuid(),
			Size:        1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getgid(),
			Size:        1,
		}},
		GidMappingsEnableSetgroups: false,
	}

	return attributes
}

func (cgroup *Cgroup) AssignProcess(process *os.Process) error {
	if cgroup == nil || cgroup.path == "" || process == nil {
		return nil
	}

	err := os.WriteFile(
		filepath.Join(cgroup.path, "cgroup.procs"),
		[]byte(strconv.Itoa(process.Pid)),
		cgroupFileMode,
	)
	if err != nil {
		return fmt.Errorf("assign process to delegated cgroup: %w", err)
	}

	return nil
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
