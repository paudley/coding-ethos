// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func loadModuleDocsSettings() (moduleDocsSettings, error) {
	var settings moduleDocsSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}

	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.module_docs",
		"module_docs",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	if !sectionFound {
		return settings, nil
	}

	if strings.TrimSpace(settings.SourceDocsPath) == "" {
		settings.SourceDocsPath = "docs/SOURCE_DOCS.md"
	}

	if len(settings.CheckFilenames) == 0 {
		settings.CheckFilenames = []string{"__init__.py", "conftest.py"}
	}

	if len(settings.ExcludedDirs) == 0 {
		settings.ExcludedDirs = defaultModuleDocsExcludedDirs()
	}

	settings.ExcludedDirs = appendRequiredModuleDocsExcludedDirs(settings.ExcludedDirs)

	if len(settings.BannedDocFilenames) == 0 {
		settings.BannedDocFilenames = []string{"README.md", "readme.md"}
	}

	return settings, nil
}

func defaultModuleDocsExcludedDirs() []string {
	return []string{
		".venv",
		".lint-cache",
		".mypy_cache",
		".ruff_cache",
		"__pycache__",
		"node_modules",
		".git",
	}
}

func requiredModuleDocsExcludedDirs() []string {
	return []string{
		".git",
		".coding-ethos",
	}
}

func appendRequiredModuleDocsExcludedDirs(excludedDirs []string) []string {
	seen := stringSet(excludedDirs)
	merged := append([]string(nil), excludedDirs...)

	for _, dir := range requiredModuleDocsExcludedDirs() {
		if !seen[dir] {
			merged = append(merged, dir)
			seen[dir] = true
		}
	}

	return merged
}

func shouldCheckModuleDocsFile(path string, settings moduleDocsSettings) bool {
	checkNames := stringSet(settings.CheckFilenames)
	if !checkNames[filepath.Base(path)] {
		return false
	}

	excluded := stringSet(settings.ExcludedDirs)
	for part := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		if excluded[part] {
			return false
		}
	}

	return true
}

func discoverModuleDocsFiles(
	root string,
	settings moduleDocsSettings,
) ([]string, error) {
	matches := make([]string, 0)
	excluded := stringSet(settings.ExcludedDirs)

	err := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				if excluded[entry.Name()] && path != root {
					return filepath.SkipDir
				}

				return nil
			}

			if shouldCheckModuleDocsFile(path, settings) {
				matches = append(matches, filepath.ToSlash(path))
			}

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("walk module docs files: %w", err)
	}

	sort.Strings(matches)

	return matches, nil
}

func listColocatedMarkdownFiles(path string) ([]string, error) {
	directory := filepath.Dir(path)

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", directory, err)
	}

	files := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(
				files,
				filepath.ToSlash(filepath.Join(directory, entry.Name())),
			)
		}
	}

	sort.Strings(files)

	return files, nil
}

func extractModuleDocstringFromFile(path string) (string, error) {
	text, binary, err := readText(path)
	if err != nil {
		return "", err
	}

	if binary {
		return "", nil
	}

	return extractModuleDocstring(text)
}

func extractModuleDocstring(text string) (string, error) {
	text = strings.TrimPrefix(text, "\ufeff")

	index := 0
	for index < len(text) {
		for index < len(text) {
			switch text[index] {
			case ' ', '\t', '\r', '\n':
				index++
			default:
				goto afterWhitespace
			}
		}

	afterWhitespace:
		if index >= len(text) {
			return "", nil
		}

		if text[index] == '#' {
			for index < len(text) && text[index] != '\n' {
				index++
			}

			continue
		}

		return parseModuleDocstringLiteral(text, index)
	}

	return "", nil
}

func parseModuleDocstringLiteral(text string, start int) (string, error) {
	index := start
	for index < len(text) && isASCIIAlpha(text[index]) {
		index++
	}

	if !isModuleDocstringPrefix(text[start:index]) {
		return "", nil
	}

	if index >= len(text) {
		return "", nil
	}

	quote := text[index]
	if quote != '\'' && quote != '"' {
		return "", nil
	}

	triple := index+minCollectionItems < len(text) &&
		text[index+1] == quote &&
		text[index+2] == quote
	if triple {
		return parseTripleQuotedDocstring(text, index, quote)
	}

	return parseSingleQuotedDocstring(text, index, quote)
}

