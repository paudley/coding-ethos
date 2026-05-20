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
	errWritePathNotAllowed   = errors.New("sandbox write path is not a file or directory")
	errGitTargetAbsolute     = errors.New("git target must be absolute")
	errGitTargetRequired     = errors.New("at least one git target is required")
	errExecutableAbsolute    = errors.New("executable path must be absolute")
	errExecutableInvalid     = errors.New("executable path is not executable")
)

const (
	privateDirMode      = 0o700
	writableFileMode    = 0o600
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
	landlockFileWriteAccess = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
)

func applyFilesystemPolicy(options options) error {
	err := applyGitBindMounts(options)
	if err != nil {
		return err
	}

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

func applyGitBindMounts(options options) error {
	if strings.TrimSpace(options.gitWrapper) == "" && len(options.gitTargets) == 0 {
		return nil
	}

	wrapper, err := validatedGitWrapper(options.gitWrapper)
	if err != nil {
		return err
	}

	targets, err := validatedGitTargets(options.gitTargets)
	if err != nil {
		return err
	}

	err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
	if err != nil {
		return fmt.Errorf("make sandbox mount namespace private: %w", err)
	}

	if strings.TrimSpace(options.realGitPath) != "" ||
		strings.TrimSpace(options.realGitBind) != "" {
		err = bindRealGit(options.realGitPath, options.realGitBind)
		if err != nil {
			return err
		}
	}

	for _, target := range targets {
		err = unix.Mount(wrapper, target, "", unix.MS_BIND, "")
		if err != nil {
			return fmt.Errorf("bind managed git wrapper over %s: %w", target, err)
		}

		err = remountReadOnlyBind(target)
		if err != nil {
			return fmt.Errorf("remount managed git wrapper read-only over %s: %w", target, err)
		}
	}

	return nil
}

func bindRealGit(sourcePath, targetPath string) error {
	source, err := validatedExecutablePath(sourcePath, "real git source")
	if err != nil {
		return err
	}

	target, err := validatedExecutablePath(targetPath, "real git bind target")
	if err != nil {
		return err
	}

	err = unix.Mount(source, target, "", unix.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("bind real git at %s: %w", target, err)
	}

	err = remountReadOnlyBind(target)
	if err != nil {
		return fmt.Errorf("remount real git read-only at %s: %w", target, err)
	}

	return nil
}

func remountReadOnlyBind(target string) error {
	var stat unix.Statfs_t

	err := unix.Statfs(target, &stat)
	if err != nil {
		return fmt.Errorf("stat mount flags: %w", err)
	}

	flags := uintptr(unix.MS_BIND | unix.MS_REMOUNT | unix.MS_RDONLY)
	if stat.Flags&unix.ST_NOSUID != 0 {
		flags |= unix.MS_NOSUID
	}

	if stat.Flags&unix.ST_NODEV != 0 {
		flags |= unix.MS_NODEV
	}

	if stat.Flags&unix.ST_NOEXEC != 0 {
		flags |= unix.MS_NOEXEC
	}

	err = unix.Mount(target, target, "", flags, "")
	if err != nil {
		return fmt.Errorf("remount read-only bind %s: %w", target, err)
	}

	return nil
}

func validatedGitWrapper(path string) (string, error) {
	return validatedExecutablePath(path, "git wrapper")
}

func validatedGitTargets(paths []string) ([]string, error) {
	targets := []string{}
	seen := map[string]struct{}{}

	for _, path := range paths {
		target := filepath.Clean(strings.TrimSpace(path))
		if target == "." {
			continue
		}

		if !filepath.IsAbs(target) {
			return nil, fmt.Errorf("%w: %s", errGitTargetAbsolute, path)
		}

		resolved, err := filepath.EvalSymlinks(target)
		if err == nil {
			target = filepath.Clean(resolved)
		}

		if _, found := seen[target]; found {
			continue
		}

		target, err = validatedExecutablePath(target, "git target")
		if err != nil {
			return nil, err
		}

		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, errGitTargetRequired
	}

	return targets, nil
}

func validatedExecutablePath(path, label string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf(
			"%w: %s must be absolute: %s",
			errExecutableAbsolute,
			label,
			path,
		)
	}

	info, err := os.Stat(cleaned)
	if err != nil {
		return "", fmt.Errorf("stat %s %s: %w", label, cleaned, err)
	}

	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: %s: %s", errExecutableInvalid, label, cleaned)
	}

	return cleaned, nil
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

		err := prepareWritablePath(
			options.paths.repoRoot,
			item,
			path,
			!filepath.IsAbs(item),
		)
		if err != nil {
			return nil, err
		}

		paths = append(paths, path)
	}

	return paths, nil
}

