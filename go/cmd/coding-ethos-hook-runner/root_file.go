// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func readRootedFile(path string) ([]byte, error) {
	root, name, err := openFileRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	payload, err := root.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return payload, nil
}

func readRootedFilePrefix(path string, limit int64) ([]byte, error) {
	root, name, err := openFileRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	payload, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, fmt.Errorf("read %s prefix: %w", path, err)
	}

	return payload, nil
}

func statRootedFile(path string) (os.FileInfo, error) {
	root, name, err := openFileRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	info, err := root.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	return info, nil
}

func writeRootedFile(path string, payload []byte, mode os.FileMode) error {
	root, name, err := openFileRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()

	err = root.WriteFile(name, payload, mode)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

func openFileRoot(path string) (*os.Root, string, error) {
	rootPath := filepath.Dir(path)

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", fmt.Errorf("open root %s: %w", rootPath, err)
	}

	return root, filepath.Base(path), nil
}
