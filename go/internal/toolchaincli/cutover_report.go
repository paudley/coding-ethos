// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func cutoverReport(args []string) error {
	flags := flag.NewFlagSet("cutover-report", flag.ExitOnError)
	action := flags.String("action", "", "Cutover action")
	status := flags.String("status", "", "Overall cutover status")
	repo := flags.String("repo", "", "Repository root")
	gitHooks := flags.String("git-hooks", "", "Git hook status")
	agentHooks := flags.String("agent-hooks", "", "Agent hook status")
	repoIgnores := flags.String("repo-ignores", "", "Repository ignore status")
	runtime := flags.String("runtime", "", "Policy runtime status")

	fixItems := flags.String("fix-items", "", "File containing TOON fix rows")

	inlineErr16 := flags.Parse(args)
	if inlineErr16 != nil {
		return fmt.Errorf("parse cutover-report flags: %w", inlineErr16)
	}

	report, err := newCutoverReport(
		*action,
		*status,
		*repo,
		map[string]string{
			"git-hooks":      *gitHooks,
			"agent-hooks":    *agentHooks,
			"repo-ignores":   *repoIgnores,
			"policy-runtime": *runtime,
		},
		*fixItems,
	)
	if err != nil {
		return err
	}

	for _, line := range cutoverReportLines(report) {
		fmt.Fprintln(os.Stdout, line)
	}

	return nil
}

type cutoverStatusReport struct {
	Action     string
	Status     string
	Repo       string
	Surfaces   map[string]string
	FixItems   []string
	HasFixFile bool
}

func newCutoverReport(
	action string,
	status string,
	repo string,
	surfaces map[string]string,
	fixItemsPath string,
) (cutoverStatusReport, error) {
	report := cutoverStatusReport{
		Action:   strings.TrimSpace(action),
		Status:   strings.TrimSpace(status),
		Repo:     strings.TrimSpace(repo),
		Surfaces: surfaces,
	}
	if report.Action == "" {
		return report, errActionRequired
	}

	if report.Status == "" {
		return report, apperror.StaticError("cutover-report requires --status")
	}

	if report.Repo == "" {
		return report, apperror.StaticError("cutover-report requires --repo")
	}

	for _, surface := range cutoverSurfaceOrder() {
		if strings.TrimSpace(report.Surfaces[surface]) == "" {
			return report, apperror.Wrapf(
				apperror.StaticError("cutover-report requires --%s"),
				"cutover-report requires --%s",
				surface,
			)
		}
	}

	if strings.TrimSpace(fixItemsPath) == "" {
		return report, nil
	}

	payload, err := os.ReadFile(fixItemsPath)
	if err != nil {
		return report, fmt.Errorf("%w: %s: %w", errFixItemsOpen, fixItemsPath, err)
	}

	report.HasFixFile = true

	for line := range strings.SplitSeq(string(payload), "\n") {
		if strings.TrimSpace(line) != "" {
			report.FixItems = append(report.FixItems, line)
		}
	}

	return report, nil
}

func cutoverReportLines(report cutoverStatusReport) []string {
	lines := []string{
		"format: toon",
		"command: cutover",
		"action: " + report.Action,
		"status: " + report.Status,
		"repo: " + report.Repo,
		"surfaces[4]{name,status}:",
	}
	for _, surface := range cutoverSurfaceOrder() {
		lines = append(lines, fmt.Sprintf("  %s,%s", surface, report.Surfaces[surface]))
	}

	if len(report.FixItems) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("fix_first[%d]{surface,problem,action}:", len(report.FixItems)),
		)
		lines = append(lines, report.FixItems...)
	}

	return lines
}

func cutoverSurfaceOrder() []string {
	return []string{"git-hooks", "agent-hooks", "repo-ignores", "policy-runtime"}
}

func installGitShimCommand(args []string) error {
	flags := flag.NewFlagSet("install-git-shim", flag.ExitOnError)
	destDir := flags.String("dest-dir", "", "Directory where the git shim is installed")
	realGit := flags.String("real-git", "", "Real git executable")

	runner := flags.String("runner", "", "runner path")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse install-git-shim flags: %w", err)
	}

	if strings.TrimSpace(*destDir) == "" {
		return errDestRequired
	}

	if strings.TrimSpace(*realGit) == "" {
		return errGitRequired
	}

	if strings.TrimSpace(*runner) == "" {
		return errHookRequired
	}

	return installGitShim(*destDir, *realGit, *runner)
}

