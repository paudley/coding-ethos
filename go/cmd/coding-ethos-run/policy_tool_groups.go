// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

const (
	pythonExtension         = ".py"
	goExtension             = ".go"
	ruffAutofixBaseArgCount = 6
)

type policyToolGroupEntry struct {
	Tool string
	Args []string
}

func runPolicyToolGroup(paths runtimePaths, rest []string) error {
	if len(rest) == 0 {
		return apperror.StaticError("policy-tool-group requires a group name")
	}

	group, found := policyToolGroup(rest[0])
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("unknown policy-tool group"),
			"unknown policy-tool group %q",
			rest[0],
		)
	}

	requirePolicyBundle(paths)

	exitCode := runPolicyToolGroupEntries(paths, group, false)
	if exitCode != 0 {
		requestRuntimeExit(exitCode)
	}

	return nil
}

func runScopedPolicyToolGroupByName(
	paths runtimePaths,
	name string,
	codeIntel bool,
	scope managedLintScope,
) int {
	group, found := policyToolGroup(name)
	if !found {
		return 1
	}

	return runPolicyToolGroupEntries(paths, scopedPolicyToolGroup(group, scope), codeIntel)
}

func runPolicyToolGroupEntries(
	paths runtimePaths,
	group []policyToolGroupEntry,
	codeIntel bool,
) int {
	exitCode := 0

	for _, entry := range group {
		args := policyToolLintArgs(paths, entry.Tool, entry.Args)
		if codeIntel {
			args = append([]string{"--code-intel"}, args...)
		}

		code := runtimeRunLint(paths, args...)

		if code != 0 && exitCode == 0 {
			exitCode = code
		}
	}

	return exitCode
}

type managedLintScope struct {
	Files  []string
	Scoped bool
}

type managedLintScopeArgs struct {
	scope          string
	files          []string
	explicitScoped bool
}

func managedLintScopeFromArgs(
	paths runtimePaths,
	rest []string,
) (managedLintScope, error) {
	parsed, err := parseManagedLintScopeArgs(rest)
	if err != nil {
		return managedLintScope{}, err
	}

	files := parsed.files
	if len(files) == 0 {
		resolved, err := managedLintScopeGitFiles(paths, parsed.scope)
		if err != nil {
			return managedLintScope{}, err
		}

		files = resolved
	}

	return managedLintScope{
		Files:  cleanManagedLintFiles(files),
		Scoped: parsed.explicitScoped && managedLintScopeCanConstrain(parsed.scope),
	}, nil
}

func parseManagedLintScopeArgs(rest []string) (managedLintScopeArgs, error) {
	parsed := managedLintScopeArgs{scope: lint.ScopeFiles}

	for index := 0; index < len(rest); index++ {
		next, err := parseManagedLintScopeArg(rest, index, &parsed)
		if err != nil {
			return managedLintScopeArgs{}, err
		}

		index = next
	}

	return parsed, nil
}

func parseManagedLintScopeArg(
	rest []string,
	index int,
	parsed *managedLintScopeArgs,
) (int, error) {
	arg := rest[index]

	next, handled := parseManagedLintScopeSelector(rest, index, arg, parsed)
	if handled {
		return next, nil
	}

	return parseManagedLintScopeFileArg(rest, index, arg, parsed)
}

func parseManagedLintScopeSelector(
	rest []string,
	index int,
	arg string,
	parsed *managedLintScopeArgs,
) (int, bool) {
	switch {
	case arg == "--scope" && index+1 < len(rest):
		parsed.scope = rest[index+1]
		parsed.explicitScoped = true

		return index + 1, true
	case strings.HasPrefix(arg, "--scope="):
		parsed.scope = strings.TrimPrefix(arg, "--scope=")
		parsed.explicitScoped = true
	case arg == "--staged":
		parsed.scope = lint.ScopeStaged
		parsed.explicitScoped = true
	case arg == "--changed":
		parsed.scope = lint.ScopeChanged
		parsed.explicitScoped = true
	case arg == "--full":
		parsed.scope = lint.ScopeFull
	default:
		return index, false
	}

	return index, true
}

func parseManagedLintScopeFileArg(
	rest []string,
	index int,
	arg string,
	parsed *managedLintScopeArgs,
) (int, error) {
	switch {
	case arg == "--files" && index+1 < len(rest):
		parsed.files = append(parsed.files, parseManagedLintFiles(rest[index+1])...)
		parsed.explicitScoped = true

		return index + 1, nil
	case strings.HasPrefix(arg, "--files="):
		parsed.files = append(
			parsed.files,
			parseManagedLintFiles(strings.TrimPrefix(arg, "--files="))...,
		)
		parsed.explicitScoped = true
	case arg == "--files-from" && index+1 < len(rest):
		listed, err := readManagedLintFileList(rest[index+1])
		if err != nil {
			return index, err
		}

		parsed.files = append(parsed.files, listed...)
		parsed.explicitScoped = true

		return index + 1, nil
	case strings.HasPrefix(arg, "--files-from="):
		listed, err := readManagedLintFileList(
			strings.TrimPrefix(arg, "--files-from="),
		)
		if err != nil {
			return index, err
		}

		parsed.files = append(parsed.files, listed...)
		parsed.explicitScoped = true
	}

	return index, nil
}

