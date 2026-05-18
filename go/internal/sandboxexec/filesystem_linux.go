// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

var errLandlockParentFDRange = errors.New("landlock parent fd out of range")

const (
	privateDirMode      = 0o700
	landlockWriteAccess = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
)

func applyFilesystemPolicy(options options) error {
	writePaths, err := prepareWritablePaths(options)
	if err != nil {
		return err
	}

	err = applyLandlockWritePolicy(writePaths)
	if err != nil {
		return fmt.Errorf("apply landlock write policy: %w", err)
	}

	return nil
}

func prepareWritablePaths(options options) ([]string, error) {
	paths := make([]string, 0, len(options.writePaths))
	for _, item := range options.writePaths {
		path, ok := cleanPolicyPath(options.paths.repoRoot, item)
		if !ok {
			continue
		}

		if pathWithin(filepath.Join(options.paths.repoRoot, ".git"), path) {
			continue
		}

		if !filepath.IsAbs(item) {
			err := os.MkdirAll(path, privateDirMode)
			if err != nil {
				return nil, fmt.Errorf("create declared writable path %s: %w", path, err)
			}
		}

		paths = append(paths, path)
	}

	return paths, nil
}

func applyLandlockWritePolicy(writePaths []string) error {
	ruleset, err := createLandlockRuleset()
	if err != nil {
		return err
	}

	defer func() { _ = ruleset.Close() }()

	for _, path := range writePaths {
		err = addLandlockWriteRule(ruleset.Fd(), path)
		if err != nil {
			return err
		}
	}

	err = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
	if err != nil {
		return fmt.Errorf("enable no_new_privs before landlock: %w", err)
	}

	_, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		ruleset.Fd(),
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("restrict process with landlock: %w", errno)
	}

	return nil
}

func createLandlockRuleset() (*os.File, error) {
	attributes := unix.LandlockRulesetAttr{Access_fs: landlockWriteAccess}

	rulesetFD, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attributes)),
		unsafe.Sizeof(attributes),
		0,
	)
	if errno != 0 {
		return nil, fmt.Errorf("create landlock ruleset: %w", errno)
	}

	return os.NewFile(rulesetFD, "landlock-ruleset"), nil
}

func addLandlockWriteRule(rulesetFD uintptr, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open landlock writable path %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	parentFD, err := landlockParentFD(file)
	if err != nil {
		return err
	}

	rule := unix.LandlockPathBeneathAttr{
		Allowed_access: landlockWriteAccess,
		Parent_fd:      parentFD,
	}

	_, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		rulesetFD,
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&rule)),
		0,
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("add landlock writable path %s: %w", path, errno)
	}

	return nil
}

func landlockParentFD(file *os.File) (int32, error) {
	fileDescriptor := file.Fd()
	if fileDescriptor > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %d", errLandlockParentFDRange, fileDescriptor)
	}

	return int32(fileDescriptor), nil
}

func sandboxedCommandSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
