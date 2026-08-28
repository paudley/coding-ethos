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
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	errLandlockParentFDRange = errors.New("landlock parent fd out of range")
	errRealGitBindPair       = errors.New(
		"real git bind requires both --real-git-path and --real-git-bind",
	)
	errSymlinkWritePath    = errors.New("sandbox write path contains symlink")
	errWritePathNotAllowed = errors.New(
		"sandbox write path is not a file or directory",
	)
	errReadOnlyPathNotAbsolute = errors.New("sandbox read-only path must be absolute")
	errGitDirWouldBeWritable   = errors.New(
		"sandbox write path would make .git writable without pinning it read-only",
	)
	errGitTargetAbsolute  = errors.New("git target must be absolute")
	errGitTargetRequired  = errors.New("at least one git target is required")
	errExecutableAbsolute = errors.New("executable path must be absolute")
	errExecutableInvalid  = errors.New("executable path is not executable")
)

const (
	privateDirMode      = 0o700
	writableFileMode    = 0o600
	mountInfoFieldCount = 6
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

	// Validate and materialize declared paths while the original filesystem is
	// still visible. No untrusted command has started yet. Some of these paths
	// are intentional writable children of a protected parent, so creating
	// them after that parent is pinned read-only would be both too late and
	// impossible.
	writePaths, err := prepareWritablePaths(options)
	if err != nil {
		return err
	}

	// Before anything is made writable, and never after. Landlock grants
	// access to a whole subtree and has no way to take part of it back, so a
	// directory that must stay read-only inside a writable parent has to be
	// held by a mount rather than by a rule. Failing here is fatal on purpose:
	// the caller only asks for a writable parent because it expects these to
	// hold, and continuing would hand out the parent without them.
	err = applyReadOnlyPaths(options.readOnlyPaths)
	if err != nil {
		return err
	}

	err = applyWritablePathOverrides(options.readOnlyPaths, writePaths)
	if err != nil {
		return err
	}

	// Must run while CAP_SYS_ADMIN is still held: the rebuild is a mount.
	err = applySystemSSHConfig(options)
	if err != nil {
		return err
	}

	// The launcher grants only CAP_SYS_ADMIN so this helper can establish its
	// private mounts without changing the caller's numeric identity. No
	// capability belongs in the requested command, including a shell or Git
	// hook, so discard both ambient and effective sets before Landlock and exec.
	err = dropSandboxCapabilities()
	if err != nil {
		return err
	}

	err = applyLandlockWritePolicy(writePaths)
	if err != nil {
		return fmt.Errorf("apply landlock write policy: %w", err)
	}

	return nil
}

func dropSandboxCapabilities() error {
	err := unix.Prctl(
		unix.PR_CAP_AMBIENT,
		unix.PR_CAP_AMBIENT_CLEAR_ALL,
		0,
		0,
		0,
	)
	if err != nil {
		return fmt.Errorf("clear ambient sandbox capabilities: %w", err)
	}

	header := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0,
	}
	data := [2]unix.CapUserData{}

	err = unix.Capset(&header, &data[0])
	if err != nil {
		return fmt.Errorf("clear effective sandbox capabilities: %w", err)
	}

	return nil
}

// applyWritablePathOverrides restores only validated writable children below
// a read-only pin. `.coding-ethos` is protected as a parent so the worktree
// root grant cannot create arbitrary state there, while its cache, state, and
// lint-run directories remain intentional runtime capabilities. A distinct
// self-bind is required: Landlock can grant the child, but it cannot make a
// read-only VFS mount writable.
func applyWritablePathOverrides(readOnlyPaths, writePaths []string) error {
	overrides := writablePathOverrides(readOnlyPaths, writePaths)
	if len(overrides) == 0 {
		return nil
	}

	err := isolateMountPropagation()
	if err != nil {
		return err
	}

	for _, path := range overrides {
		err = unix.Mount(path, path, "", unix.MS_BIND|unix.MS_REC, "")
		if err != nil {
			return fmt.Errorf("bind writable override %s: %w", path, err)
		}

		err = remountWritableBind(path)
		if err != nil {
			return fmt.Errorf("remount writable override %s: %w", path, err)
		}
	}

	return nil
}

func writablePathOverrides(readOnlyPaths, writePaths []string) []string {
	overrides := []string{}

	for _, writePath := range writePaths {
		for _, readOnlyPath := range readOnlyPaths {
			pin := filepath.Clean(strings.TrimSpace(readOnlyPath))
			if pin != "." && writePath != pin && pathWithin(pin, writePath) {
				overrides = append(overrides, writePath)

				break
			}
		}
	}

	return overrides
}

