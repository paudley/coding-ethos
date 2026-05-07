// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const ciArtifactDirMode = 0o755

func runCISARIF(paths runtimePaths, args []string) error {
	provider, err := parseCISARIFProvider(args)
	if err != nil {
		return err
	}

	repoRoot := envOrDefault("CODING_ETHOS_REPO_ROOT", paths.Root)

	sarifPath := strings.TrimSpace(os.Getenv("CODING_ETHOS_SARIF_PATH"))
	if sarifPath == "" {
		return apperror.StaticError("CODING_ETHOS_SARIF_PATH is required")
	}

	files, err := ciChangedFiles(paths.RealGit, repoRoot, provider)
	if err != nil {
		return err
	}

	filesPath, err := writeCIFileList(repoRoot, files)
	if err != nil {
		return err
	}

	defer func() {
		removeErr := os.Remove(filesPath)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			fmt.Fprintf(os.Stderr, "WARN: remove temporary CI file list: %v\n", removeErr)
		}
	}()

	output, err := createCISARIFOutput(repoRoot, sarifPath)
	if err != nil {
		return err
	}
	defer output.close()

	command := safeexec.Command(
		filepath.Join(paths.BinDir, "coding-ethos-lint"),
		ciSARIFLintArgs(paths, repoRoot, filesPath)...,
	)
	command.Stdout = output.file
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	runErr := command.Run()

	closeErr := output.file.Close()
	if closeErr != nil {
		return fmt.Errorf("close temporary SARIF %s: %w", output.tmpRel, closeErr)
	}

	output.file = nil

	err = installCISARIFOutput(output, sarifPath)
	if err != nil {
		return err
	}

	if runErr != nil {
		return fmt.Errorf("run ci-sarif hook: %w", runErr)
	}

	return nil
}

func installCISARIFOutput(output *ciSARIFOutput, sarifPath string) error {
	info, statErr := output.root.Stat(output.tmpRel)
	if statErr == nil && info.Size() > 0 {
		err := output.root.Rename(output.tmpRel, output.rel)
		if err != nil {
			return fmt.Errorf("install SARIF %s: %w", sarifPath, err)
		}

		return nil
	}

	err := removeRootFileIfExists(output.root, output.tmpRel)
	if err != nil {
		return fmt.Errorf("remove empty temporary SARIF %s: %w", output.tmpRel, err)
	}

	return nil
}

func parseCISARIFProvider(args []string) (string, error) {
	flags := flag.NewFlagSet("ci-sarif", flag.ExitOnError)
	provider := flags.String("provider", "", "CI provider: github or gitlab")

	err := flags.Parse(args)
	if err != nil {
		return "", fmt.Errorf("parse ci-sarif flags: %w", err)
	}

	value := strings.TrimSpace(*provider)
	if value == "" {
		return "", apperror.StaticError("ci-sarif requires --provider")
	}

	return value, nil
}

type ciSARIFOutput struct {
	file   *os.File
	root   *os.Root
	rel    string
	tmpRel string
}

func (output *ciSARIFOutput) close() {
	if output.file != nil {
		_ = output.file.Close()
	}

	if output.root != nil {
		_ = output.root.Close()
	}
}

func createCISARIFOutput(repoRoot, sarifPath string) (*ciSARIFOutput, error) {
	root, rel, err := rootedCIPath(repoRoot, sarifPath)
	if err != nil {
		return nil, err
	}

	sarifDir := filepath.Dir(rel)

	err = root.MkdirAll(sarifDir, ciArtifactDirMode)
	if err != nil && sarifDir != "." {
		_ = root.Close()

		return nil, fmt.Errorf("create SARIF directory: %w", err)
	}

	tmpRel := rel + ".tmp"

	err = removeRootFileIfExists(root, tmpRel)
	if err != nil {
		_ = root.Close()

		return nil, fmt.Errorf("remove stale temporary SARIF %s: %w", tmpRel, err)
	}

	err = removeRootFileIfExists(root, rel)
	if err != nil {
		_ = root.Close()

		return nil, fmt.Errorf("remove stale SARIF %s: %w", rel, err)
	}

	file, err := root.Create(tmpRel)
	if err != nil {
		_ = root.Close()

		return nil, fmt.Errorf("create temporary SARIF %s: %w", tmpRel, err)
	}

	return &ciSARIFOutput{file: file, root: root, rel: rel, tmpRel: tmpRel}, nil
}

func rootedCIPath(repoRoot, targetPath string) (*os.Root, string, error) {
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, "", fmt.Errorf("resolve repo root: %w", err)
	}

	targetAbs := targetPath
	if !filepath.IsAbs(targetAbs) {
		targetAbs = filepath.Join(repoAbs, targetPath)
	}

	rel, err := filepath.Rel(repoAbs, targetAbs)
	if err != nil {
		return nil, "", fmt.Errorf("resolve SARIF path: %w", err)
	}

	if !filepath.IsLocal(rel) {
		return nil, "", apperror.Wrapf(
			apperror.StaticError("SARIF path escapes repo root"),
			"SARIF path escapes repo root: %s",
			targetPath,
		)
	}

	root, err := os.OpenRoot(repoAbs)
	if err != nil {
		return nil, "", fmt.Errorf("open repo root: %w", err)
	}

	return root, rel, nil
}

func removeRootFileIfExists(root *os.Root, path string) error {
	err := root.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}

	return fmt.Errorf("remove root file %s: %w", path, err)
}

func ciSARIFLintArgs(paths runtimePaths, repoRoot, filesPath string) []string {
	lintArgs := []string{
		"--bundle", paths.PolicyBundle,
		"--cwd", repoRoot,
		"--scope", "files",
		"--files-from", filesPath,
		"--sarif",
	}
	lintArgs = appendCISARIFOption(
		lintArgs,
		"CODING_ETHOS_SANDBOX_MODE",
		"--sandbox-mode",
	)

	return appendCISARIFOption(
		lintArgs,
		"CODING_ETHOS_SARIF_CATEGORY",
		"--sarif-category",
	)
}

func appendCISARIFOption(args []string, envName, flagName string) []string {
	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return args
	}

	return append(args[:len(args)-1], flagName, value, "--sarif")
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
		return nil, apperror.Wrapf(
			apperror.StaticError("unknown CI provider %q"),
			"unknown CI provider %q",
			provider,
		)
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
		_, inlineErrA := fmt.Fprintln(file, path)
		if inlineErrA != nil {
			_ = file.Close()

			return "", fmt.Errorf("write CI file list: %w", inlineErrA)
		}
	}

	inlineErr1 := file.Close()
	if inlineErr1 != nil {
		return "", fmt.Errorf("close CI file list: %w", inlineErr1)
	}

	return file.Name(), nil
}

func gitRefExists(realGit, repoRoot, ref string) bool {
	command := safeexec.Command(realGit, "-C", repoRoot, "rev-parse", "--verify", ref)

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
