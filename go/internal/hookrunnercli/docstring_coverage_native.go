// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

const (
	docstringCoveragePercent = 100
	pythonNodeAsyncFunction  = "async_function_definition"
	pythonNodeClass          = "class_definition"
	pythonNodeFunction       = "function_definition"
)

type docstringCoverageStats struct {
	Missing    []string
	Total      int
	Documented int
}

func usesNativeDocstringCoverage(command []string) bool {
	return len(command) == 1 && command[0] == nativeDocstringCoverageCommand
}

func runNativeDocstringCoverage(
	settings docstringCoverageSettings,
) (int, string, string, error) {
	stats, err := collectNativeDocstringCoverage(settings)
	if err != nil {
		return 1, "", "", err
	}

	stdout := renderNativeDocstringCoverageOutput(stats)
	if nativeDocstringCoverageFailsThreshold(stats, settings.Threshold) {
		return 1, stdout, "", nil
	}

	return 0, stdout, "", nil
}

func collectNativeDocstringCoverage(
	settings docstringCoverageSettings,
) (docstringCoverageStats, error) {
	var stats docstringCoverageStats

	excludePatterns, err := compileDocstringCoverageExcludes(settings.ExcludePatterns)
	if err != nil {
		return stats, err
	}

	for _, checkPath := range settings.CheckPaths {
		err = collectNativeDocstringCoveragePath(
			settings,
			checkPath,
			excludePatterns,
			&stats,
		)
		if err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func collectNativeDocstringCoveragePath(
	settings docstringCoverageSettings,
	checkPath string,
	excludePatterns []*regexp.Regexp,
	stats *docstringCoverageStats,
) error {
	root := filepath.Join(settings.ConsumerRoot, filepath.Clean(checkPath))

	err := filepath.WalkDir(root, func(
		path string,
		entry os.DirEntry,
		walkErr error,
	) error {
		return collectNativeDocstringCoverageEntry(
			path,
			entry,
			walkErr,
			settings,
			excludePatterns,
			stats,
		)
	})
	if err != nil {
		return fmt.Errorf("walk docstring coverage path %q: %w", root, err)
	}

	return nil
}

func collectNativeDocstringCoverageEntry(
	path string,
	entry os.DirEntry,
	walkErr error,
	settings docstringCoverageSettings,
	excludePatterns []*regexp.Regexp,
	stats *docstringCoverageStats,
) error {
	if walkErr != nil {
		return fmt.Errorf("walk docstring coverage entry %q: %w", path, walkErr)
	}

	relPath, err := filepath.Rel(settings.ConsumerRoot, path)
	if err != nil {
		return fmt.Errorf("resolve docstring coverage path %q: %w", path, err)
	}

	if docstringCoveragePathExcluded(relPath, excludePatterns) {
		return skipDocstringCoveragePath(entry)
	}

	if entry.IsDir() || filepath.Ext(path) != extPy {
		return nil
	}

	return collectFileDocstringCoverage(path, relPath, settings, stats)
}

func skipDocstringCoveragePath(entry os.DirEntry) error {
	if entry.IsDir() {
		return filepath.SkipDir
	}

	return nil
}

func compileDocstringCoverageExcludes(patterns []string) ([]*regexp.Regexp, error) {
	excludes := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf(
				"compile docstring coverage exclude %q: %w",
				pattern,
				err,
			)
		}

		excludes = append(excludes, compiled)
	}

	return excludes, nil
}

func docstringCoveragePathExcluded(path string, patterns []*regexp.Regexp) bool {
	normalized := filepath.ToSlash(path)
	for _, pattern := range patterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}

	return false
}

func collectFileDocstringCoverage(
	path string,
	relPath string,
	settings docstringCoverageSettings,
	stats *docstringCoverageStats,
) error {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return err
	}
	defer tree.Close()

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if nativeDocstringCoverageNode(node) {
			collectPythonSymbolDocstringCoverage(node, source, relPath, settings, stats)
		}
	})

	return nil
}

func nativeDocstringCoverageNode(node *ts.Node) bool {
	switch node.Kind() {
	case pythonNodeAsyncFunction, pythonNodeClass, pythonNodeFunction:
		return true
	default:
		return false
	}
}

