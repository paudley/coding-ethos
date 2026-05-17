// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

func celPythonASTFacts(
	context Context,
	expression string,
) []celexpr.PythonASTFactInput {
	if !celExpressionNeedsPythonAST(expression) {
		return nil
	}

	sources, err := pythonSources(context)
	if err != nil {
		return nil
	}

	facts := []celexpr.PythonASTFactInput{}

	for _, source := range sources {
		sourceFacts, err := collectPythonASTFacts(source)
		if err != nil {
			continue
		}

		for _, fact := range sourceFacts {
			facts = append(facts, pythonASTFactInput(fact))
		}
	}

	return facts
}

func celExpressionNeedsPythonAST(expression string) bool {
	return strings.Contains(expression, "python_ast")
}

func pythonASTFactInput(fact pythonASTFact) celexpr.PythonASTFactInput {
	return celexpr.PythonASTFactInput{
		File:              fact.File,
		Language:          fact.Language,
		NodeKind:          fact.NodeKind,
		SymbolKind:        fact.SymbolKind,
		SymbolName:        fact.SymbolName,
		SymbolPath:        fact.SymbolPath,
		ParentSymbolPath:  fact.ParentSymbolPath,
		Text:              fact.Text,
		ImportModule:      fact.ImportModule,
		CallName:          fact.CallName,
		AnnotationRole:    fact.AnnotationRole,
		Line:              int64(fact.Line),
		Column:            int64(fact.Column),
		EndLine:           int64(fact.EndLine),
		ParameterCount:    int64(fact.ParameterCount),
		HasVarargs:        fact.HasVarargs,
		HasKwargs:         fact.HasKwargs,
		ModuleLevel:       fact.ModuleLevel,
		UnderClass:        fact.UnderClass,
		UnderConditional:  fact.UnderConditional,
		UnderFunction:     fact.UnderFunction,
		UnderTry:          fact.UnderTry,
		UnderTypeChecking: fact.UnderTypeChecking,
		IsImport:          fact.IsImport,
		IsImportFallback:  fact.IsImportFallback,
		IsDynamicImport:   fact.IsDynamicImport,
		IsAssignedLambda:  fact.IsAssignedLambda,
		IsClosureFactory:  fact.IsClosureFactory,
	}
}
