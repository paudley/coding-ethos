// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseOptionsNormalizesPathsAndPreservesCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parsed, err := parseOptions([]string{
		"--cwd", root,
		"--repo-root", root,
		"--git-wrapper", "/tmp/git-wrapper",
		"--real-git-path", "/usr/bin/git",
		"--real-git-bind", "/tmp/real-git",
		"--git-target", "/usr/bin/git",
		"--write-path", ".coding-ethos/cache",
		"--requires-network",
		"--",
		"/bin/true",
		"--flag",
	})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if parsed.paths.cwd != root || parsed.paths.repoRoot != root {
		t.Fatalf("paths = %#v", parsed.paths)
	}
	if !slices.Equal(parsed.writePaths, []string{".coding-ethos/cache"}) {
		t.Fatalf("write paths = %#v", parsed.writePaths)
	}
	if parsed.gitWrapper != "/tmp/git-wrapper" ||
		parsed.realGitPath != "/usr/bin/git" ||
		parsed.realGitBind != "/tmp/real-git" ||
		!slices.Equal(parsed.gitTargets, []string{"/usr/bin/git"}) {
		t.Fatalf("git bind options = %#v", parsed)
	}
	if !slices.Equal(parsed.commandArgv, []string{"/bin/true", "--flag"}) {
		t.Fatalf("command argv = %#v", parsed.commandArgv)
	}
	if !parsed.requiresNetwork {
		t.Fatalf("requires-network flag not parsed: %#v", parsed)
	}
}

func TestParseOptionsRequiresCommand(t *testing.T) {
	t.Parallel()

	_, err := parseOptions([]string{"--cwd", t.TempDir()})
	if !errors.Is(err, errSandboxExecCommand) {
		t.Fatalf("parseOptions() error = %v, want command error", err)
	}
}

func TestRunReturnsFailureForInvalidInvocation(t *testing.T) {
	t.Parallel()

	if code := Run([]string{"--cwd", t.TempDir()}); code != sandboxExecFailureExitCode {
		t.Fatalf("Run() exit = %d, want %d", code, sandboxExecFailureExitCode)
	}
}

func TestExecSandboxedCommandReportsStartFailure(t *testing.T) {
	t.Parallel()

	err := execSandboxedCommand(options{
		paths:       &sandboxPaths{cwd: t.TempDir()},
		commandArgv: []string{filepath.Join(t.TempDir(), "missing-tool")},
	})
	if err == nil {
		t.Fatal("execSandboxedCommand() error = nil")
	}
}

func TestSandboxExecEnvRemovesGitOverrides(t *testing.T) {
	t.Parallel()

	got := sandboxExecEnv([]string{
		"PATH=/bin",
		"CODING_ETHOS_EXEC_STACK=coding-ethos-sandbox",
		"GIT_DIR=/tmp/git",
		"GIT_CONFIG_KEY_0=core.sshCommand",
		"GIT_CONFIG_VALUE_0=ssh -i key",
		"KEEP=value",
	})
	joined := strings.Join(got, "\n")

	for _, blocked := range []string{
		"CODING_ETHOS_EXEC_STACK=",
		"GIT_DIR=",
		"GIT_CONFIG_KEY_0",
		"GIT_CONFIG_VALUE_0",
	} {
		if strings.Contains(joined, blocked) {
			t.Fatalf("environment retained blocked %q: %#v", blocked, got)
		}
	}
	for _, kept := range []string{"PATH=/bin", "KEEP=value"} {
		if !slices.Contains(got, kept) {
			t.Fatalf("environment missing %q: %#v", kept, got)
		}
	}
}

func TestApplyGitBindMountsRequiresRealGitPair(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wrapper := filepath.Join(root, "wrapper-git")
	target := filepath.Join(root, "target-git")
	writeTestFile(t, wrapper, 0o700)
	writeTestFile(t, target, 0o700)

	err := applyGitBindMounts(options{
		gitWrapper:  wrapper,
		gitTargets:  []string{target},
		realGitPath: "/usr/bin/git",
	})
	if err == nil ||
		!strings.Contains(err.Error(), "--real-git-path and --real-git-bind") {
		t.Fatalf("applyGitBindMounts() error = %v, want paired real git flags", err)
	}
}

func TestCleanPolicyPathStaysInsideRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path, ok := cleanPolicyPath(root, "pkg/../.coding-ethos/cache", false)
	if !ok || path != filepath.Join(root, ".coding-ethos", "cache") {
		t.Fatalf("cleanPolicyPath relative = %q %t", path, ok)
	}

	outside, ok := cleanPolicyPath(root, filepath.Dir(root), false)
	if ok || outside == "" {
		t.Fatalf("cleanPolicyPath outside = %q %t", outside, ok)
	}

	nullDevice, ok := cleanPolicyPath(root, os.DevNull, false)
	if !ok || nullDevice != os.DevNull {
		t.Fatalf("cleanPolicyPath dev null = %q %t", nullDevice, ok)
	}
}

func TestSharedLockDirectoryMetadataAllowsOnlyTheValidatedCapabilityShape(
	t *testing.T,
) {
	t.Parallel()

	shared := t.TempDir()
	if err := os.Chmod(shared, os.ModeSticky|0o777); err != nil {
		t.Fatalf("set shared lock directory mode: %v", err)
	}
	info, err := os.Lstat(shared)
	if err != nil {
		t.Fatalf("inspect shared lock metadata: %v", err)
	}
	if !validSharedLockDirectoryMetadata("/var/tmp/coding-ethos-sandbox-lock-test", info) {
		t.Fatal("the exact direct-child mode-1777 capability shape was rejected")
	}

	if err := os.Chmod(shared, 0o0777); err != nil {
		t.Fatalf("remove sticky bit: %v", err)
	}
	info, err = os.Lstat(shared)
	if err != nil {
		t.Fatalf("inspect non-sticky metadata: %v", err)
	}
	if validSharedLockDirectoryMetadata("/var/tmp/coding-ethos-sandbox-lock-test", info) {
		t.Fatal("a non-sticky external directory became a sandbox write capability")
	}
	if validSharedLockDirectoryMetadata("/tmp/coding-ethos-sandbox-lock-test", info) {
		t.Fatal("a path outside /var/tmp became a sandbox write capability")
	}
}

func TestCleanPolicyPathAllowsCurrentTerminal(t *testing.T) {
	root := t.TempDir()

	target, err := os.Readlink("/proc/self/fd/1")
	if err != nil || !strings.HasPrefix(target, "/dev/pts/") {
		t.Skip("test requires stdout attached to a PTY")
	}

	terminalPath, ok := cleanPolicyPath(root, target, false)
	if !ok || terminalPath != target {
		t.Fatalf("cleanPolicyPath terminal = %q %t", terminalPath, ok)
	}

	err = prepareWritablePath(root, target, target, false)
	if err != nil {
		t.Fatalf("prepareWritablePath terminal error = %v", err)
	}
}

func TestPrepareWritablePathsFiltersGitAndCreatesRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	external := filepath.Join(root, "external")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatalf("create absolute write path fixture: %v", err)
	}

	config := options{
		paths: &sandboxPaths{repoRoot: root},
		writePaths: []string{
			".coding-ethos/cache",
			".git/config",
			external,
		},
	}

	paths, err := prepareWritablePaths(config)
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}

	want := filepath.Join(root, ".coding-ethos", "cache")
	if !slices.Contains(paths, want) {
		t.Fatalf("write paths missing %s: %#v", want, paths)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("declared relative write path was not created: %v", err)
	}
	if slices.Contains(paths, filepath.Join(root, ".git", "config")) {
		t.Fatalf(".git write path leaked through: %#v", paths)
	}
}

