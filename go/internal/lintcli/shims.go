// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var (
	errToolsBinDirRequired = apperror.StaticError(
		"--tools-bin-dir is required with --install-shims",
	)
	errRunnerRequired = apperror.StaticError(
		"--runner is required with --install-shims",
	)
)

const (
	shimExecutableMode os.FileMode = 0o755
	shimWriteMode      os.FileMode = 0o600
)

func installCapturedToolShims(toolsBinDir, runner, ethosRoot string) error {
	if strings.TrimSpace(toolsBinDir) == "" {
		return errToolsBinDirRequired
	}

	if strings.TrimSpace(runner) == "" {
		return errRunnerRequired
	}

	err := os.MkdirAll(toolsBinDir, shimExecutableMode)
	if err != nil {
		return fmt.Errorf("create shim directory: %w", err)
	}

	for _, captureTool := range toolcatalog.CapturedLintTools() {
		tool, found := toolcatalog.HookOwnedTool(captureTool.Name)
		if !found || !managedToolAvailable(tool, ethosRoot) {
			_ = os.Remove(filepath.Join(toolsBinDir, captureTool.Name))

			continue
		}

		err := installCapturedToolShim(toolsBinDir, runner, captureTool.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

func installCapturedToolShim(toolsBinDir, runner, tool string) error {
	shim := filepath.Join(toolsBinDir, tool)
	tmp := fmt.Sprintf("%s.tmp.%d", shim, os.Getpid())

	content := fmt.Sprintf(
		"#!/usr/bin/env bash\n"+
			"set -euo pipefail\n"+
			"unset %s\n"+
			"export CODING_ETHOS_POLICY_TOOL_SHIM=1\n"+
			"exec %s policy-tool %s \"$@\"\n",
		realToolEnvVar(tool),
		shellQuote(runner),
		shellQuote(tool),
	)

	err := os.WriteFile(tmp, []byte(content), shimWriteMode)
	if err != nil {
		return fmt.Errorf("write %s shim: %w", tool, err)
	}

	err = os.Chmod(tmp, shimExecutableMode)
	if err != nil {
		return fmt.Errorf("mark %s shim executable: %w", tool, err)
	}

	err = os.Rename(tmp, shim)
	if err != nil {
		return fmt.Errorf("install %s shim: %w", tool, err)
	}

	return nil
}

func managedToolAvailable(tool toolcatalog.Tool, ethosRoot string) bool {
	if managed := tool.ManagedExecutablePath(ethosRoot); managed != "" {
		return managedcapture.ExecutableAvailable(managed)
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
		return "CODING_ETHOS_REAL_" + strings.ReplaceAll(
			strings.ToUpper(tool),
			"-",
			"_",
		)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