func collectPythonSymbolDocstringCoverage(
	node *ts.Node,
	source []byte,
	relPath string,
	settings docstringCoverageSettings,
	stats *docstringCoverageStats,
) {
	name := pythonNodeText(node.ChildByFieldName("name"), source)
	if skipDocstringCoverageSymbol(node, name, settings) {
		return
	}

	countDocstringCoverageItem(
		stats,
		fmt.Sprintf("%s:%s:%s", relPath, node.Kind(), name),
		hasNativeBlockDocstring(node.ChildByFieldName("body"), source),
	)
}

func skipDocstringCoverageSymbol(
	node *ts.Node,
	name string,
	settings docstringCoverageSettings,
) bool {
	return name == "" ||
		skipDocstringCoverageSpecialMethod(name, settings) ||
		skipDocstringCoveragePrivateSymbol(name, settings) ||
		skipDocstringCoverageNestedSymbol(node, settings)
}

func skipDocstringCoverageSpecialMethod(
	name string,
	settings docstringCoverageSettings,
) bool {
	return settings.IgnoreInitMethod && name == "__init__" ||
		settings.IgnoreMagic &&
			strings.HasPrefix(name, "__") &&
			strings.HasSuffix(name, "__")
}

func skipDocstringCoveragePrivateSymbol(
	name string,
	settings docstringCoverageSettings,
) bool {
	return settings.IgnorePrivate && strings.HasPrefix(name, "__") ||
		settings.IgnoreSemiprivate && strings.HasPrefix(name, "_")
}

func skipDocstringCoverageNestedSymbol(
	node *ts.Node,
	settings docstringCoverageSettings,
) bool {
	if nativeDocstringCoverageFunctionNode(node) &&
		settings.IgnoreNestedFunctions &&
		hasPythonAncestor(node, pythonNodeFunction) {
		return true
	}

	return node.Kind() == pythonNodeClass &&
		settings.IgnoreNestedClasses &&
		hasPythonAncestor(node, pythonNodeClass)
}

func nativeDocstringCoverageFunctionNode(node *ts.Node) bool {
	return node.Kind() == pythonNodeFunction || node.Kind() == pythonNodeAsyncFunction
}

func hasPythonAncestor(node *ts.Node, kind string) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == kind {
			return true
		}
	}

	return false
}

func hasNativeBlockDocstring(block *ts.Node, source []byte) bool {
	if block == nil {
		return false
	}

	cursor := block.Walk()
	defer cursor.Close()

	children := block.NamedChildren(cursor)
	if len(children) == 0 || children[0].Kind() != pythonNodeExprStmt {
		return false
	}

	expression := children[0].NamedChild(0)

	return expression != nil &&
		(expression.Kind() == pythonNodeString ||
			expression.Kind() == pythonNodeConcatString) &&
		strings.TrimSpace(stringNodeLiteralText(expression, source)) != ""
}

func countDocstringCoverageItem(
	stats *docstringCoverageStats,
	label string,
	documented bool,
) {
	stats.Total++
	if documented {
		stats.Documented++

		return
	}

	stats.Missing = append(stats.Missing, label)
}

func renderNativeDocstringCoverageOutput(stats docstringCoverageStats) string {
	coverage := 100.0
	if stats.Total > 0 {
		coverage = float64(stats.Documented) *
			docstringCoveragePercent /
			float64(stats.Total)
	}

	var stdout strings.Builder

	_, _ = fmt.Fprintf(
		&stdout,
		"Coverage: %.1f%% (%d/%d documented)\n",
		coverage,
		stats.Documented,
		stats.Total,
	)

	for _, missing := range stats.Missing {
		_, _ = fmt.Fprintf(&stdout, "Missing: %s\n", missing)
	}

	return stdout.String()
}

func nativeDocstringCoverageFailsThreshold(
	stats docstringCoverageStats,
	threshold int,
) bool {
	total := stats.Total
	if total == 0 {
		total = 1
	}

	return stats.Documented*docstringCoveragePercent < threshold*total
}
