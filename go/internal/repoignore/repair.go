// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package repoignore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	gitignoreFileMode = 0o600
	repairLineSlack   = 2
	sectionHeader     = "# coding-ethos generated runtime output"
)

// RuntimePaths returns the generated coding-ethos output paths that should be
// ignored while keeping .coding-ethos/memories trackable.
func RuntimePaths() []string {
	return []string{
		".code-ethos/cache/",
		".coding-ethos/cache/",
		".coding-ethos/code-intel.db",
		".coding-ethos/hook-runs/",
		".coding-ethos/lint-runs/",
		".coding-ethos/prune-runs/",
		".coding-ethos/state/",
	}
}

// RepairGitignore removes broad coding-ethos memory-blocking ignore entries
// and appends missing generated runtime output ignores.
func RepairGitignore(repoRoot string) (bool, error) {
	path := filepath.Join(filepath.Clean(repoRoot), ".gitignore")
	payload, err := os.ReadFile(path)

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read .gitignore %s: %w", path, err)
	}

	original := string(payload)
	repaired := repairGitignorePayload(original)

	if repaired == original {
		return false, nil
	}

	err = os.WriteFile(path, []byte(repaired), gitignoreFileMode)
	if err != nil {
		return false, fmt.Errorf("write .gitignore %s: %w", path, err)
	}

	return true, nil
}

func repairGitignorePayload(payload string) string {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	filtered := make([]string, 0, len(lines)+len(RuntimePaths())+repairLineSlack)
	present := map[string]bool{}

	for _, line := range lines {
		normalized := normalizeGitignoreLine(line)
		if blockedMemoryIgnore(normalized) {
			continue
		}

		filtered = append(filtered, line)

		if normalized != "" {
			present[normalized] = true
		}
	}

	missing := missingRuntimeIgnores(present)
	if len(missing) == 0 {
		return strings.Join(filtered, "\n") + "\n"
	}

	if len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) != "" {
		filtered = append(filtered, "")
	}

	if !slices.Contains(filtered, sectionHeader) {
		filtered = append(filtered, sectionHeader)
	}

	filtered = append(filtered, missing...)

	return strings.Join(filtered, "\n") + "\n"
}

func normalizeGitignoreLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "!") {
		return ""
	}

	return strings.TrimPrefix(trimmed, "/")
}

func blockedMemoryIgnore(normalized string) bool {
	switch normalized {
	case ".coding-ethos", ".coding-ethos/", "**/.coding-ethos", "**/.coding-ethos/",
		".coding-ethos/memories", ".coding-ethos/memories/",
		".coding-ethos/memories/MEMORY.md", ".coding-ethos/memories/*.yaml":
		return true
	default:
		return false
	}
}

func missingRuntimeIgnores(present map[string]bool) []string {
	required := RuntimePaths()
	missing := make([]string, 0, len(required))

	for _, path := range required {
		if !present[path] {
			missing = append(missing, path)
		}
	}

	return missing
}
