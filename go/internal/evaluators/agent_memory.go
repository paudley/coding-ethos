// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"path/filepath"
	"strings"
)

func isAgentWorkspacePath(file string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(file))
	segments := strings.Split(cleaned, "/")
	for index, segment := range segments {
		if !isAgentConfigDir(segment) {
			continue
		}
		for _, candidate := range segments[index+1:] {
			switch strings.ToLower(candidate) {
			case "memory.md", "memory", "memories", "plans":
				return true
			}
		}
	}

	return false
}

func isAgentConfigDir(segment string) bool {
	switch strings.ToLower(segment) {
	case ".claude", ".codex", ".gemini":
		return true
	default:
		return false
	}
}
