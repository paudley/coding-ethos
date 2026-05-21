// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
)

const capturedProcessPathEnv = "PATH"

const gitShimProbeMaxBytes = 4096

func managedSubprocessGitEnv(
	ctx context.Context,
	tempDir string,
) (string, string, error) {
	realGit, err := resolvedManagedSubprocessGit(ctx)
	if err != nil {
		return "", "", err
	}

	pathPrefix, err := managedSubprocessPathPrefix(tempDir, realGit)
	if err != nil {
		return "", "", err
	}

	return realGit, pathPrefix, nil
}

func capturedProcessEnvBlocked(name string) bool {
	if name == evaluators.RealGitEnv {
		return false
	}

	if strings.HasPrefix(name, "CODE_ETHOS_") ||
		strings.HasPrefix(name, "CODING_ETHOS_") {
		return true
	}

	if strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
		strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
		return true
	}

	switch name {
	case "GIT_HOOK_SRC_DIR",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE",
		"INVOCATION_CWD",
		"MANAGED_TOOLCHAIN_MANIFEST",
		"POLICY_METADATA",
		"TOOLS_SRC_DIR":
		return true
	default:
		return false
	}
}

func capturedProcessPathWithPrefix(pathValue, prefix string) string {
	kept := []string{}
	if prefix != "" {
		kept = append(kept, prefix)
	}

	for _, entry := range filepath.SplitList(pathValue) {
		if entry == "" || pathEntryHasCodingEthosGitShim(entry) {
			continue
		}

		kept = append(kept, entry)
	}

	for _, entry := range capturedSystemPathCandidates() {
		if slices.Contains(kept, entry) {
			continue
		}

		kept = append(kept, entry)
	}

	return strings.Join(kept, string(os.PathListSeparator))
}

func capturedSystemPathCandidates() []string {
	return []string{
		"/usr/local/sbin",
		"/usr/local/bin",
		"/usr/sbin",
		"/usr/bin",
		"/sbin",
		"/bin",
	}
}

func pathEntryHasCodingEthosGitShim(entry string) bool {
	gitPath := filepath.Join(entry, "git")

	info, err := os.Stat(gitPath)
	if err != nil || info.IsDir() || info.Size() > gitShimProbeMaxBytes {
		return false
	}

	payload, err := os.ReadFile(gitPath)
	if err != nil {
		return false
	}

	text := string(payload)

	return strings.Contains(text, "coding-ethos-run") &&
		strings.Contains(text, "policy-git")
}
