// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func policyToolParentExecutable() (string, error) {
	parentExecutable, err := os.Readlink(
		filepath.Join("/proc", strconv.Itoa(os.Getppid()), "exe"),
	)
	if err != nil {
		return "", fmt.Errorf("resolve parent executable identity: %w", err)
	}

	return parentExecutable, nil
}