func TestPrepareWritablePathsAllowsExplicitGitWrites(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o700); err != nil {
		t.Fatalf("create git dir fixture: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: root},
		writePaths:     []string{gitDir},
		allowGitWrites: true,
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, gitDir) {
		t.Fatalf("git write path missing: %#v", paths)
	}
}

// TestPrepareWritablePathsRefusesAWritableParentOfGitWithoutAPin makes the
// protection of .git structural rather than conventional.
//
// A Landlock grant reaches everything beneath it, so granting the worktree
// root -- which is what lets git create, delete and rename top-level files --
// grants .git too unless a mount says otherwise. The caller that grants the
// root does pass a pin. Nothing stopped the next caller from forgetting, and
// forgetting would be silent: the sandbox would come up, everything would
// work, and an agent could write .git/config or drop in a hook and so step
// outside the git wrapper entirely.
func TestPrepareWritablePathsRefusesAWritableParentOfGitWithoutAPin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o700); err != nil {
		t.Fatalf("create git dir fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: root},
		writePaths:     []string{root},
		allowGitWrites: true,
	})
	if err == nil {
		t.Fatal("granting a directory that holds .git without pinning .git " +
			"read-only was accepted; the grant reaches .git and nothing " +
			"downstream would notice")
	}

	// With the pin the same grant is exactly what the merge needs.
	paths, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: root},
		writePaths:     []string{root},
		readOnlyPaths:  []string{gitDir},
		allowGitWrites: true,
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() with a pin error = %v", err)
	}
	if !slices.Contains(paths, root) {
		t.Fatalf("worktree root missing, so no top-level file can be "+
			"replaced: %#v", paths)
	}
}

