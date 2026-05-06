// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runCISARIF(paths runtimePaths, args []string) error {
	flags := flag.NewFlagSet("ci-sarif", flag.ExitOnError)

	provider := flags.String("provider", "", "CI provider: github or gitlab")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ci-sarif flags: %w", err)
	}

	if strings.TrimSpace(*provider) == "" {
		return errors.New("ci-sarif requires --provider")
	}

	repoRoot := envOrDefault("CODING_ETHOS_REPO_ROOT", paths.Root)

	sarifPath := strings.TrimSpace(os.Getenv("CODING_ETHOS_SARIF_PATH"))
	if sarifPath == "" {
		return errors.New("CODING_ETHOS_SARIF_PATH is required")
	}

	files, err := ciChangedFiles(paths.RealGit, repoRoot, *provider)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(
		filepath.Dir(sarifPath),
		0o755,
	); err != nil &&
		filepath.Dir(sarifPath) != "." {
		return fmt.Errorf("create SARIF directory: %w", err)
	}

	filesPath, err := writeCIFileList(repoRoot, files)
	if err != nil {
		return err
	}

	defer func() {
		_ = os.Remove(filesPath)
	}()

	tmpPath := sarifPath + ".tmp"
	_ = os.Remove(tmpPath)
	_ = os.Remove(sarifPath)

	output, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temporary SARIF %s: %w", tmpPath, err)
	}

	lintArgs := []string{
		"--bundle", paths.PolicyBundle,
		"--cwd", repoRoot,
		"--scope", "files",
		"--files-from", filesPath,
		"--sarif",
	}
	if sandboxMode := strings.TrimSpace(
		os.Getenv("CODING_ETHOS_SANDBOX_MODE"),
	); sandboxMode != "" {
		lintArgs = append(
			lintArgs[:len(lintArgs)-1],
			"--sandbox-mode",
			sandboxMode,
			"--sarif",
		)
	}

	if category := strings.TrimSpace(
		os.Getenv("CODING_ETHOS_SARIF_CATEGORY"),
	); category != "" {
		lintArgs = append(lintArgs[:len(lintArgs)-1], "--sarif-category", category, "--sarif")
	}

	command := exec.Command(filepath.Join(paths.BinDir, "coding-ethos-lint"), lintArgs...)
	command.Stdout = output
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	runErr := command.Run()

	closeErr := output.Close()
	if closeErr != nil {
		return fmt.Errorf("close temporary SARIF %s: %w", tmpPath, closeErr)
	}

	if info, statErr := os.Stat(tmpPath); statErr == nil && info.Size() > 0 {
		err := os.Rename(tmpPath, sarifPath)
		if err != nil {
			return fmt.Errorf("install SARIF %s: %w", sarifPath, err)
		}
	} else {
		_ = os.Remove(tmpPath)
	}

	if runErr != nil {
		return runErr
	}

	return nil
}

func ciChangedFiles(realGit, repoRoot, provider string) ([]string, error) {
	if explicit := strings.TrimSpace(os.Getenv("CODING_ETHOS_FILES")); explicit != "" {
		return filterExistingFiles(repoRoot, splitCIFileList(explicit)), nil
	}

	switch provider {
	case "github":
		return githubChangedFiles(realGit, repoRoot)
	case "gitlab":
		return gitlabChangedFiles(realGit, repoRoot)
	default:
		return nil, fmt.Errorf("unknown CI provider %q", provider)
	}
}

func githubChangedFiles(realGit, repoRoot string) ([]string, error) {
	baseRef := strings.TrimSpace(os.Getenv("CODING_ETHOS_GITHUB_BASE_REF"))
	if baseRef != "" && gitRefExists(realGit, repoRoot, "origin/"+baseRef) {
		return diffNameOnly(realGit, repoRoot, "origin/"+baseRef+"...HEAD")
	}

	before := strings.TrimSpace(os.Getenv("CODING_ETHOS_GITHUB_EVENT_BEFORE"))

	sha := strings.TrimSpace(os.Getenv("CODING_ETHOS_GITHUB_SHA"))
	if strings.TrimSpace(os.Getenv("CODING_ETHOS_GITHUB_EVENT_NAME")) == "push" &&
		before != "" &&
		sha != "" &&
		!isZeroGitSHA(before) &&
		gitRefExists(realGit, repoRoot, before+"^{commit}") {
		return diffNameOnly(realGit, repoRoot, before, sha)
	}

	return nil, nil
}

func gitlabChangedFiles(realGit, repoRoot string) ([]string, error) {
	target := strings.TrimSpace(os.Getenv("CI_MERGE_REQUEST_TARGET_BRANCH_SHA"))
	if target != "" {
		return diffNameOnly(realGit, repoRoot, target+"...HEAD")
	}

	before := strings.TrimSpace(os.Getenv("CI_COMMIT_BEFORE_SHA"))

	sha := strings.TrimSpace(os.Getenv("CI_COMMIT_SHA"))
	if before != "" &&
		sha != "" &&
		!isZeroGitSHA(before) &&
		gitRefExists(realGit, repoRoot, before+"^{commit}") {
		return diffNameOnly(realGit, repoRoot, before, sha)
	}

	return nil, nil
}

func diffNameOnly(realGit, repoRoot string, refs ...string) ([]string, error) {
	args := append([]string{"diff", "--name-only"}, refs...)

	output, err := gitOutput(realGit, repoRoot, args...)
	if err != nil {
		return nil, err
	}

	return filterExistingFiles(repoRoot, splitCIFileList(output)), nil
}

func splitCIFileList(value string) []string {
	normalized := strings.NewReplacer(",", "\n", "\r\n", "\n").Replace(value)
	parts := strings.Split(normalized, "\n")

	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			files = append(files, trimmed)
		}
	}

	return files
}

func filterExistingFiles(repoRoot string, files []string) []string {
	filtered := make([]string, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(filepath.Join(repoRoot, file))
		if err == nil && !info.IsDir() {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

func writeCIFileList(repoRoot string, files []string) (string, error) {
	file, err := os.CreateTemp(repoRoot, "coding-ethos-ci-files-*.txt")
	if err != nil {
		return "", fmt.Errorf("create CI file list: %w", err)
	}

	for _, path := range files {
		if _, err := fmt.Fprintln(file, path); err != nil {
			_ = file.Close()

			return "", fmt.Errorf("write CI file list: %w", err)
		}
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close CI file list: %w", err)
	}

	return file.Name(), nil
}

func gitRefExists(realGit, repoRoot, ref string) bool {
	command := exec.Command(realGit, "-C", repoRoot, "rev-parse", "--verify", ref)

	return command.Run() == nil
}

func isZeroGitSHA(value string) bool {
	return value == "0000000000000000000000000000000000000000"
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}

	return fallback
}
