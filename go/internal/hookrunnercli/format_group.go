// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

func runFormatGroupCommand(cfg Config, files []string) int {
	return runFormatGroup(cfg, files, false)
}

func runFormatGroup(cfg Config, files []string, restage bool) int {
	snapshots := map[string]fileSnapshot(nil)
	if restage {
		snapshots = fileSnapshots(files)
	}

	exit := fixText(cfg, files)
	if runPyupgrade(files) != 0 {
		exit = 1
	}

	if runRuffFormat(files) != 0 {
		exit = 1
	}

	if runRuffAutofix(files) != 0 {
		exit = 1
	}

	if runGofmtWrite(files) != 0 {
		exit = 1
	}

	if restage && exit == 0 {
		changed := changedExistingFiles(files, snapshots)
		if len(changed) > 0 && restageFiles(changed) != 0 {
			exit = 1
		}
	}

	return exit
}

type fileSnapshot struct {
	hash  [sha256.Size]byte
	found bool
}

func fileSnapshots(files []string) map[string]fileSnapshot {
	snapshots := make(map[string]fileSnapshot, len(files))
	for _, file := range existingFiles(files) {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		snapshots[file] = fileSnapshot{
			hash:  sha256.Sum256(content),
			found: true,
		}
	}

	return snapshots
}

func changedExistingFiles(
	files []string,
	snapshots map[string]fileSnapshot,
) []string {
	changed := []string{}

	for _, file := range existingFiles(files) {
		before, ok := snapshots[file]
		if !ok || !before.found {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		if sha256.Sum256(content) != before.hash {
			changed = append(changed, file)
		}
	}

	return changed
}

func runPyupgrade(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}

	version := configuredPythonVersion()
	target := "--py" + strings.ReplaceAll(version, ".", "") + "-plus"
	args := append([]string{target}, pyFiles...)

	return runManagedPolicyTool("pyupgrade", args)
}

func runRuffFormat(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}

	return runManagedPolicyTool("ruff-format", append([]string{"format"}, pyFiles...))
}

func runRuffAutofix(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}

	return runManagedPolicyTool("ruff-autofix", append([]string{"check"}, pyFiles...))
}

func runGofmtWrite(files []string) int {
	goFiles := goFiles(existingFiles(files))
	if len(goFiles) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktreeName()
	if !ok {
		return 1
	}

	return runManagedPolicyTool("golangci-lint-format", []string{worktree})
}

func formatPythonFiles(files []string) []string {
	selected := []string{}

	for _, path := range existingFiles(files) {
		if !strings.HasSuffix(path, ".py") && !strings.HasSuffix(path, ".pyi") {
			continue
		}

		if strings.HasPrefix(path, ".venv/") ||
			strings.Contains(path, "/.venv/") ||
			strings.HasSuffix(path, "/vulture_whitelist.py") ||
			path == "vulture_whitelist.py" {
			continue
		}

		selected = append(selected, path)
	}

	return selected
}

func configuredPythonVersion() string {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return defaultPythonVersion
	}

	value, ok := rootConfigValue(rootConfig, "style.python_version")
	if !ok {
		return defaultPythonVersion
	}

	version := strings.TrimSpace(fmt.Sprint(value))
	if version == "" || version == nilString {
		return defaultPythonVersion
	}

	return version
}

func genericToolFailureFindingForResult(
	name string,
	result externalToolResult,
) hookFinding {
	if result.TimedOut {
		return hookFinding{
			Tool:     name,
			Severity: "error",
			Code:     timeoutCode,
			Message:  "external tool timed out",
			Detail:   "timeout exceeded before the tool completed",
		}
	}

	return hookFinding{
		Tool:     name,
		Severity: "error",
		Message:  fmt.Sprintf("external tool exited with status %d", result.ExitCode),
	}
}
