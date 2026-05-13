// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolchaincli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

func installNPMBinary(
	packageName string,
	version string,
	binary string,
	expectedIntegrity string,
	lockDir string,
	destDir string,
) error {
	if strings.TrimSpace(packageName) == "" {
		return apperror.StaticError("npm package name is required")
	}

	if strings.TrimSpace(version) == "" {
		return apperror.StaticError("npm package version is required")
	}

	if strings.TrimSpace(binary) == "" {
		return apperror.StaticError("npm binary name is required")
	}

	if strings.TrimSpace(lockDir) == "" || strings.TrimSpace(lockDir) == "-" {
		return apperror.StaticError("npm lock directory is required")
	}

	err := verifyNPMIntegrity(packageName, version, expectedIntegrity)
	if err != nil {
		return err
	}

	packageRoot := filepath.Join(destDir, ".npm", binary)

	err = os.RemoveAll(packageRoot)
	if err != nil {
		return fmt.Errorf("remove stale npm package root %s: %w", packageRoot, err)
	}

	err = os.MkdirAll(packageRoot, directoryMode)
	if err != nil {
		return fmt.Errorf("create npm package root %s: %w", packageRoot, err)
	}

	err = copyNPMManifestFiles(lockDir, packageRoot)
	if err != nil {
		return err
	}

	command := safeexec.CommandContext(
		context.Background(),
		"npm",
		"ci",
		"--ignore-scripts",
		"--omit=dev",
	)
	command.Dir = packageRoot

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"install npm tool %s@%s: %w: %s",
			packageName,
			version,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return writeNPMBinaryWrapper(packageRoot, binary, filepath.Join(destDir, binary))
}

func copyNPMManifestFiles(lockDir, packageRoot string) error {
	for _, name := range []string{"package.json", "package-lock.json"} {
		err := copyNPMManifestFile(lockDir, packageRoot, name)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyNPMManifestFile(lockDir, packageRoot, name string) error {
	if name != "package.json" && name != "package-lock.json" {
		return apperror.Wrapf(
			apperror.StaticError("unsupported npm lock file name: %s"),
			"unsupported npm lock file name: %s",
			name,
		)
	}

	source := filepath.Join(lockDir, name)

	payload, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read npm lock file %s: %w", source, err)
	}

	target := filepath.Join(packageRoot, name)

	// #nosec G703 -- name is restricted to fixed npm manifest basenames above.
	err = os.WriteFile(target, payload, privateFileMode)
	if err != nil {
		return fmt.Errorf("write npm lock file %s: %w", target, err)
	}

	return nil
}

func verifyNPMIntegrity(packageName, version, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" || expected == "-" {
		return apperror.StaticError("npm dist integrity is required")
	}

	command := safeexec.CommandContext(
		context.Background(),
		"npm",
		"view",
		packageName+"@"+version,
		"dist.integrity",
	)

	output, err := command.Output()
	if err != nil {
		return fmt.Errorf(
			"read npm integrity for %s@%s: %w",
			packageName,
			version,
			err,
		)
	}

	actual := strings.TrimSpace(string(output))
	if actual != expected {
		return apperror.Wrapf(
			apperror.StaticError("npm integrity mismatch for %s@%s: expected %s, actual %s"),
			"npm integrity mismatch for %s@%s: expected %s, actual %s",
			packageName,
			version,
			expected,
			actual,
		)
	}

	return nil
}

func writeNPMBinaryWrapper(packageRoot, binary, target string) error {
	script := filepath.Join(packageRoot, "node_modules", ".bin", binary)

	info, err := os.Stat(script)
	if err != nil {
		return fmt.Errorf("validate npm binary shim %s: %w", script, err)
	}

	if info.IsDir() {
		return apperror.Wrapf(
			apperror.StaticError("npm binary shim is a directory: %s"),
			"npm binary shim is a directory: %s",
			script,
		)
	}

	payload := fmt.Sprintf(
		"#!/usr/bin/env sh\nexec %s \"$@\"\n",
		shellSingleQuote(script),
	)

	inlineErr0 := os.MkdirAll(filepath.Dir(target), directoryMode)
	if inlineErr0 != nil {
		return fmt.Errorf(
			"create npm binary destination dir %s: %w",
			filepath.Dir(target),
			inlineErr0,
		)
	}

	return writeExecutableFile(target, []byte(payload))
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