func TestPrepareWritablePathsAllowsParentWhenGitIsExplicitlyWritable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o700); err != nil {
		t.Fatalf("create git dir fixture: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: root},
		writePaths:     []string{gitDir, root},
		allowGitWrites: true,
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	for _, want := range []string{gitDir, root} {
		if !slices.Contains(paths, want) {
			t.Fatalf("explicit write capability missing %s: %#v", want, paths)
		}
	}
}

func TestPrepareWritablePathsAllowsGPGHomeForSignedGit(t *testing.T) {
	root := t.TempDir()
	gpgHome := filepath.Join(t.TempDir(), "gnupg")
	t.Setenv("GNUPGHOME", gpgHome)
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatalf("create GPG home fixture: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{gpgHome},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, gpgHome) {
		t.Fatalf("GPG home write path missing: %#v", paths)
	}
}

func TestPrepareWritablePathsAllowsResolvedGPGHomeSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	gpgHome := filepath.Join(t.TempDir(), "gnupg")
	resolvedGPGHome := filepath.Join(t.TempDir(), "dot-gnupg")
	t.Setenv("GNUPGHOME", gpgHome)
	if err := os.MkdirAll(gpgHome, 0o700); err != nil {
		t.Fatalf("create GPG home fixture: %v", err)
	}
	if err := os.MkdirAll(resolvedGPGHome, 0o700); err != nil {
		t.Fatalf("create resolved GPG home fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(resolvedGPGHome, "trustdb.gpg"),
		[]byte{},
		0o600,
	); err != nil {
		t.Fatalf("create resolved GPG file fixture: %v", err)
	}
	resolvedPrivateKeys := filepath.Join(resolvedGPGHome, "private-keys-v1.d")
	if err := os.MkdirAll(resolvedPrivateKeys, 0o700); err != nil {
		t.Fatalf("create resolved private key fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(resolvedPrivateKeys, "key.key"),
		[]byte{},
		0o600,
	); err != nil {
		t.Fatalf("create private key fixture: %v", err)
	}
	privateKeys := filepath.Join(gpgHome, "private-keys-v1.d")
	if err := os.MkdirAll(privateKeys, 0o700); err != nil {
		t.Fatalf("create GPG private key directory fixture: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(resolvedGPGHome, "trustdb.gpg"),
		filepath.Join(gpgHome, "trustdb.gpg"),
	); err != nil {
		t.Fatalf("create GPG symlink fixture: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(resolvedPrivateKeys, "key.key"),
		filepath.Join(privateKeys, "key.key"),
	); err != nil {
		t.Fatalf("create private key symlink fixture: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{resolvedGPGHome, resolvedPrivateKeys},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, resolvedGPGHome) {
		t.Fatalf("resolved GPG home write path missing: %#v", paths)
	}
	if !slices.Contains(paths, resolvedPrivateKeys) {
		t.Fatalf("resolved private key write path missing: %#v", paths)
	}
}

func TestPrepareWritablePathsAllowsLinkedWorktreeGitCommonDir(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	repo := filepath.Join(parent, "worktree")
	worktreeGitDir := filepath.Join(parent, ".git", "worktrees", "feature")
	gitCommonDir := filepath.Join(
		parent,
		".git",
		"modules",
		"coding-ethos",
	)
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := os.MkdirAll(worktreeGitDir, 0o700); err != nil {
		t.Fatalf("create linked worktree git dir: %v", err)
	}
	if err := os.MkdirAll(gitCommonDir, 0o700); err != nil {
		t.Fatalf("create linked worktree git common dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, ".git"),
		[]byte("gitdir: "+worktreeGitDir+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write linked worktree .git file: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: repo},
		writePaths:     []string{gitCommonDir},
		allowGitWrites: true,
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, gitCommonDir) {
		t.Fatalf("linked worktree git common dir missing: %#v", paths)
	}
}

func TestPrepareWritablePathsRejectsUnrelatedGitMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("create git dir fixture: %v", err)
	}

	unrelated := filepath.Join(t.TempDir(), ".git", "config")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o700); err != nil {
		t.Fatalf("create unrelated git dir fixture: %v", err)
	}
	if err := os.WriteFile(unrelated, []byte("[core]\n"), 0o600); err != nil {
		t.Fatalf("write unrelated git config fixture: %v", err)
	}

	paths, err := prepareWritablePaths(options{
		paths:          &sandboxPaths{repoRoot: root},
		writePaths:     []string{unrelated},
		allowGitWrites: true,
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if slices.Contains(paths, unrelated) {
		t.Fatalf("unrelated git metadata write path leaked through: %#v", paths)
	}
}

func TestPrepareWritablePathsRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".coding-ethos")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{".coding-ethos/cache"},
	})
	if !errors.Is(err, errSymlinkWritePath) {
		t.Fatalf("prepareWritablePaths() error = %v, want symlink rejection", err)
	}
}

func TestPrepareWritablePathsAllowsFilePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile := filepath.Join(root, ".coding-ethos")
	if err := os.WriteFile(writeFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create file fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{".coding-ethos"},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
}

func TestPrepareWritablePathsCreatesMissingFilePathAsFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile := filepath.Join(root, "pkg", "new.py")

	paths, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{"pkg/new.py"},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, writeFile) {
		t.Fatalf("prepared write paths missing %s: %#v", writeFile, paths)
	}

	info, statErr := os.Stat(writeFile)
	if statErr != nil {
		t.Fatalf("created writable file missing: %v", statErr)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		t.Fatalf("writable path was not created as regular file: %s", info.Mode())
	}
}

func TestPrepareWritablePathsRejectsFileAsDirectoryComponent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	component := filepath.Join(root, "pkg")
	if err := os.WriteFile(component, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create file component fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{"pkg/new.py"},
	})
	if !errors.Is(err, errWritePathNotAllowed) {
		t.Fatalf("prepareWritablePaths() error = %v, want path rejection", err)
	}
}

func TestPrepareWritablePathsCreatesDotCachePathAsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := filepath.Join(root, ".ruff_cache")

	paths, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{".ruff_cache/"},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, cacheDir) {
		t.Fatalf("prepared write paths missing %s: %#v", cacheDir, paths)
	}

	info, statErr := os.Stat(cacheDir)
	if statErr != nil {
		t.Fatalf("created writable cache path missing: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("dot cache path was not created as directory: %s", info.Mode())
	}
}

