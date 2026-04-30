// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var (
	errToolsBinDirRequired = errors.New("--tools-bin-dir is required with --install-shims")
	errRunGoHookRequired   = errors.New("--run-go-hook is required with --install-shims")
)

func installCapturedToolShims(toolsBinDir string, runGoHook string, ethosRoot string) error {
	if strings.TrimSpace(toolsBinDir) == "" {
		return errToolsBinDirRequired
	}
	if strings.TrimSpace(runGoHook) == "" {
		return errRunGoHookRequired
	}
	if err := os.MkdirAll(toolsBinDir, 0o755); err != nil {
		return fmt.Errorf("create shim directory: %w", err)
	}

	for _, captureTool := range toolcatalog.CapturedLintTools() {
		tool, found := toolcatalog.HookOwnedTool(captureTool.Name)
		if !found || !managedToolAvailable(tool, ethosRoot) {
			_ = os.Remove(filepath.Join(toolsBinDir, captureTool.Name))
			continue
		}
		if err := installCapturedToolShim(toolsBinDir, runGoHook, captureTool.Name); err != nil {
			return err
		}
	}

	return nil
}

func installCapturedToolShim(toolsBinDir string, runGoHook string, tool string) error {
	shim := filepath.Join(toolsBinDir, tool)
	tmp := fmt.Sprintf("%s.tmp.%d", shim, os.Getpid())
	content := fmt.Sprintf(
		"#!/usr/bin/env bash\nset -euo pipefail\nunset %s\nexec %s policy-tool %s \"$@\"\n",
		realToolEnvVar(tool),
		shellQuote(runGoHook),
		shellQuote(tool),
	)
	if err := os.WriteFile(tmp, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write %s shim: %w", tool, err)
	}
	if err := os.Rename(tmp, shim); err != nil {
		return fmt.Errorf("install %s shim: %w", tool, err)
	}

	return nil
}

func managedToolAvailable(tool toolcatalog.Tool, ethosRoot string) bool {
	if managed := tool.ManagedExecutablePath(ethosRoot); managed != "" {
		return isExecutable(managed)
	}

	uvBin := strings.TrimSpace(os.Getenv("UV"))
	if uvBin == "" {
		uvBin = "uv"
	}
	_, err := exec.LookPath(uvBin)

	return err == nil
}

func realToolEnvVar(tool string) string {
	switch tool {
	case "golangci-lint":
		return "CODING_ETHOS_REAL_GOLANGCI_LINT"
	default:
		return "CODING_ETHOS_REAL_" + strings.ReplaceAll(strings.ToUpper(tool), "-", "_")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
