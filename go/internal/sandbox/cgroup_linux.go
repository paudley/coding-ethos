// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
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
		return nil, evidence, err
	}
	cgroup := &Cgroup{path: path}
	if err := applyCgroupLimitFiles(path, evidence); err != nil {
		_ = cgroup.Close()
		evidence.Reason = err.Error()
		return nil, evidence, err
	}
	dir, err := os.Open(path)
	if err != nil {
		_ = cgroup.Close()
		evidence.Reason = "delegated cgroup directory could not be opened"
		return nil, evidence, err
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
	command.SysProcAttr.CgroupFD = int(cgroup.dir.Fd())
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
	if removeErr := os.RemoveAll(cgroup.path); removeErr != nil && err == nil {
		err = removeErr
	}

	return err
}

func applyCgroupLimitFiles(path string, evidence Evidence) error {
	if evidence.MemoryMB > 0 {
		limit := strconv.FormatInt(int64(evidence.MemoryMB)*1024*1024, 10)
		if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte(limit), 0o600); err != nil {
			return fmt.Errorf("cgroup memory limit could not be applied: %w", err)
		}
	}
	if evidence.CPUQuotaPercent > 0 {
		quota := max(1, evidence.CPUQuotaPercent) * 1000
		if err := os.WriteFile(
			filepath.Join(path, "cpu.max"),
			[]byte(fmt.Sprintf("%d 100000", quota)),
			0o600,
		); err != nil {
			return fmt.Errorf("cgroup CPU limit could not be applied: %w", err)
		}
	}

	return nil
}