func isASCIIAlpha(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isModuleDocstringPrefix(prefix string) bool {
	for _, char := range strings.ToLower(prefix) {
		if char != 'r' && char != 'u' {
			return false
		}
	}

	return true
}

func parseTripleQuotedDocstring(
	text string,
	index int,
	quote byte,
) (string, error) {
	contentStart := index + tripleQuoteLen
	for cursor := contentStart; cursor+2 < len(text); cursor++ {
		if text[cursor] == '\\' {
			cursor++

			continue
		}

		if text[cursor] == quote &&
			text[cursor+1] == quote &&
			text[cursor+2] == quote {
			return text[contentStart:cursor], nil
		}
	}

	return "", errUnterminatedTripleDoc
}

func parseSingleQuotedDocstring(
	text string,
	index int,
	quote byte,
) (string, error) {
	contentStart := index + 1
	for cursor := contentStart; cursor < len(text); cursor++ {
		switch text[cursor] {
		case '\\':
			cursor++
		case '\n':
			return "", errUnterminatedModuleDoc
		case quote:
			return text[contentStart:cursor], nil
		}
	}

	return "", errUnterminatedModuleDoc
}

func hasMeaningfulModuleDocstring(docstring string) bool {
	return strings.TrimSpace(docstring) != ""
}

func extractModuleSeeAlsoContent(docstring string) string {
	location := moduleDocsSeeAlsoPattern.FindStringIndex(docstring)
	if location == nil {
		return ""
	}

	return docstring[location[1]:]
}

func extractModuleSeeAlsoReferences(docstring string) []string {
	content := extractModuleSeeAlsoContent(docstring)
	if content == "" {
		return nil
	}

	refs := make(map[string]struct{})

	for _, match := range moduleDocsEntryPattern.FindAllStringSubmatch(content, -1) {
		if len(match) >= minCollectionItems {
			refs[match[1]] = struct{}{}
		}
	}

	return sortedKeys(refs)
}

func extractModulePathPrefixedReferences(docstring string) []string {
	content := extractModuleSeeAlsoContent(docstring)
	if content == "" {
		return nil
	}

	refs := make(map[string]struct{})

	for _, match := range moduleDocsPathPattern.FindAllStringSubmatch(content, -1) {
		if len(match) >= minCollectionItems {
			refs[match[1]] = struct{}{}
		}
	}

	return sortedKeys(refs)
}

func missingModuleDocstringReferences(
	docstring string,
	markdownFiles []string,
) []string {
	if docstring == "" {
		return markdownFiles
	}

	referenced := stringSet(extractModuleSeeAlsoReferences(docstring))
	missing := make([]string, 0)

	for _, markdownFile := range markdownFiles {
		if !referenced[filepath.Base(markdownFile)] {
			missing = append(missing, markdownFile)
		}
	}

	return missing
}

func nonexistentModuleReferences(path, docstring string) []string {
	directory := filepath.Dir(path)
	missing := make([]string, 0)

	for _, ref := range extractModuleSeeAlsoReferences(docstring) {
		refPath := filepath.Join(directory, ref)

		_, err := os.Stat(refPath)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, ref)
		}
	}

	sort.Strings(missing)

	return missing
}

func loadModuleDocsIndex(settings moduleDocsSettings) (string, error) {
	if strings.TrimSpace(settings.SourceDocsPath) == "" {
		return "", nil
	}

	data, err := os.ReadFile(settings.SourceDocsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("read %s: %w", settings.SourceDocsPath, err)
	}

	return string(data), nil
}

func missingSourceDocsEntries(markdownFiles []string, index string) []string {
	if strings.TrimSpace(index) == "" {
		return append([]string{}, markdownFiles...)
	}

	missing := make([]string, 0)

	for _, markdownFile := range markdownFiles {
		directory := strings.TrimPrefix(
			filepath.ToSlash(filepath.Dir(markdownFile))+"/",
			"./",
		)

		name := filepath.Base(markdownFile)
		if !strings.Contains(index, directory) || !strings.Contains(index, name) {
			missing = append(missing, markdownFile)
		}
	}

	return missing
}

func bannedModuleDocFilenames(
	markdownFiles []string,
	settings moduleDocsSettings,
) []string {
	banned := stringSet(settings.BannedDocFilenames)
	violations := make([]string, 0)

	for _, markdownFile := range markdownFiles {
		if banned[filepath.Base(markdownFile)] {
			violations = append(violations, markdownFile)
		}
	}

	sort.Strings(violations)

	return violations
}

func collectModuleDocsViolations(
	files []string,
	settings moduleDocsSettings,
) (moduleDocsViolations, error) {
	violations := moduleDocsViolations{}
	allMarkdown := make(map[string]struct{})

	for _, path := range files {
		err := collectModuleDocsFileViolations(path, settings, &violations, allMarkdown)
		if err != nil {
			return violations, fmt.Errorf("%s: %w", path, err)
		}
	}

	allMarkdownFiles := sortedKeys(allMarkdown)

	index, err := loadModuleDocsIndex(settings)
	if err != nil {
		return violations, err
	}

	violations.MissingIndex = missingSourceDocsEntries(allMarkdownFiles, index)
	violations.BannedFilenames = bannedModuleDocFilenames(allMarkdownFiles, settings)
	sort.Strings(violations.MissingDocstring)
	sort.Strings(violations.MissingMarkdown)
	sort.Strings(violations.MissingIndex)

	return violations, nil
}

func collectModuleDocsFileViolations(
	path string,
	settings moduleDocsSettings,
	violations *moduleDocsViolations,
	allMarkdown map[string]struct{},
) error {
	if !shouldCheckModuleDocsFile(path, settings) {
		return nil
	}

	docstring, err := extractModuleDocstringFromFile(path)
	if err != nil {
		return err
	}

	if !hasMeaningfulModuleDocstring(docstring) {
		violations.MissingDocstring = append(
			violations.MissingDocstring,
			filepath.ToSlash(path),
		)
	}

	markdownFiles, err := listColocatedMarkdownFiles(path)
	if err != nil {
		return err
	}

	collectModuleMarkdownViolations(
		path,
		docstring,
		markdownFiles,
		violations,
		allMarkdown,
	)
	collectModuleDocstringReferenceViolations(path, docstring, violations)

	return nil
}