func managedLintScopeCanConstrain(scope string) bool {
	return scope == lint.ScopeFiles ||
		scope == lint.ScopeStaged ||
		scope == lint.ScopeChanged
}

func managedLintScopeGitFiles(paths runtimePaths, scope string) ([]string, error) {
	switch scope {
	case lint.ScopeStaged:
		return managedLintGitFiles(
			paths,
			"diff",
			"--cached",
			"--name-only",
			"--diff-filter=ACMR",
			"--",
		)
	case lint.ScopeChanged:
		return managedLintGitFiles(
			paths,
			"diff",
			"--name-only",
			"--diff-filter=ACMR",
			"--",
		)
	default:
		return nil, nil
	}
}

func managedLintGitFiles(paths runtimePaths, args ...string) ([]string, error) {
	output, err := gitOutput(paths.RealGit, paths.Root, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve managed lint files: %w", err)
	}

	return parseManagedLintFileList(output), nil
}

func readManagedLintFileList(path string) ([]string, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read lint file list %s: %w", path, err)
	}

	return parseManagedLintFileList(string(payload)), nil
}

func parseManagedLintFiles(raw string) []string {
	return parseManagedLintFileList(strings.ReplaceAll(raw, ",", "\n"))
}

func parseManagedLintFileList(raw string) []string {
	lines := strings.Split(raw, "\n")
	files := make([]string, 0, len(lines))

	for _, line := range lines {
		file := strings.TrimSpace(line)
		if file != "" {
			files = append(files, file)
		}
	}

	return files
}

func cleanManagedLintFiles(files []string) []string {
	cleaned := make([]string, 0, len(files))
	seen := map[string]struct{}{}

	for _, file := range files {
		normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
		if normalized == "." || normalized == "" {
			continue
		}

		if _, found := seen[normalized]; found {
			continue
		}

		seen[normalized] = struct{}{}
		cleaned = append(cleaned, normalized)
	}

	return cleaned
}

func scopedPolicyToolGroup(
	group []policyToolGroupEntry,
	scope managedLintScope,
) []policyToolGroupEntry {
	if !scope.Scoped {
		return group
	}

	pythonFiles := filesWithExtension(scope.Files, pythonExtension)
	goFiles := filesWithExtension(scope.Files, goExtension)
	scoped := make([]policyToolGroupEntry, 0, len(group))

	for _, entry := range group {
		switch entry.Tool {
		case "ruff-format":
			if len(pythonFiles) > 0 {
				scoped = append(scoped, policyToolGroupEntry{
					Tool: entry.Tool,
					Args: append([]string{"format"}, pythonFiles...),
				})
			}
		case "ruff-autofix":
			if len(pythonFiles) > 0 {
				args := make(
					[]string,
					0,
					ruffAutofixBaseArgCount+len(pythonFiles),
				)
				args = append(args,
					"check",
					"--fix",
					"--quiet",
					"--ignore-noqa",
					"--output-format",
					"json",
				)
				args = append(args, pythonFiles...)
				scoped = append(scoped, policyToolGroupEntry{
					Tool: entry.Tool,
					Args: args,
				})
			}
		case "golangci-lint-format", "golangci-lint-autofix":
			if len(goFiles) > 0 {
				scoped = append(scoped, policyToolGroupEntry{
					Tool: entry.Tool,
					Args: append([]string(nil), goFiles...),
				})
			}
		default:
			scoped = append(scoped, entry)
		}
	}

	return scoped
}

func filesWithExtension(files []string, extension string) []string {
	filtered := make([]string, 0, len(files))

	for _, file := range files {
		if filepath.Ext(file) == extension {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

func policyToolGroup(name string) ([]policyToolGroupEntry, bool) {
	switch name {
	case "linters":
		return []policyToolGroupEntry{
			{Tool: "ruff", Args: []string{"check", "coding_ethos", "tests"}},
			{Tool: "golangci-lint"},
		}, true
	case "formatters":
		return []policyToolGroupEntry{
			{Tool: "ruff-format", Args: []string{"format", "coding_ethos", "tests"}},
			{Tool: "golangci-lint-format"},
		}, true
	case "autofixers":
		return []policyToolGroupEntry{
			{
				Tool: "ruff-autofix",
				Args: []string{
					"check",
					"--fix",
					"--quiet",
					"--ignore-noqa",
					"--output-format",
					"json",
					"coding_ethos",
					"tests",
				},
			},
			{Tool: "golangci-lint-autofix"},
		}, true
	default:
		return nil, false
	}
}