func TestPrepareWritablePathsCreatesDottedDirectoryPathAsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := filepath.Join(root, "pkg.v1")

	paths, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{"pkg.v1/"},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}
	if !slices.Contains(paths, cacheDir) {
		t.Fatalf("prepared write paths missing %s: %#v", cacheDir, paths)
	}

	info, statErr := os.Stat(cacheDir)
	if statErr != nil {
		t.Fatalf("created writable dotted directory missing: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("dotted directory path was not created as directory: %s", info.Mode())
	}
}

func TestJoinPolicyErrors(t *testing.T) {
	t.Parallel()

	err := joinPolicyErrors(errors.New("first"), errors.New("second"))
	if err == nil || !strings.Contains(err.Error(), "first") ||
		!strings.Contains(err.Error(), "second") {
		t.Fatalf("joinPolicyErrors() = %v", err)
	}
}

func TestValidatedExecutablePathRequiresAbsoluteExecutableFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executable := filepath.Join(root, "tool")
	writeTestFile(t, executable, 0o700)

	got, err := validatedExecutablePath(executable, "test tool")
	if err != nil {
		t.Fatalf("validatedExecutablePath() error = %v", err)
	}
	if got != executable {
		t.Fatalf("validatedExecutablePath() = %q, want %q", got, executable)
	}

	_, err = validatedExecutablePath("relative-tool", "test tool")
	if !errors.Is(err, errExecutableAbsolute) {
		t.Fatalf("relative executable error = %v, want absolute-path error", err)
	}

	nonExecutable := filepath.Join(root, "not-executable")
	writeTestFile(t, nonExecutable, 0o600)

	_, err = validatedExecutablePath(nonExecutable, "test tool")
	if !errors.Is(err, errExecutableInvalid) {
		t.Fatalf("non-executable error = %v, want invalid-executable error", err)
	}

	_, err = validatedExecutablePath(root, "test tool")
	if !errors.Is(err, errExecutableInvalid) {
		t.Fatalf("directory executable error = %v, want invalid-executable error", err)
	}
}

func TestValidatedGitTargetsRequiresAbsoluteExecutableTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "git")
	duplicate := filepath.Join(root, "git-duplicate")
	writeTestFile(t, target, 0o700)
	if err := os.Symlink(target, duplicate); err != nil {
		t.Fatalf("create git target symlink fixture: %v", err)
	}

	got, err := validatedGitTargets([]string{"", target, duplicate})
	if err != nil {
		t.Fatalf("validatedGitTargets() error = %v", err)
	}
	if !slices.Equal(got, []string{target}) {
		t.Fatalf("validatedGitTargets() = %#v, want %#v", got, []string{target})
	}

	_, err = validatedGitTargets([]string{"git"})
	if !errors.Is(err, errGitTargetAbsolute) {
		t.Fatalf("relative git target error = %v, want absolute-target error", err)
	}

	_, err = validatedGitTargets([]string{"", " "})
	if !errors.Is(err, errGitTargetRequired) {
		t.Fatalf("empty git targets error = %v, want required-target error", err)
	}
}

func TestValidatedGitWrapperDelegatesExecutableValidation(t *testing.T) {
	t.Parallel()

	wrapper := filepath.Join(t.TempDir(), "policy-git")
	writeTestFile(t, wrapper, 0o700)

	got, err := validatedGitWrapper(wrapper)
	if err != nil {
		t.Fatalf("validatedGitWrapper() error = %v", err)
	}
	if got != wrapper {
		t.Fatalf("validatedGitWrapper() = %q, want %q", got, wrapper)
	}
}

