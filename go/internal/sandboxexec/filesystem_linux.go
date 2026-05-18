// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	privateDirMode        = 0o700
	remountCommandTimeout = 5 * time.Second
)

func applyFilesystemPolicy(options options) error {
	var errs []error

	err := makeMountsPrivate()
	if err != nil {
		return err
	}

	writePaths, err := prepareWritablePaths(options)
	if err != nil {
		return err
	}

	err = bindPath(options.paths.repoRoot)
	if err != nil {
		return err
	}

	for _, path := range []string{"/home", "/root"} {
		err = hideCredentialDir(path)
		if err != nil {
			errs = append(errs, err)
		}
	}

	for _, path := range writePaths {
		err = bindWritablePath(path)
		if err != nil {
			errs = append(errs, err)
		}
	}

	err = remountReadOnly(options.paths.repoRoot)
	if err != nil {
		errs = append(errs, err)
	}

	err = remountReadOnly("/")
	if err != nil {
		errs = append(errs, err)
	}

	return joinPolicyErrors(errs...)
}

func makeMountsPrivate() error {
	err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("make mount namespace private: %w", err)
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

func hideCredentialDir(path string) error {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("stat credential directory %s: %w", path, err)
	}

	err = syscall.Mount(
		"tmpfs",
		path,
		"tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC,
		"size=1m,mode=0700",
	)
	if err != nil {
		return fmt.Errorf("hide credential directory %s: %w", path, err)
	}

	return nil
}

func remountReadOnly(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remountCommandTimeout)
	defer cancel()

	command := safeexec.CommandContext(
		ctx,
		"/bin/mount",
		"-o",
		"remount,bind,ro",
		path,
		path,
	)

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"remount %s read-only: %w: %s",
			path,
			err,
			string(output),
		)
	}

	return nil
}

func bindWritablePath(path string) error {
	err := bindPath(path)
	if err != nil {
		return fmt.Errorf("bind mount writable path %s: %w", path, err)
	}

	return nil
}

func bindPath(path string) error {
	err := syscall.Mount(path, path, "", syscall.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("bind mount %s: %w", path, err)
	}

	return nil
}

func sandboxedCommandSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID,
		Setpgid:    true,
	}
}
