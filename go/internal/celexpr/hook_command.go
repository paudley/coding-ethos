// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type hookCommandScopeRule struct {
	CommandFunctions      []string
	PathSensitiveRunCalls []string
	ScopeGuardCalls       []string
}

type HookCommandInput struct {
	File                           string   `json:"file"`
	SymbolName                     string   `json:"symbol_name"`
	SymbolPath                     string   `json:"symbol_path"`
	CallNames                      []string `json:"call_names"`
	Line                           int64    `json:"line"`
	CommandFunction                bool     `json:"command_function"`
	RunsPathSensitiveCheck         bool     `json:"runs_path_sensitive_check"`
	ChangedFileScopeBeforeRun      bool     `json:"changed_file_scope_before_run"`
	UnsafeUnscopedPathSensitiveRun bool     `json:"unsafe_unscoped_path_sensitive_run"`
}

func HookCommandInputs(cwd string, files []string) []HookCommandInput {
	inputs := []HookCommandInput{}

	for _, file := range cleanStringSlice(files) {
		if filepath.Ext(file) != ".go" {
			continue
		}

		content, err := os.ReadFile(cleanReadablePath(cwd, file))
		if err != nil {
			continue
		}

		facts, supported, err := astfacts.Analyze(file, content)
		if err != nil || !supported {
			continue
		}

		inputs = append(inputs, hookCommandInputsForFile(facts)...)
	}

	return inputs
}

func hookCommandInputsForFile(file astfacts.File) []HookCommandInput {
	inputs := []HookCommandInput{}

	for _, symbol := range file.Symbols {
		if symbol.SymbolKind != "function" {
			continue
		}

		calls := append([]string(nil), symbol.OrderedCallNames...)
		scope := hookCommandScope(symbol.SymbolName, calls)

		inputs = append(inputs, HookCommandInput{
			File:                      cleanInputFile(symbol.Path),
			SymbolName:                symbol.SymbolName,
			SymbolPath:                symbol.SymbolPath,
			CallNames:                 calls,
			Line:                      int64(symbol.StartLine),
			CommandFunction:           scope.CommandFunction,
			RunsPathSensitiveCheck:    scope.RunsPathSensitiveCheck,
			ChangedFileScopeBeforeRun: scope.ChangedFileScopeBeforeRun,
			UnsafeUnscopedPathSensitiveRun: scope.CommandFunction &&
				scope.RunsPathSensitiveCheck && !scope.ChangedFileScopeBeforeRun,
		})
	}

	return inputs
}

type hookCommandScopeResult struct {
	CommandFunction           bool
	RunsPathSensitiveCheck    bool
	ChangedFileScopeBeforeRun bool
}

func hookCommandScope(symbolName string, calls []string) hookCommandScopeResult {
	for _, rule := range hookCommandScopeRules() {
		if !slices.Contains(rule.CommandFunctions, symbolName) {
			continue
		}

		runIndex := firstMatchingCallIndex(calls, rule.PathSensitiveRunCalls)
		scopeIndex := firstMatchingCallIndex(calls, rule.ScopeGuardCalls)
		runsPathSensitive := runIndex >= 0

		return hookCommandScopeResult{
			CommandFunction:        true,
			RunsPathSensitiveCheck: runsPathSensitive,
			ChangedFileScopeBeforeRun: runsPathSensitive && scopeIndex >= 0 &&
				scopeIndex < runIndex,
		}
	}

	return hookCommandScopeResult{}
}

func hookCommandScopeRules() []hookCommandScopeRule {
	return []hookCommandScopeRule{
		{
			CommandFunctions: []string{
				"checkDocstringCoverageCommand",
			},
			PathSensitiveRunCalls: []string{
				"runDocstringCoverage",
			},
			ScopeGuardCalls: []string{
				"scopeDocstringCoverageForHook",
			},
		},
	}
}

func firstMatchingCallIndex(calls, names []string) int {
	for index, call := range calls {
		if slices.Contains(names, call) {
			return index
		}
	}

	return -1
}

func cleanReadablePath(cwd, file string) string {
	cleanFile := filepath.Clean(file)
	if filepath.IsAbs(cleanFile) || strings.TrimSpace(cwd) == "" {
		return cleanFile
	}

	return filepath.Clean(filepath.Join(cwd, cleanFile))
}