func prepareWritablePath(repoRoot, originalPath, path string, create bool) error {
	err := rejectSymlinkPath(repoRoot, path)
	if err != nil {
		return err
	}

	info, err := writablePathInfo(repoRoot, originalPath, path, create)
	if err != nil {
		return err
	}

	err = rejectSymlinkPath(repoRoot, path)
	if err != nil {
		return err
	}

	if !info.IsDir() && !info.Mode().IsRegular() && path != os.DevNull {
		return fmt.Errorf("%w: %s", errWritePathNotAllowed, path)
	}

	return nil
}

func writablePathInfo(
	repoRoot,
	originalPath,
	path string,
	create bool,
) (os.FileInfo, error) {
	info, statErr := os.Stat(path)
	if errors.Is(statErr, unix.ENOTDIR) {
		return nil, fmt.Errorf("%w: %s", errWritePathNotAllowed, path)
	}

	if statErr != nil && (!create || !errors.Is(statErr, os.ErrNotExist)) {
		return nil, fmt.Errorf("stat sandbox writable path %s: %w", path, statErr)
	}

	if statErr == nil {
		return info, nil
	}

	if missingWritePathLooksLikeFile(originalPath, path) {
		return writableFilePathInfo(repoRoot, path)
	}

	err := mkdirAllNoSymlinks(repoRoot, path)
	if err != nil {
		return nil, err
	}

	info, statErr = os.Stat(path)
	if statErr != nil {
		return nil, fmt.Errorf("stat sandbox writable path %s: %w", path, statErr)
	}

	return info, nil
}

func missingWritePathLooksLikeFile(originalPath, path string) bool {
	if strings.HasSuffix(originalPath, string(os.PathSeparator)) ||
		strings.HasSuffix(originalPath, "/") {
		return false
	}

	base := filepath.Base(path)
	extension := filepath.Ext(base)

	return extension != "" && extension != base
}

func writableFilePathInfo(repoRoot, path string) (os.FileInfo, error) {
	parent := filepath.Dir(path)

	err := mkdirAllNoSymlinks(repoRoot, parent)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, writableFileMode)
	if err != nil {
		return nil, fmt.Errorf("create sandbox writable file %s: %w", path, err)
	}

	err = file.Close()
	if err != nil {
		return nil, fmt.Errorf("close sandbox writable file %s: %w", path, err)
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		return nil, fmt.Errorf("stat sandbox writable path %s: %w", path, statErr)
	}

	return info, nil
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
			return fmt.Errorf("%w: %s", errWritePathNotAllowed, path)
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
		return fmt.Errorf("%w: %s", errWritePathNotAllowed, path)
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

		if errors.Is(statErr, unix.ENOTDIR) {
			return fmt.Errorf("%w: %s", errWritePathNotAllowed, current)
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

	if !info.IsDir() && !info.Mode().IsRegular() && path != os.DevNull {
		return fmt.Errorf("%w: %s", errWritePathNotAllowed, path)
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
		Allowed_access: landlockAllowedWriteAccess(info),
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

func landlockAllowedWriteAccess(info os.FileInfo) uint64 {
	if info.IsDir() {
		return landlockWriteAccess
	}

	return landlockFileWriteAccess
}

func landlockParentFD(file *os.File) (int32, error) {
	fileDescriptor := file.Fd()
	if fileDescriptor > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %d", errLandlockParentFDRange, fileDescriptor)
	}

	return int32(fileDescriptor), nil
}