// applyReadOnlyPaths pins each path read-only with a bind mount over itself.
//
// This exists because Landlock is allow-only and hierarchical: a rule can open
// a subtree but never close part of one. Granting the repository root -- which
// is what lets git create, delete and rename files at the top level -- would
// otherwise grant everything beneath it too, including .git, and an agent that
// can write .git/config or drop in a hook is outside the git wrapper entirely.
//
// A mount does what a rule cannot, and it is applied first so no window exists
// where the parent is writable and these are not.
func applyReadOnlyPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	mountInfo, mountInfoErr := os.ReadFile("/proc/self/mountinfo")
	mountPropagationIsolated := false

	for _, path := range paths {
		cleaned, needsMount, err := readOnlyPathNeedsMount(
			path,
			string(mountInfo),
			mountInfoErr == nil,
		)
		if err != nil {
			return err
		}

		if !needsMount {
			continue
		}

		if !mountPropagationIsolated {
			err = isolateMountPropagation()
			if err != nil {
				return err
			}

			mountPropagationIsolated = true
		}

		err = bindReadOnlyPath(cleaned)
		if err != nil {
			return err
		}
	}

	return nil
}

func readOnlyPathNeedsMount(
	path, mountInfo string,
	mountInfoAvailable bool,
) (string, bool, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || !filepath.IsAbs(cleaned) {
		return "", false, fmt.Errorf("%w: %s", errReadOnlyPathNotAbsolute, path)
	}

	// A path that is not there cannot be written either, and repositories
	// legitimately differ: not every one carries a .coding-ethos.
	_, err := os.Lstat(cleaned)
	if errors.Is(err, os.ErrNotExist) {
		return cleaned, false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("stat read-only path %s: %w", cleaned, err)
	}

	// A parent supervisor may already have established the exact pin before
	// dropping mount capabilities. That is the normal shape when this helper
	// runs inside Nyar's outer Bubblewrap sandbox. Accept only an exact
	// mountpoint with an explicit `ro` option, never ordinary permissions.
	if mountInfoAvailable && readOnlyMountInfoForPath(mountInfo, cleaned) {
		return cleaned, false, nil
	}

	return cleaned, true, nil
}

func bindReadOnlyPath(path string) error {
	err := unix.Mount(path, path, "", unix.MS_BIND|unix.MS_REC, "")
	if err != nil {
		return fmt.Errorf("bind read-only path %s: %w", path, err)
	}

	err = remountReadOnlyBind(path)
	if err != nil {
		return fmt.Errorf("remount read-only path %s: %w", path, err)
	}

	return nil
}

