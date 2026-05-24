// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluatePythonOptionalReturns(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonOptionalReturnIssue)
}

func EvaluatePythonCatchAndSilence(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonSilentExceptionIssue)
}

func EvaluatePythonStructuredLogging(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonStructuredLogIssue)
}

func EvaluatePythonDirectImports(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonDirectImportIssue)
}

func firstPythonOptionalReturnIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if !fact.IsOptionalReturn ||
			fact.SymbolName == "__exit__" ||
			fact.SymbolName == "__aexit__" {
			continue
		}

		return newPythonASTIssueFromFact(
			fact,
			"optional-return",
			"Required return values must not be modeled as optional.",
		)
	}

	return nil
}

func firstPythonSilentExceptionIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if fact.IsSilentExcept {
			return newPythonASTIssueFromFact(
				fact,
				"silent-exception",
				"Exception handlers must not silently swallow failures.",
			)
		}
	}

	return nil
}

func firstPythonStructuredLogIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if fact.IsUnstructuredLogMessage {
			return newPythonASTIssueFromFact(
				fact,
				"unstructured-log-message",
				"Logger calls should preserve structured context.",
			)
		}
	}

	return nil
}

func firstPythonDirectImportIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if !fact.IsDirectImport || insidePackage(fact.File, "coding_ethos") {
			continue
		}

		return newPythonASTIssueFromFact(
			fact,
			"direct-import",
			"Import protected packages through their public API boundary.",
		)
	}

	return nil
}

type pythonSource struct {
	Path string
	Text string
}

func pythonSources(context Context) ([]pythonSource, error) {
	if context.Content != "" {
		return []pythonSource{
			{Path: firstFile(context.Files), Text: context.Content},
		}, nil
	}

	sources := []pythonSource{}

	for _, file := range context.Files {
		if filepath.Ext(file) != ".py" {
			continue
		}

		payload, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("read python source %s: %w", file, err)
		}

		sources = append(sources, pythonSource{Path: file, Text: string(payload)})
	}

	return sources, nil
}

func firstFile(files []string) string {
	if len(files) == 0 {
		return ""
	}

	return files[0]
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func insidePackage(path, packageName string) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")

	return normalized == packageName || strings.HasPrefix(normalized, packageName+"/")
}