func TestMountInfoHasSharedPropagation(t *testing.T) {
	t.Parallel()

	shared := "36 25 0:32 / / rw,relatime shared:1 - ext4 /dev/root rw\n"
	if !mountInfoHasSharedPropagation(shared) {
		t.Fatal("mountInfoHasSharedPropagation() did not detect shared propagation")
	}

	private := "36 25 0:32 / / rw,relatime master:1 - ext4 /dev/root rw\n" +
		"37 36 0:33 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw\n"
	if mountInfoHasSharedPropagation(private) {
		t.Fatal("mountInfoHasSharedPropagation() reported non-shared mounts as shared")
	}

	if mountInfoHasSharedPropagation("malformed\n") {
		t.Fatal("mountInfoHasSharedPropagation() reported malformed mount info as shared")
	}
}

func TestReadOnlyMountInfoForPathRequiresAnExactReadOnlyMount(t *testing.T) {
	t.Parallel()

	content := "36 25 0:32 / /workspace rw,nosuid - ext4 /dev/root rw\n" +
		"37 36 0:32 /repo/.git /workspace/repo/.git ro,nosuid - ext4 /dev/root rw\n" +
		"38 36 0:32 /repo/cache /workspace/repo/cache rw,nosuid - ext4 /dev/root rw\n" +
		"39 36 0:32 /repo/a\\040b /workspace/repo/a\\040b ro,nosuid - ext4 /dev/root rw\n"

	if !readOnlyMountInfoForPath(content, "/workspace/repo/.git") {
		t.Fatal("exact read-only .git mount was not recognized")
	}
	if !readOnlyMountInfoForPath(content, "/workspace/repo/a b") {
		t.Fatal("escaped read-only mountpoint was not recognized")
	}
	for _, path := range []string{
		"/workspace/repo",
		"/workspace/repo/cache",
		"/workspace/repo/.git/config",
	} {
		if readOnlyMountInfoForPath(content, path) {
			t.Fatalf("%s was accepted without an exact read-only mount", path)
		}
	}
}

func TestWritablePathOverridesSelectOnlyChildrenOfPins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pin := filepath.Join(root, ".coding-ethos")
	cache := filepath.Join(pin, "cache")
	state := filepath.Join(pin, "state")
	gitDir := filepath.Join(root, ".git")

	got := writablePathOverrides(
		[]string{gitDir, pin},
		[]string{root, cache, state, gitDir, "/dev/null"},
	)
	want := []string{cache, state}
	if !slices.Equal(got, want) {
		t.Fatalf("writablePathOverrides() = %#v, want %#v", got, want)
	}
}

func TestIsMountPropagationPermissionError(t *testing.T) {
	t.Parallel()

	for _, err := range []error{unix.EPERM, unix.EACCES} {
		if !isMountPropagationPermissionError(err) {
			t.Fatalf("isMountPropagationPermissionError(%v) = false", err)
		}
	}

	if isMountPropagationPermissionError(os.ErrNotExist) {
		t.Fatal("isMountPropagationPermissionError() accepted unrelated error")
	}
}

func TestLandlockAllowedWriteAccessSeparatesDirectoriesAndFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "state.db")
	writeTestFile(t, filePath, 0o600)

	dirInfo, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat directory fixture: %v", err)
	}
	if got := landlockAllowedWriteAccess(dirInfo); got != landlockWriteAccess {
		t.Fatalf("directory write access = %d, want %d", got, landlockWriteAccess)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat file fixture: %v", err)
	}
	if got := landlockAllowedWriteAccess(fileInfo); got != landlockFileWriteAccess {
		t.Fatalf("file write access = %d, want %d", got, landlockFileWriteAccess)
	}
}

func TestLandlockParentFDReturnsOpenFileDescriptor(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.db")
	writeTestFile(t, path, 0o600)

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	got, err := landlockParentFD(file)
	if err != nil {
		t.Fatalf("landlockParentFD() error = %v", err)
	}
	if got != int32(file.Fd()) {
		t.Fatalf("landlockParentFD() = %d, want %d", got, file.Fd())
	}
}

func writeTestFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatalf("create file fixture %s: %v", path, err)
	}
}
