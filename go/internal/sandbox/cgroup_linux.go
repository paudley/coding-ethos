// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	cgroupCPUPeriodMicros = 100_000
	cgroupCPUQuotaFactor  = 1_000
	cgroupFileMode        = 0o600
	cgroupStartProbe      = "/bin/true"
	cgroupStartProbeDelay = 5 * time.Second
	bytesPerMegabyte      = 1024 * 1024
)

type Cgroup struct {
	path string
	fd   int
}

func PrepareCgroupLimits(evidence Evidence) (*Cgroup, Evidence, error) {
	return prepareCgroupLimits(evidence, cgroupRootPath, runCgroupStartCheck)
}

func prepareCgroupLimits(
	evidence Evidence,
	rootPath func() string,
	startCheck func(context.Context, *Cgroup) error,
) (*Cgroup, Evidence, error) {
	if !evidence.CgroupRequested {
		return nil, evidence, nil
	}

	if !cgroupStartAttachmentAllowed(evidence) {
		evidence.Reason = "cgroup limits skipped for namespace-isolated tool"

		return nil, evidence, nil
	}

	root := rootPath()
	if root == "" {
		evidence.Reason = "delegated cgroup v2 filesystem is unavailable"

		return nil, evidence, ErrBackendUnavailable
	}

	path, err := os.MkdirTemp(root, "coding-ethos-"+safeCgroupName(evidence.Tool)+"-")
	if err != nil {
		evidence.Reason = "delegated cgroup directory could not be created"

		return nil, evidence, fmt.Errorf("create delegated cgroup directory: %w", err)
	}

	cgroup := &Cgroup{fd: -1, path: path}

	err = applyCgroupLimitFiles(path, evidence)
	if err != nil {
		_ = cgroup.Close()
		evidence.Reason = err.Error()

		return nil, evidence, err
	}

	descriptor, err := unix.Open(path, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = cgroup.Close()
		evidence.Reason = "delegated cgroup directory could not be opened"

		return nil, evidence, fmt.Errorf("open delegated cgroup directory: %w", err)
	}

	cgroup.fd = descriptor

	ctx, cancel := context.WithTimeout(context.Background(), cgroupStartProbeDelay)
	defer cancel()

	err = startCheck(ctx, cgroup)
	if err != nil {
		_ = cgroup.Close()
		evidence.CgroupEnabled = false
		evidence.CgroupPath = ""
		evidence.Reason = fmt.Sprintf(
			"cgroup limits skipped because delegated start attachment is unavailable: %v",
			err,
		)

		return nil, evidence, nil
	}

	evidence.CgroupEnabled = true
	evidence.CgroupPath = path

	return cgroup, evidence, nil
}

func cgroupStartAttachmentAllowed(evidence Evidence) bool {
	return !evidence.Enabled || evidence.RequiresProcesses || !evidence.NamespaceEnforced
}

func runCgroupStartCheck(ctx context.Context, cgroup *Cgroup) error {
	command := exec.CommandContext(ctx, cgroupStartProbe)
	command.SysProcAttr = cgroup.SysProcAttr()

	err := command.Run()
	if err != nil {
		return fmt.Errorf("run cgroup start probe: %w", err)
	}

	return nil
}

func (cgroup *Cgroup) ConfigureCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
}

func (cgroup *Cgroup) SysProcAttr() *syscall.SysProcAttr {
	if cgroup == nil || cgroup.fd < 0 {
		return nil
	}

	return &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    cgroup.fd,
	}
}

func SysProcAttr(cgroup *Cgroup, evidence Evidence) *syscall.SysProcAttr {
	attributes := cgroup.SysProcAttr()
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	}

	attributes.Setpgid = true

	needsFilesystemNamespace := !evidence.RequiresProcesses ||
		len(evidence.ReadOnlyPaths) > 0
	if evidence.Enabled && needsFilesystemNamespace {
		// Read-only pins need a user and mount namespace even when a tool must
		// retain host process visibility. Ordinary process-enabled tools do not:
		// putting every one in a user namespace changes file-permission and GPG
		// agent behavior. Map the caller to the same numeric identity and grant
		// only the ambient mount capability needed by the native helper; the
		// helper drops it before it execs the requested command.
		attributes.Cloneflags |= syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS
		attributes.UidMappings = []syscall.SysProcIDMap{{
			ContainerID: os.Getuid(),
			HostID:      os.Getuid(),
			Size:        1,
		}}
		attributes.GidMappings = []syscall.SysProcIDMap{{
			ContainerID: os.Getgid(),
			HostID:      os.Getgid(),
			Size:        1,
		}}
		attributes.GidMappingsEnableSetgroups = false
		attributes.AmbientCaps = []uintptr{uintptr(unix.CAP_SYS_ADMIN)}

		if !evidence.RequiresProcesses && evidence.NamespaceEnforced {
			attributes.Cloneflags |= syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC
		}

		if !evidence.RequiresProcesses && !evidence.RequiresNetwork {
			attributes.Cloneflags |= syscall.CLONE_NEWNET
		}
	}

	return attributes
}

func (cgroup *Cgroup) AssignProcess(process *os.Process) error {
	if cgroup == nil || cgroup.path == "" || process == nil {
		return nil
	}

	if cgroup.fd >= 0 {
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
	if cgroup.fd >= 0 {
		err = unix.Close(cgroup.fd)
		cgroup.fd = -1
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