func collectModuleMarkdownViolations(
	path string,
	docstring string,
	markdownFiles []string,
	violations *moduleDocsViolations,
	allMarkdown map[string]struct{},
) {
	if len(markdownFiles) == 0 {
		violations.MissingMarkdown = append(
			violations.MissingMarkdown,
			filepath.ToSlash(path),
		)

		return
	}

	for _, markdownFile := range markdownFiles {
		allMarkdown[markdownFile] = struct{}{}
	}

	missingRefs := missingModuleDocstringReferences(docstring, markdownFiles)
	if len(missingRefs) > 0 {
		violations.MissingRefs = append(violations.MissingRefs, moduleDocsMissingRefs{
			PythonFile: filepath.ToSlash(path),
			Markdown:   append([]string{}, missingRefs...),
		})
	}
}

func collectModuleDocstringReferenceViolations(
	path string,
	docstring string,
	violations *moduleDocsViolations,
) {
	if docstring == "" {
		return
	}

	if refs := extractModulePathPrefixedReferences(docstring); len(refs) > 0 {
		violations.PathPrefixed = append(
			violations.PathPrefixed,
			moduleDocsPathRefs{
				PythonFile: filepath.ToSlash(path),
				Refs:       refs,
			},
		)
	}

	if refs := nonexistentModuleReferences(path, docstring); len(refs) > 0 {
		violations.NonexistentRefs = append(
			violations.NonexistentRefs,
			moduleDocsBadRefs{
				PythonFile: filepath.ToSlash(path),
				Refs:       refs,
			},
		)
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func checkModuleDocsCommand(_ Config, args []string) int {
	settings, err := loadModuleDocsSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled {
		return 0
	}

	files, err := moduleDocsCommandFiles(args, settings)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	violations, err := collectModuleDocsViolations(files, settings)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return reportModuleDocsViolations(violations)
}

func moduleDocsCommandFiles(
	args []string,
	settings moduleDocsSettings,
) ([]string, error) {
	if len(args) == 0 {
		return discoverModuleDocsFiles(".", settings)
	}

	files := make([]string, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) == extPy {
			files = append(files, filepath.ToSlash(path))
		}
	}

	return files, nil
}

func reportModuleDocsViolations(violations moduleDocsViolations) int {
	findings := moduleDocsHookFindings(violations)
	if len(findings) == 0 {
		return 0
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "module_docs",
		Title:    "MODULE DOCUMENTATION CHECK FAILED",
		Summary:  "Documentation files must follow MODULE.md naming and reference contracts.",
		Findings: findings,
		Guidance: []string{
			"Add missing module docstrings and MODULE.md files.",
			"Link docs from __init__.py/conftest.py and SOURCE_DOCS.md.",
			"Rename README.md docs to the containing directory's MODULE.md naming convention.",
		},
	}, selectedHookOutputFormat())

	return 1
}

func moduleDocsHookFindings(violations moduleDocsViolations) []hookFinding {
	findings := make([]hookFinding, 0, moduleDocsFindingCount(violations))
	for _, path := range violations.MissingDocstring {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    path,
			Code:    "missing_docstring",
			Message: "missing module docstring",
		})
	}

	for _, path := range violations.MissingMarkdown {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    path,
			Code:    "missing_markdown",
			Message: "missing MODULE.md documentation",
		})
	}

	for _, item := range violations.MissingRefs {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    item.PythonFile,
			Code:    "missing_refs",
			Message: "missing documentation refs",
			Detail:  strings.Join(item.Markdown, ", "),
		})
	}

	for _, path := range violations.MissingIndex {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    path,
			Code:    "missing_index",
			Message: "missing SOURCE_DOCS.md index reference",
		})
	}

	for _, item := range violations.PathPrefixed {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    item.PythonFile,
			Code:    "path_prefixed_refs",
			Message: "documentation refs must be bare filenames",
			Detail:  strings.Join(item.Refs, ", "),
		})
	}

	for _, item := range violations.NonexistentRefs {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    item.PythonFile,
			Code:    "nonexistent_refs",
			Message: "documentation refs point to missing files",
			Detail:  strings.Join(item.Refs, ", "),
		})
	}

	for _, path := range violations.BannedFilenames {
		findings = append(findings, hookFinding{
			Tool:    "module_docs",
			File:    path,
			Code:    "banned_filename",
			Message: "documentation file uses banned name",
		})
	}

	return findings
}

func moduleDocsFindingCount(violations moduleDocsViolations) int {
	return len(violations.MissingDocstring) +
		len(violations.MissingMarkdown) +
		len(violations.MissingRefs) +
		len(violations.MissingIndex) +
		len(violations.PathPrefixed) +
		len(violations.NonexistentRefs) +
		len(violations.BannedFilenames)
}