func installGitShim(destDir, realGit, runner string) error {
	err := os.MkdirAll(destDir, directoryMode)
	if err != nil {
		return fmt.Errorf("create git shim dir %s: %w", destDir, err)
	}

	shim := filepath.Join(destDir, "git")
	payload := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"export CODING_ETHOS_REAL_GIT=" + shellQuote(realGit),
		"exec " + shellQuote(runner) + ` policy-git "$@"`,
		"",
	}, "\n")

	return writeExecutableFile(shim, []byte(payload))
}

func installGitHooks(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("install-git-hooks", args)
	if err != nil {
		return err
	}

	for _, hook := range gitHookNames() {
		err := installHookEntrypoint(hooksDir, runner, hook)
		if err != nil {
			return err
		}
	}

	for _, hook := range lfsHookNames() {
		err := installHookEntrypoint(hooksDir, runner, hook)
		if err != nil {
			return err
		}
	}

	return nil
}

func verifyGitHooks(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("verify-git-hooks", args)
	if err != nil {
		return err
	}

	stale, err := gitHookShimFixItems(hooksDir, runner)
	if err != nil {
		return err
	}

	if len(stale) > 0 {
		return apperror.StaticError("git hook entrypoints missing or stale")
	}

	return nil
}

func gitHookFixItems(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("git-hook-fix-items", args)
	if err != nil {
		return err
	}

	items, err := gitHookShimFixItems(hooksDir, runner)
	if err != nil {
		return err
	}

	for _, item := range items {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func agentHookFixItems(args []string) error {
	input, err := inputFileFlag("agent-hook-fix-items", args)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read agent hook verify output %s: %w", input, err)
	}

	for _, item := range agentHookFixItemLines(string(payload)) {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func agentHookFixItemLines(output string) []string {
	if strings.Contains(
		output,
		"settings do not contain expected hooks for all providers",
	) {
		return []string{
			"  agent-hooks,native agent settings missing or stale,run cutover install",
		}
	}

	items := make([]string, 0, agentHookFixHintCap)
	if strings.Contains(output, "Codex hooks feature") ||
		strings.Contains(output, "hooks = true") {
		items = append(
			items,
			"  agent-hooks,.codex/config.toml missing hooks=true,run cutover install",
		)
	}

	if strings.Contains(output, ".gemini/settings.json") ||
		strings.Contains(output, "Gemini") {
		items = append(
			items,
			"  agent-hooks,.gemini/settings.json missing expected hook,run cutover install",
		)
	}

	return items
}

func runtimeFixItems(args []string) error {
	input, err := inputFileFlag("runtime-fix-items", args)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read runtime verify output %s: %w", input, err)
	}

	for _, item := range runtimeFixItemLines(string(payload)) {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func runtimeFixItemLines(output string) []string {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	return []string{
		"  policy-runtime,git-hook validate failed,inspect policy runtime validation output",
	}
}

func repoIgnoreFixItems(args []string) error {
	flags := flag.NewFlagSet("repo-ignore-fix-items", flag.ExitOnError)
	repoRoot := flags.String("repo-root", "", "Repository root")

	realGit := flags.String("real-git", "", "Real git executable")

	inlineErr17 := flags.Parse(args)
	if inlineErr17 != nil {
		return fmt.Errorf("parse repo-ignore-fix-items flags: %w", inlineErr17)
	}

	if strings.TrimSpace(*repoRoot) == "" {
		return errRepoRootRequired
	}

	if strings.TrimSpace(*realGit) == "" {
		return errRealGitRequired
	}

	items, err := repoIgnoreFixItemLines(*realGit, *repoRoot)
	if err != nil {
		return err
	}

	for _, item := range items {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func repoIgnoreFixItemLines(realGit, repoRoot string) ([]string, error) {
	requiredIgnores := []string{
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	}

	items := make([]string, 0, len(requiredIgnores))
	for _, requiredIgnore := range requiredIgnores {
		ignored, err := gitCheckIgnore(realGit, repoRoot, requiredIgnore)
		if err != nil {
			return nil, err
		}

		if !ignored {
			items = append(
				items,
				fmt.Sprintf(
					"  repo-ignores,%s is not ignored,add .coding-ethos/ to .gitignore",
					requiredIgnore,
				),
			)
		}
	}

	return items, nil
}

func gitCheckIgnore(realGit, repoRoot, path string) (bool, error) {
	command := realgit.CommandFor(
		context.Background(),
		realGit,
		false,
		"-C",
		repoRoot,
		"check-ignore",
		"--quiet",
		path,
	)

	err := command.Run()
	if err == nil {
		return true, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("git check-ignore %s: %w", path, err)
}

func inputFileFlag(command string, args []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)

	input := flags.String("input", "", "Input file to parse")

	err := flags.Parse(args)
	if err != nil {
		return "", fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*input) == "" {
		return "", errInputRequired
	}

	return *input, nil
}

func gitHookShimFlags(command string, args []string) (string, string, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	hooksDir := flags.String("hooks-dir", "", "Git hooks directory")
	runner := flags.String("runner", "", "coding-ethos-run executable")
	flags.String("source-dir", "", "Deprecated; hook files are generated from --runner")

	err := flags.Parse(args)
	if err != nil {
		return "", "", fmt.Errorf("parse %s flags: %w", command, err)
	}

	if strings.TrimSpace(*hooksDir) == "" {
		return "", "", errHooksRequired
	}

	if strings.TrimSpace(*runner) == "" {
		return "", "", errHookRequired
	}

	return *hooksDir, *runner, nil
}

func installHookEntrypoint(hooksDir, runner, hookName string) error {
	target := filepath.Join(hooksDir, hookName)

	err := os.MkdirAll(hooksDir, directoryMode)
	if err != nil {
		return fmt.Errorf("create hooks dir %s: %w", hooksDir, err)
	}

	err = os.Remove(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove existing hook %s: %w", target, err)
	}

	command := gitHookCommand
	if slices.Contains(lfsHookNames(), hookName) {
		command = lfsHookCommand
	}

	payload := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"exec " + shellQuote(
			runner,
		) + " " + command + " " + shellQuote(
			hookName,
		) + ` "$@"`,
		"",
	}, "\n")

	return writeExecutableFile(target, []byte(payload))
}

func gitHookShimFixItems(hooksDir, runner string) ([]string, error) {
	items := make([]string, 0)

	for _, hook := range gitHookNames() {
		item, err := hookShimFixItem(hooksDir, runner, hook)
		if err != nil {
			return nil, err
		}

		if item != "" {
			items = append(items, item)
		}
	}

	for _, hook := range lfsHookNames() {
		item, err := hookShimFixItem(hooksDir, runner, hook)
		if err != nil {
			return nil, err
		}

		if item != "" {
			items = append(items, item)
		}
	}

	return items, nil
}

func hookShimFixItem(
	hooksDir string,
	runner string,
	hookName string,
) (string, error) {
	target := filepath.Join(hooksDir, hookName)

	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf(
				"  git-hooks,%s missing or not executable,run cutover install",
				target,
			), nil
		}

		return "", fmt.Errorf("stat hook shim %s: %w", target, err)
	}

	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Sprintf(
			"  git-hooks,%s missing or not executable,run cutover install",
			target,
		), nil
	}

	matches, err := hookEntrypointTargetsRunner(target, runner, hookName)
	if err != nil {
		return "", err
	}

	if !matches {
		return fmt.Sprintf(
			"  git-hooks,%s does not route to coding-ethos-run,run cutover install",
			target,
		), nil
	}

	return "", nil
}

func hookEntrypointTargetsRunner(target, runner, hookName string) (bool, error) {
	payload, err := os.ReadFile(target)
	if err != nil {
		return false, fmt.Errorf("read hook entrypoint %s: %w", target, err)
	}

	command := "git-hook"
	if slices.Contains(lfsHookNames(), hookName) {
		command = "lfs-hook"
	}

	want := "exec " + shellQuote(
		runner,
	) + " " + command + " " + shellQuote(
		hookName,
	) + ` "$@"`

	return strings.Contains(string(payload), want), nil
}

func filesEqual(left, right string) (bool, error) {
	leftPayload, err := os.ReadFile(left)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", left, err)
	}

	rightPayload, err := os.ReadFile(right)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", right, err)
	}

	if len(leftPayload) != len(rightPayload) {
		return false, nil
	}

	return subtle.ConstantTimeCompare(leftPayload, rightPayload) == 1, nil
}

func writeExecutableFile(path string, payload []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}

	tmpPath := tmp.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	_, inlineErrD := tmp.Write(payload)
	if inlineErrD != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temporary file for %s: %w", path, inlineErrD)
	}

	inlineErr18 := tmp.Close()
	if inlineErr18 != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, inlineErr18)
	}

	inlineErr19 := os.Chmod(tmpPath, executableFileMode)
	if inlineErr19 != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, inlineErr19)
	}

	inlineErr20 := os.Rename(tmpPath, path)
	if inlineErr20 != nil {
		return fmt.Errorf("install %s: %w", path, inlineErr20)
	}

	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
