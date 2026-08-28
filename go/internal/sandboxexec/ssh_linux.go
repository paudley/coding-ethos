// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	systemSSHConfigDir = "/etc/ssh"
	sshConfigDirMode   = 0o755
	sshConfigFileMode  = 0o644
)

// sshConfigTree is the readable slice of the system OpenSSH configuration as
// seen by the sandbox identity, plus whether that identity is allowed to keep
// using it in place.
type sshConfigTree struct {
	dirs              []string
	files             []sshConfigFile
	needsOwnershipFix bool
}

type sshConfigFile struct {
	relativePath string
	content      []byte
}

// applySystemSSHConfig re-presents the system OpenSSH client configuration as
// files owned by the sandbox identity.
//
// The launcher maps only the caller's uid into the sandbox user namespace, so
// every root-owned file surfaces as the kernel overflow uid. OpenSSH refuses
// configuration owned by anyone other than root or the invoking user, which
// turns that mapping artifact into "Bad owner or permissions" for every file
// included from /etc/ssh/ssh_config — and with it breaks every ssh-based git
// fetch or push routed through the sandbox. Rebuilding the readable
// configuration on a tmpfs owned by the mapped identity restores the ownership
// truth the check wants to see without touching the host filesystem.
//
// Only tools that keep network access get the rebuild: everything else cannot
// reach an ssh server, and a nested supervisor that has already dropped mount
// capability must not fail setup over a capability its tool will never use.
// The ownership inspection likewise keeps this idempotent under composed
// supervisors — when a parent already presents /etc/ssh with acceptable
// ownership, there is nothing to mount.
func applySystemSSHConfig(config options) error {
	if !config.requiresNetwork {
		return nil
	}

	tree, err := inspectSystemSSHConfig(systemSSHConfigDir, os.Getuid())
	if err != nil {
		return err
	}

	if !tree.needsOwnershipFix {
		return nil
	}

	err = isolateMountPropagation()
	if err != nil {
		return err
	}

	err = unix.Mount(
		"tmpfs",
		systemSSHConfigDir,
		"tmpfs",
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		"mode=0755",
	)
	if err != nil {
		return fmt.Errorf(
			"mount user-owned ssh config over %s: %w",
			systemSSHConfigDir,
			err,
		)
	}

	err = materializeSSHConfigTree(systemSSHConfigDir, tree)
	if err != nil {
		return err
	}

	err = remountReadOnlyBind(systemSSHConfigDir)
	if err != nil {
		return fmt.Errorf(
			"remount rebuilt ssh config read-only at %s: %w",
			systemSSHConfigDir,
			err,
		)
	}

	return nil
}

// inspectSystemSSHConfig walks root and captures every directory and readable
// regular file, recording whether any readable file is owned by neither root
// nor ownerUID — the exact ownership condition OpenSSH enforces on included
// configuration files.
func inspectSystemSSHConfig(root string, ownerUID int) (sshConfigTree, error) {
	tree := sshConfigTree{}

	walkErr := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if path == root {
					// A host without the directory has nothing for ssh to
					// reject; the rebuild is pointless either way.
					return filepath.SkipAll
				}

				// Entries the sandbox identity cannot list (private key
				// directories) are not client configuration; leaving them out
				// of the rebuild is the point, not a failure.
				return nil
			}

			return collectSSHConfigEntry(&tree, root, path, entry, ownerUID)
		},
	)
	if walkErr != nil {
		return sshConfigTree{}, fmt.Errorf(
			"inspect system ssh config %s: %w",
			root,
			walkErr,
		)
	}

	return tree, nil
}

func collectSSHConfigEntry(
	tree *sshConfigTree,
	root, path string,
	entry fs.DirEntry,
	ownerUID int,
) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("resolve ssh config path %s: %w", path, err)
	}

	if entry.IsDir() {
		if relative != "." {
			tree.dirs = append(tree.dirs, relative)
		}

		return nil
	}

	info, content, readable := loadReadableRegularFile(path)
	if !readable {
		return nil
	}

	tree.files = append(tree.files, sshConfigFile{
		relativePath: relative,
		content:      content,
	})

	if stat, ok := info.Sys().(*syscall.Stat_t); ok &&
		stat.Uid != 0 && ownerUID >= 0 && int64(stat.Uid) != int64(ownerUID) {
		tree.needsOwnershipFix = true
	}

	return nil
}

// loadReadableRegularFile reports whether path is a regular file the current
// identity can read, returning its metadata and content when it is.
//
// Stat follows symlinks so a linked config is captured as the regular file ssh
// would read; dangling links, sockets, and devices have no place in the
// rebuilt configuration, and a file the sandbox identity cannot read (a host
// key) cannot matter to an ssh client running as that identity either.
func loadReadableRegularFile(path string) (os.FileInfo, []byte, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, false
	}

	// #nosec G304 -- path comes from walking the system ssh directory.
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}

	return info, content, true
}

func materializeSSHConfigTree(root string, tree sshConfigTree) error {
	for _, dir := range tree.dirs {
		err := os.MkdirAll(filepath.Join(root, dir), sshConfigDirMode)
		if err != nil {
			return fmt.Errorf("recreate ssh config directory %s: %w", dir, err)
		}
	}

	for _, file := range tree.files {
		path := filepath.Join(root, file.relativePath)

		err := os.MkdirAll(filepath.Dir(path), sshConfigDirMode)
		if err != nil {
			return fmt.Errorf(
				"recreate ssh config parent for %s: %w",
				file.relativePath,
				err,
			)
		}

		err = os.WriteFile(path, file.content, sshConfigFileMode)
		if err != nil {
			return fmt.Errorf(
				"rewrite ssh config file %s: %w",
				file.relativePath,
				err,
			)
		}
	}

	return nil
}
