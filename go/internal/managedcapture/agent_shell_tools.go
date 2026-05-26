// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
)

const (
	agentShellToolDirMode   = 0o700
	agentShellToolHashBytes = 8
)

func agentShellToolExecutable(path string) (string, error) {
	toolDir := trustedAgentShellToolDir()
	if toolDir == "" {
		return path, nil
	}

	source := filepath.Clean(strings.TrimSpace(path))
	if source == "" || pathInsideOrSame(toolDir, source) {
		return source, nil
	}

	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("stat agent-shell tool executable %s: %w", source, err)
	}

	if info.IsDir() || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return source, nil
	}

	err = os.MkdirAll(toolDir, agentShellToolDirMode)
	if err != nil {
		return "", fmt.Errorf("create agent-shell tool directory %s: %w", toolDir, err)
	}

	target := agentShellToolTarget(toolDir, source)
	if reusableAgentShellToolTarget(source, target, info) {
		return target, nil
	}

	return installAgentShellToolExecutable(source, target, info)
}

func agentShellToolTarget(toolDir, source string) string {
	sum := sha256.Sum256([]byte(source))

	return filepath.Join(
		toolDir,
		fmt.Sprintf(
			"tool-%x-%s",
			sum[:agentShellToolHashBytes],
			filepath.Base(source),
		),
	)
}

func installAgentShellToolExecutable(
	source, target string,
	info os.FileInfo,
) (string, error) {
	// #nosec G703 -- source is the managed tool executable selected by policy.
	payload, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read agent-shell tool executable %s: %w", source, err)
	}

	temp, err := os.CreateTemp(filepath.Dir(target), ".tool-*")
	if err != nil {
		return "", fmt.Errorf("create temporary agent-shell tool executable: %w", err)
	}

	tempPath := temp.Name()

	defer func() { _ = os.Remove(tempPath) }()

	_, writeErr := temp.Write(payload)
	closeErr := temp.Close()

	if writeErr != nil {
		return "", fmt.Errorf("write agent-shell tool executable %s: %w", tempPath, writeErr)
	}

	if closeErr != nil {
		return "", fmt.Errorf("close agent-shell tool executable %s: %w", tempPath, closeErr)
	}

	err = os.Chmod(tempPath, info.Mode().Perm())
	if err != nil {
		return "", fmt.Errorf("chmod agent-shell tool executable %s: %w", tempPath, err)
	}

	err = os.Rename(tempPath, target)
	if err != nil {
		return "", fmt.Errorf("install agent-shell tool executable %s: %w", target, err)
	}

	return target, nil
}

func reusableAgentShellToolTarget(source, target string, sourceInfo os.FileInfo) bool {
	info, err := os.Stat(target)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return false
	}

	if info.Size() != sourceInfo.Size() || info.Mode().Perm()&0o111 == 0 {
		return false
	}

	// #nosec G304,G703 -- both paths are validated managed tool executables.
	sourcePayload, err := os.ReadFile(source)
	if err != nil {
		return false
	}

	// #nosec G304,G703 -- target is inside the trusted agent-shell tool directory.
	targetPayload, err := os.ReadFile(target)
	if err != nil {
		return false
	}

	return bytes.Equal(sourcePayload, targetPayload)
}

func trustedAgentShellToolDir() string {
	if os.Getenv("CODING_ETHOS_AGENT_SHELL_SANDBOX") != "1" {
		return ""
	}

	realGitBind := filepath.Clean(strings.TrimSpace(os.Getenv(evaluators.RealGitEnv)))
	if filepath.Base(realGitBind) != "real-git" {
		return ""
	}

	runDir := filepath.Dir(realGitBind)
	if !strings.HasPrefix(filepath.Base(runDir), "run-") {
		return ""
	}

	if !strings.HasSuffix(
		filepath.ToSlash(filepath.Dir(runDir)),
		"/.coding-ethos/cache/agent-shell",
	) {
		return ""
	}

	info, err := os.Stat(runDir)
	if err != nil || !info.IsDir() {
		return ""
	}

	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(runDir))))

	return filepath.Join(repoRoot, ".coding-ethos", "state", "agent-shell-tools")
}

func pathInsideOrSame(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}

	return rel == "." ||
		(rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
