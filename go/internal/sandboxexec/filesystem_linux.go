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
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	errLandlockParentFDRange = errors.New("landlock parent fd out of range")
	errSymlinkWritePath      = errors.New("sandbox write path contains symlink")
	errWritePathNotDirectory = errors.New("sandbox write path is not a directory")
)

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

		err := prepareWritableDirectory(options.paths.repoRoot, path, !filepath.IsAbs(item))
		if err != nil {
			return nil, err
		}

		paths = append(paths, path)
	}

	return paths, nil
}

func prepareWritableDirectory(repoRoot, path string, create bool) error {
	err := rejectSymlinkPath(repoRoot, path)
	if err != nil {
		return err
	}

	if create {
		err = mkdirAllNoSymlinks(repoRoot, path)
		if err != nil {
			return err
		}
	}

	err = rejectSymlinkPath(repoRoot, path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat sandbox writable path %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s", errWritePathNotDirectory, path)
	}

	return nil
}

func mkdirAllNoSymlinks(repoRoot, path string) error {
	relative, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return fmt.Errorf("resolve sandbox writable path %s: %w", path, err)
	}

	current := repoRoot

	for part := range strings.SplitSeq(relative, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}

		current = filepath.Join(current, part)

		err = mkdirOneNoSymlink(current)
		if err != nil {
			return err
		}
	}

	return nil
}

func mkdirOneNoSymlink(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", errSymlinkWritePath, path)
		}

		if !info.IsDir() {
			return fmt.Errorf("%w: %s", errWritePathNotDirectory, path)
		}

		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect sandbox writable path %s: %w", path, err)
	}

	err = os.Mkdir(path, privateDirMode)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create declared writable path %s: %w", path, err)
	}

	info, err = os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect created sandbox writable path %s: %w", path, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", errSymlinkWritePath, path)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s", errWritePathNotDirectory, path)
	}

	return nil
}

func rejectSymlinkPath(repoRoot, path string) error {
	relative, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return fmt.Errorf("resolve sandbox writable path %s: %w", path, err)
	}

	current := repoRoot

	for part := range strings.SplitSeq(relative, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}

		current = filepath.Join(current, part)

		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}

		if statErr != nil {
			return fmt.Errorf("inspect sandbox writable path %s: %w", current, statErr)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", errSymlinkWritePath, current)
		}
	}

	return nil
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
	info, statErr := os.Stat(path)
	if statErr != nil {
		return fmt.Errorf("stat landlock writable path %s: %w", path, statErr)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s", errWritePathNotDirectory, path)
	}

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
