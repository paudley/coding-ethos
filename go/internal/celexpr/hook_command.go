// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

import (
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

const (
	docstringCoverageRunCall   = "runDocstringCoverage"
	docstringCoverageScopeCall = "scopeDocstringCoverageForHook"
)

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
		commandFunction := strings.HasSuffix(symbol.SymbolName, "Command")
		runIndex := firstCallIndex(calls, docstringCoverageRunCall)
		scopeIndex := firstCallIndex(calls, docstringCoverageScopeCall)
		runsPathSensitive := runIndex >= 0
		scopeBeforeRun := runsPathSensitive && scopeIndex >= 0 && scopeIndex < runIndex

		inputs = append(inputs, HookCommandInput{
			File:                      cleanInputFile(symbol.Path),
			SymbolName:                symbol.SymbolName,
			SymbolPath:                symbol.SymbolPath,
			CallNames:                 calls,
			Line:                      int64(symbol.StartLine),
			CommandFunction:           commandFunction,
			RunsPathSensitiveCheck:    runsPathSensitive,
			ChangedFileScopeBeforeRun: scopeBeforeRun,
			UnsafeUnscopedPathSensitiveRun: commandFunction &&
				runsPathSensitive && !scopeBeforeRun,
		})
	}

	return inputs
}

func firstCallIndex(calls []string, name string) int {
	for index, call := range calls {
		if call == name {
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