// readOnlyMountInfoForPath reports whether path is an exact read-only
// mountpoint in Linux mountinfo. Exactness matters: this is the evidence that
// a writable parent has a VFS boundary over the protected child, rather than
// an inference from permissions that the process may be able to change.
func readOnlyMountInfoForPath(content, path string) bool {
	cleaned := filepath.Clean(path)

	for line := range strings.Lines(content) {
		fields := strings.Fields(line)
		if len(fields) <= mountInfoFieldCount {
			continue
		}

		mountPoint := strings.NewReplacer(
			`\040`, " ",
			`\011`, "\t",
			`\012`, "\n",
			`\134`, `\`,
		).Replace(fields[4])
		if filepath.Clean(mountPoint) != cleaned {
			continue
		}

		return slices.Contains(strings.Split(fields[5], ","), "ro")
	}

	return false
}

func applyGitBindMounts(options options) error {
	if strings.TrimSpace(options.gitWrapper) == "" && len(options.gitTargets) == 0 {
		return nil
	}

	realGitPath, realGitBind, err := validatedRealGitBindPair(options)
	if err != nil {
		return err
	}

	wrapper, err := validatedGitWrapper(options.gitWrapper)
	if err != nil {
		return err
	}

	targets, err := validatedGitTargets(options.gitTargets)
	if err != nil {
		return err
	}

	err = isolateMountPropagation()
	if err != nil {
		return err
	}

	if realGitPath != "" && realGitBind != "" {
		err = bindRealGit(options.realGitPath, options.realGitBind)
		if err != nil {
			return err
		}
	}

	err = bindGitWrapperTargets(wrapper, targets)
	if err != nil {
		return err
	}

	return nil
}

func bindGitWrapperTargets(wrapper string, targets []string) error {
	for _, target := range targets {
		err := unix.Mount(wrapper, target, "", unix.MS_BIND, "")
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

func validatedRealGitBindPair(options options) (string, string, error) {
	realGitPath := strings.TrimSpace(options.realGitPath)
	realGitBind := strings.TrimSpace(options.realGitBind)

	if (realGitPath == "") != (realGitBind == "") {
		return "", "", errRealGitBindPair
	}

	return realGitPath, realGitBind, nil
}

func isolateMountPropagation() error {
	err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "")
	if err == nil {
		return nil
	}

	if !isMountPropagationPermissionError(err) {
		return fmt.Errorf("make sandbox mount namespace private: %w", err)
	}

	mountInfo, readErr := os.ReadFile("/proc/self/mountinfo")
	if readErr != nil {
		return fmt.Errorf(
			"make sandbox mount namespace private: %w; inspect mount propagation: %w",
			err,
			readErr,
		)
	}

	// Some rootless CI namespaces deny the propagation-change syscall while
	// already exposing only non-shared mounts. Preserve the invariant instead
	// of requiring one specific kernel operation to prove it.
	if mountInfoHasSharedPropagation(string(mountInfo)) {
		return fmt.Errorf(
			"make sandbox mount namespace private: %w; shared mount propagation remains",
			err,
		)
	}

	return nil
}

func isMountPropagationPermissionError(err error) bool {
	return errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)
}

func mountInfoHasSharedPropagation(content string) bool {
	for line := range strings.Lines(content) {
		fields := strings.Fields(line)
		if len(fields) <= mountInfoFieldCount {
			continue
		}

		for _, field := range fields[mountInfoFieldCount:] {
			if field == "-" {
				break
			}

			if strings.HasPrefix(field, "shared:") {
				return true
			}
		}
	}

	return false
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

func remountWritableBind(target string) error {
	var stat unix.Statfs_t

	err := unix.Statfs(target, &stat)
	if err != nil {
		return fmt.Errorf("stat mount flags: %w", err)
	}

	flags := uintptr(unix.MS_BIND | unix.MS_REMOUNT)
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
		return fmt.Errorf("remount writable bind %s: %w", target, err)
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
	gitDir := filepath.Join(options.paths.repoRoot, ".git")
	paths := make([]string, 0, len(options.writePaths))
	gitDirExplicitlyWritable := explicitlyWritablePolicyPath(options, gitDir)

	for _, item := range options.writePaths {
		path, include, err := preparedPolicyWritePath(
			options,
			item,
			gitDir,
			gitDirExplicitlyWritable,
		)
		if err != nil {
			return nil, err
		}

		if !include {
			continue
		}

		paths = append(paths, path)
	}

	return paths, nil
}

func explicitlyWritablePolicyPath(options options, expected string) bool {
	if !options.allowGitWrites {
		return false
	}

	for _, item := range options.writePaths {
		path, ok := cleanPolicyPath(
			options.paths.repoRoot,
			item,
			options.allowGitWrites,
		)
		if ok && path == expected {
			return true
		}
	}

	return false
}

func preparedPolicyWritePath(
	options options,
	item, gitDir string,
	gitDirExplicitlyWritable bool,
) (string, bool, error) {
	path, ok := cleanPolicyPath(options.paths.repoRoot, item, options.allowGitWrites)
	if !ok || (!options.allowGitWrites && pathWithin(gitDir, path)) {
		return "", false, nil
	}

	err := validateIncidentalGitWrite(
		options.readOnlyPaths,
		path,
		gitDir,
		gitDirExplicitlyWritable,
	)
	if err != nil {
		return "", false, err
	}

	err = prepareWritablePath(
		options.paths.repoRoot,
		item,
		path,
		!filepath.IsAbs(item),
	)
	if err != nil {
		return "", false, err
	}

	return path, true, nil
}

// validateIncidentalGitWrite makes the .git exclusion an invariant rather
// than a caller convention. A Landlock grant reaches everything beneath a
// directory, so a writable parent must be accompanied either by the exact,
// wrapper-gated Git capability of a primary checkout or by a read-only mount
// over a linked worktree's .git pointer.
func validateIncidentalGitWrite(
	readOnlyPaths []string,
	path, gitDir string,
	gitDirExplicitlyWritable bool,
) error {
	incidental := path != gitDir && pathWithin(path, gitDir)
	if incidental &&
		!gitDirExplicitlyWritable &&
		!pinnedReadOnly(readOnlyPaths, gitDir) {
		return fmt.Errorf("%w: %s", errGitDirWouldBeWritable, path)
	}

	return nil
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

	if !info.IsDir() &&
		!info.Mode().IsRegular() &&
		path != os.DevNull &&
		!allowedTerminalWritePath(path) {
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

	if !info.IsDir() &&
		!info.Mode().IsRegular() &&
		path != os.DevNull &&
		!allowedTerminalWritePath(path) {
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

// pinnedReadOnly reports whether target is held read-only by one of the pins.
//
// A pin on an ancestor covers what is beneath it, because the mount is
// recursive, so an exact match is not required.
func pinnedReadOnly(readOnlyPaths []string, target string) bool {
	for _, path := range readOnlyPaths {
		cleaned := filepath.Clean(strings.TrimSpace(path))
		if cleaned == "" {
			continue
		}

		if pathWithin(cleaned, target) {
			return true
		}
	}

	return false
}
