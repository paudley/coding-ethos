// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

func isDirectImportExempt(path string, settings directImportsSettings) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))
	for _, marker := range settings.ExemptPaths {
		normalizedMarker := filepath.ToSlash(filepath.Clean(strings.TrimSpace(marker)))
		if normalizedMarker != "." && normalizedMarker != "" &&
			strings.Contains(normalizedPath, normalizedMarker) {
			return true
		}
	}

	return false
}

func checkUtilCentralizationCommand(_ Config, args []string) int {
	settings, err := loadUtilCentralizationSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]directImportViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := findUtilityViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintln(os.Stderr, formatDirectImportReport(
		"util_centralization",
		"BANNED DIRECT IMPORT DETECTED",
		"Production code must use configured wrapper modules instead of importing utility libraries directly.",
		violations,
	))

	return 1
}

func formatDirectImportReport(
	tool string,
	title string,
	summary string,
	violations []directImportViolation,
) string {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    tool,
			File:    violation.File,
			Line:    violation.Line,
			Message: violation.Statement,
			Detail:  "use " + violation.Suggestion,
		})
	}

	return formatHookReport(hookReport{
		Tool:     tool,
		Title:    title,
		Summary:  summary,
		Findings: findings,
		Guidance: []string{"Import through the public or configured wrapper API."},
	}, selectedHookOutputFormat())
}

func checkSQLCentralizationCommand(_ Config, args []string) int {
	settings, err := loadSQLCentralizationSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]sqlViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		found, err := findSQLViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}

		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return 0
	}

	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "sql_centralization",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.Pattern,
			Message: violation.Snippet,
		})
	}

	fmt.Fprintln(os.Stderr, formatHookReport(hookReport{
		Tool:  "sql_centralization",
		Title: "SQL STRINGS FOUND OUTSIDE " + settings.ModuleName,
		Summary: fmt.Sprintf(
			"All SQL, DDL, DML, and Cypher strings must live in %s.",
			settings.ModuleName,
		),
		Findings: findings,
		Guidance: []string{
			fmt.Sprintf(
				"Move the SQL string to %s as a Final[str] constant.",
				sqlModuleHint(settings),
			),
			fmt.Sprintf("Import it from %s.", settings.ModuleName),
			fmt.Sprintf(
				"For dynamic queries, create a builder function in %s.",
				settings.ModuleName,
			),
		},
	}, selectedHookOutputFormat()))

	return 1
}

func loggerMethodAndReceiver(
	node *ts.Node,
	source []byte,
	settings structuredLoggingSettings,
) (string, string, bool) {
	if node == nil || node.Kind() != pythonNodeCall {
		return "", "", false
	}

	function := node.ChildByFieldName("function")
	if function == nil || function.Kind() != pythonNodeAttribute {
		return "", "", false
	}

	method := pythonNodeText(function.ChildByFieldName(pythonNodeAttribute), source)
	if !isAllowedLoggerMethod(method, settings) {
		return "", "", false
	}

	receiverNode := function.ChildByFieldName("object")

	receiver := pythonNodeText(receiverNode, source)
	if receiver == "" {
		return "", "", false
	}

	if isAllowedLoggerReceiver(receiverNode, receiver, source, settings) {
		return receiver, method, true
	}

	return "", "", false
}

func isAllowedLoggerMethod(
	method string,
	settings structuredLoggingSettings,
) bool {
	methods := stringSet(settings.Methods)

	return method != "" && method != "exception" && methods[method]
}

func isAllowedLoggerReceiver(
	receiverNode *ts.Node,
	receiver string,
	source []byte,
	settings structuredLoggingSettings,
) bool {
	loggerNames := stringSet(settings.LoggerNames)
	if loggerNames[receiver] {
		return true
	}

	if receiverNode == nil || receiverNode.Kind() != pythonNodeAttribute {
		return false
	}

	attr := pythonNodeText(receiverNode.ChildByFieldName(pythonNodeAttribute), source)

	return loggerNames[attr]
}

func callHasStructuredContext(
	callNode *ts.Node,
	source []byte,
	settings structuredLoggingSettings,
) bool {
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return false
	}

	exempt := stringSet(settings.ExemptKwargs)

	cursor := args.Walk()
	defer cursor.Close()

	children := args.NamedChildren(cursor)
	for i := range children {
		child := children[i]
		if child.Kind() != pythonNodeKeywordArg {
			continue
		}

		name := pythonNodeText(child.ChildByFieldName("name"), source)
		if name != "" && !exempt[name] {
			return true
		}
	}

	return false
}

func callUsesPercentFormatting(callNode *ts.Node) bool {
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return false
	}

	cursor := args.Walk()
	defer cursor.Close()

	children := args.NamedChildren(cursor)
	count := 0

	for i := range children {
		if children[i].Kind() == pythonNodeKeywordArg {
			continue
		}

		count++
	}

	return count > 1
}

func loggingMessagePreview(callNode *ts.Node, source []byte) string {
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return "<no message>"
	}

	cursor := args.Walk()
	defer cursor.Close()

	children := args.NamedChildren(cursor)
	for i := range children {
		child := children[i]
		if child.Kind() == pythonNodeKeywordArg {
			continue
		}

		switch child.Kind() {
		case pythonNodeString, pythonNodeConcatString:
			return truncateSQLSnippet(stringNodeLiteralText(&child, source))
		default:
			return "<dynamic>"
		}
	}

	return "<no message>"
}

func findStructuredLoggingViolations(
	path string,
	settings structuredLoggingSettings,
) ([]structuredLoggingViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]structuredLoggingViolation, 0)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if node.Kind() != pythonNodeCall {
			return
		}

		_, method, ok := loggerMethodAndReceiver(node, source, settings)
		if !ok {
			return
		}

		if !callHasStructuredContext(node, source, settings) ||
			callUsesPercentFormatting(node) {
			violations = append(violations, structuredLoggingViolation{
				File:    path,
				Line:    treeSitterLine(node.StartPosition().Row),
				Method:  method,
				Preview: loggingMessagePreview(node, source),
			})
		}
	})

	return violations, nil
}

func exceptClauseValue(node *ts.Node) *ts.Node {
	if node == nil || node.Kind() != pythonNodeExceptClause {
		return nil
	}

	if value := node.ChildByFieldName("value"); value != nil {
		return value
	}

	cursor := node.Walk()
	defer cursor.Close()

	children := node.NamedChildren(cursor)
	for i := range children {
		if children[i].Kind() != pythonNodeBlock {
			child := children[i]

			return &child
		}
	}

	return nil
}

func exceptClauseBlock(node *ts.Node) *ts.Node {
	if node == nil || node.Kind() != pythonNodeExceptClause {
		return nil
	}

	if body := node.ChildByFieldName("body"); body != nil {
		return body
	}

	cursor := node.Walk()
	defer cursor.Close()

	children := node.NamedChildren(cursor)
	for i := range children {
		if children[i].Kind() == pythonNodeBlock {
			child := children[i]

			return &child
		}
	}

	return nil
}

func exceptClauseCatchesImportError(
	node *ts.Node,
	settings conditionalImportsSettings,
	source []byte,
) bool {
	if node == nil || node.Kind() != pythonNodeExceptClause {
		return false
	}

	exceptions := stringSet(settings.ExceptionNames)

	value := exceptClauseValue(node)
	if value == nil {
		return true
	}

	if value.Kind() == pythonNodeIdentifier {
		return exceptions[pythonNodeText(value, source)]
	}

	if value.Kind() == "tuple" {
		cursor := value.Walk()
		defer cursor.Close()

		children := value.NamedChildren(cursor)
		for i := range children {
			if children[i].Kind() == pythonNodeIdentifier &&
				exceptions[pythonNodeText(&children[i], source)] {
				return true
			}
		}
	}

	return false
}

func extractImportsFromBlock(block *ts.Node, source []byte) []string {
	if block == nil {
		return nil
	}

	names := make([]string, 0)

	cursor := block.Walk()
	defer cursor.Close()

	children := block.NamedChildren(cursor)
	for i := range children {
		child := children[i]
		switch child.Kind() {
		case "import_statement", pythonNodeImportFrom:
			imports := collectPythonImports(&child, source)
			for _, stmt := range imports {
				if stmt.Kind == pythonNodeImport {
					for _, name := range stmt.Names {
						names = append(names, name.Name)
					}
				} else if stmt.Module != "" {
					names = append(names, stmt.Module)
				}
			}
		}
	}

	return names
}

func capabilityFlagsInExceptClause(
	node *ts.Node,
	settings conditionalImportsSettings,
	source []byte,
) []string {
	if node == nil || node.Kind() != pythonNodeExceptClause {
		return nil
	}

	block := exceptClauseBlock(node)
	if block == nil {
		return nil
	}

	flags := make([]string, 0)

	cursor := block.Walk()
	defer cursor.Close()

	children := block.NamedChildren(cursor)
	for i := range children {
		child := children[i]
		if child.Kind() != pythonNodeExprStmt {
			continue
		}

		expr := child.NamedChild(0)
		if expr == nil || expr.Kind() != pythonNodeAssignment {
			continue
		}

		left := expr.ChildByFieldName("left")
		if left == nil || left.Kind() != pythonNodeIdentifier {
			continue
		}

		name := pythonNodeText(left, source)
		if strings.HasPrefix(name, settings.CapabilityPrefix) {
			flags = append(flags, name)
		}
	}

	return flags
}

func findConditionalImportViolations(
	path string,
	settings conditionalImportsSettings,
) ([]conditionalImportViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]conditionalImportViolation, 0)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if node.Kind() != "try_statement" {
			return
		}

		body := node.ChildByFieldName("body")
		imports := extractImportsFromBlock(body, source)

		excepts := tryStatementExceptClauses(node)
		if !tryStatementCatchesImportError(excepts, settings, source) {
			return
		}

		violations = append(
			violations,
			conditionalImportViolationsForTry(path, node, imports)...,
		)
		violations = append(
			violations,
			conditionalFlagViolationsForTry(path, node, excepts, settings, source)...,
		)
	})

	return violations, nil
}

func tryStatementExceptClauses(node *ts.Node) []ts.Node {
	cursor := node.Walk()
	defer cursor.Close()

	children := node.NamedChildren(cursor)
	excepts := make([]ts.Node, 0)

	for childIndex := range children {
		if children[childIndex].Kind() == pythonNodeExceptClause {
			excepts = append(excepts, children[childIndex])
		}
	}

	return excepts
}

func tryStatementCatchesImportError(
	excepts []ts.Node,
	settings conditionalImportsSettings,
	source []byte,
) bool {
	for exceptIndex := range excepts {
		if exceptClauseCatchesImportError(&excepts[exceptIndex], settings, source) {
			return true
		}
	}

	return false
}

func conditionalImportViolationsForTry(
	path string,
	node *ts.Node,
	imports []string,
) []conditionalImportViolation {
	violations := make([]conditionalImportViolation, 0, len(imports))
	for _, module := range imports {
		violations = append(violations, conditionalImportViolation{
			File:    path,
			Line:    treeSitterLine(node.StartPosition().Row),
			Module:  module,
			Pattern: "try/import/except ImportError",
		})
	}

	return violations
}

func conditionalFlagViolationsForTry(
	path string,
	node *ts.Node,
	excepts []ts.Node,
	settings conditionalImportsSettings,
	source []byte,
) []conditionalImportViolation {
	violations := make([]conditionalImportViolation, 0)

	for exceptIndex := range excepts {
		for _, flag := range capabilityFlagsInExceptClause(
			&excepts[exceptIndex],
			settings,
			source,
		) {
			violations = append(violations, conditionalImportViolation{
				File:    path,
				Line:    treeSitterLine(node.StartPosition().Row),
				Module:  flag,
				Pattern: "HAS_* capability flag in except ImportError",
			})
		}
	}

	return violations
}

func isTypeCheckingRef(
	node *ts.Node,
	settings typeCheckingImportsSettings,
	source []byte,
) bool {
	if node == nil {
		return false
	}

	names := stringSet(settings.TypeCheckingNames)

	switch node.Kind() {
	case pythonNodeIdentifier:
		return names[pythonNodeText(node, source)]
	case pythonNodeAttribute:
		return names[pythonNodeText(node.ChildByFieldName(pythonNodeAttribute), source)] &&
			pythonNodeText(node.ChildByFieldName("object"), source) == "typing"
	default:
		return false
	}
}

func findTypeCheckingImportViolations(
	path string,
	settings typeCheckingImportsSettings,
) ([]typeCheckingViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]typeCheckingViolation, 0)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		switch node.Kind() {
		case "future_import_statement":
			violations = append(
				violations,
				futureImportViolations(path, node, settings, source)...,
			)
		case pythonNodeImportFrom:
			violations = append(
				violations,
				typeCheckingImportFromViolations(path, node, settings, source)...,
			)
		case "if_statement":
			condition := node.ChildByFieldName("condition")
			if isTypeCheckingRef(condition, settings, source) {
				violations = append(violations, typeCheckingViolation{
					File:    path,
					Line:    treeSitterLine(node.StartPosition().Row),
					Pattern: "if TYPE_CHECKING: (conditional import guard)",
				})
			}
		}
	})

	return violations, nil
}

func futureImportViolations(
	path string,
	node *ts.Node,
	settings typeCheckingImportsSettings,
	source []byte,
) []typeCheckingViolation {
	violations := make([]typeCheckingViolation, 0)

	cursor := node.Walk()
	defer cursor.Close()

	names := node.ChildrenByFieldName("name", cursor)
	for nameIndex := range names {
		name := parsePythonImportAlias(&names[nameIndex], source).Name
		if name == settings.FutureImportName {
			violations = append(violations, typeCheckingViolation{
				File:    path,
				Line:    treeSitterLine(node.StartPosition().Row),
				Pattern: "from __future__ import annotations (PEP 563 string annotations)",
			})
		}
	}

	return violations
}

func typeCheckingImportFromViolations(
	path string,
	node *ts.Node,
	settings typeCheckingImportsSettings,
	source []byte,
) []typeCheckingViolation {
	module := pythonNodeText(node.ChildByFieldName("module_name"), source)
	if module != "typing" {
		return nil
	}

	allowedNames := stringSet(settings.TypeCheckingNames)
	violations := make([]typeCheckingViolation, 0)

	cursor := node.Walk()
	defer cursor.Close()

	names := node.ChildrenByFieldName("name", cursor)
	for nameIndex := range names {
		name := parsePythonImportAlias(&names[nameIndex], source).Name
		if allowedNames[name] {
			violations = append(violations, typeCheckingViolation{
				File:    path,
				Line:    treeSitterLine(node.StartPosition().Row),
				Pattern: "from typing import TYPE_CHECKING",
			})
		}
	}

	return violations
}

func exceptClauseBodyStatements(node *ts.Node) []ts.Node {
	block := exceptClauseBlock(node)
	if block == nil {
		return nil
	}

	cursor := block.Walk()
	defer cursor.Close()

	children := block.NamedChildren(cursor)
	statements := make([]ts.Node, 0)

	for childIndex := range children {
		if children[childIndex].Kind() == pythonNodeExprStmt {
			expr := children[childIndex].NamedChild(0)
			if expr != nil && expr.Kind() == pythonNodeString {
				continue
			}
		}

		statements = append(statements, children[childIndex])
	}

	return statements
}

func exceptClauseExceptionType(node *ts.Node, source []byte) string {
	value := exceptClauseValue(node)
	if value == nil {
		return "(bare except)"
	}

	return pythonNodeText(value, source)
}

func silenceBodyDescription(node ts.Node) string {
	switch node.Kind() {
	case "pass_statement":
		return "pass"
	case "continue_statement":
		return "continue"
	case "ellipsis":
		return "..."
	case "return_statement":
		value := returnStatementValue(node)
		if value == nil || value.Kind() == pythonNodeNone {
			return "return None"
		}

		return "return value"
	}

	return "unknown"
}

func returnStatementValue(node ts.Node) *ts.Node {
	if value := node.ChildByFieldName("value"); value != nil {
		return value
	}

	if node.NamedChildCount() > 0 {
		return node.NamedChild(0)
	}

	return nil
}

func isSilencingStatement(node ts.Node) bool {
	switch node.Kind() {
	case "pass_statement", "continue_statement", "ellipsis":
		return true
	case "return_statement":
		value := returnStatementValue(node)

		return value == nil || value.Kind() == pythonNodeNone
	default:
		return false
	}
}

func findCatchSilenceViolations(
	path string,
	_ catchSilenceSettings,
) ([]catchSilenceViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]catchSilenceViolation, 0)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if node.Kind() != pythonNodeExceptClause {
			return
		}

		body := exceptClauseBodyStatements(node)
		if len(body) == 1 && isSilencingStatement(body[0]) {
			violations = append(violations, catchSilenceViolation{
				File:          path,
				Line:          treeSitterLine(node.StartPosition().Row),
				ExceptionType: exceptClauseExceptionType(node, source),
				HandlerBody:   silenceBodyDescription(body[0]),
			})
		}
	})

	return violations, nil
}

func containsNoneUnion(node *ts.Node) bool {
	if node == nil {
		return false
	}

	if unionChildren := noneUnionChildren(node); len(unionChildren) == minCollectionItems {
		left := unionChildren[0]

		right := unionChildren[1]
		if left.Kind() == pythonNodeNone || right.Kind() == pythonNodeNone {
			return true
		}

		return containsNoneUnion(&left) || containsNoneUnion(&right)
	}

	cursor := node.Walk()
	defer cursor.Close()

	children := node.NamedChildren(cursor)
	for childIndex := range children {
		if containsNoneUnion(&children[childIndex]) {
			return true
		}
	}

	return false
}

func noneUnionChildren(node *ts.Node) []ts.Node {
	if node.Kind() != "binary_operator" {
		return nil
	}

	operator := node.Child(1)
	if operator == nil || operator.Kind() != "|" {
		return nil
	}

	cursor := node.Walk()
	defer cursor.Close()

	return node.NamedChildren(cursor)
}

func typedParameterName(node *ts.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()

	children := node.NamedChildren(cursor)
	for childIndex := range children {
		switch children[childIndex].Kind() {
		case pythonNodeIdentifier:
			return pythonNodeText(&children[childIndex], source)
		case "list_splat_pattern":
			return "*" + pythonNodeText(children[childIndex].NamedChild(0), source)
		case "dictionary_splat_pattern":
			return "**" + pythonNodeText(children[childIndex].NamedChild(0), source)
		}
	}

	return "<expr>"
}

func isClassVariableAssignment(node *ts.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != pythonNodeExprStmt {
		return false
	}

	block := parent.Parent()
	if block == nil || block.Kind() != pythonNodeBlock {
		return false
	}

	owner := block.Parent()

	return owner != nil && owner.Kind() == "class_definition"
}

func findOptionalTypeViolations(
	path string,
	settings optionalReturnsSettings,
) ([]optionalTypeViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]optionalTypeViolation, 0)
	exemptMethods := stringSet(settings.ExemptMethodNames)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		switch node.Kind() {
		case pythonNodeAssignment:
			violations = append(
				violations,
				optionalAssignmentViolations(path, node, source)...,
			)
		case "function_definition", "async_function_definition":
			violations = append(
				violations,
				optionalFunctionViolations(path, node, source, exemptMethods)...,
			)
		}
	})

	return violations, nil
}

func optionalAssignmentViolations(
	path string,
	node *ts.Node,
	source []byte,
) []optionalTypeViolation {
	annotation := node.ChildByFieldName("type")
	if annotation == nil || !containsNoneUnion(annotation) {
		return nil
	}

	left := node.ChildByFieldName("left")

	context := "| None variable: " + pythonNodeText(left, source)
	if isClassVariableAssignment(node) {
		context = "| None class variable: " + pythonNodeText(left, source)
	}

	return []optionalTypeViolation{{
		File:    path,
		Line:    treeSitterLine(node.StartPosition().Row),
		Context: context,
	}}
}

func optionalFunctionViolations(
	path string,
	node *ts.Node,
	source []byte,
	exemptMethods map[string]bool,
) []optionalTypeViolation {
	name := pythonNodeText(node.ChildByFieldName("name"), source)
	if exemptMethods[name] {
		return nil
	}

	violations := optionalReturnViolations(path, node, name)

	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return violations
	}

	return append(violations, optionalParameterViolations(path, parameters, source)...)
}

func optionalReturnViolations(
	path string,
	node *ts.Node,
	name string,
) []optionalTypeViolation {
	returnType := node.ChildByFieldName("return_type")
	if returnType == nil || !containsNoneUnion(returnType) {
		return nil
	}

	return []optionalTypeViolation{{
		File:    path,
		Line:    treeSitterLine(returnType.StartPosition().Row),
		Context: fmt.Sprintf("| None return: %s()", name),
	}}
}

func optionalParameterViolations(
	path string,
	parameters *ts.Node,
	source []byte,
) []optionalTypeViolation {
	cursor := parameters.Walk()
	defer cursor.Close()

	children := parameters.NamedChildren(cursor)
	violations := make([]optionalTypeViolation, 0)

	for childIndex := range children {
		child := children[childIndex]
		if !isTypedParameterKind(child.Kind()) {
			continue
		}

		annotation := child.ChildByFieldName("type")
		if annotation == nil || !containsNoneUnion(annotation) {
			continue
		}

		violations = append(violations, optionalTypeViolation{
			File: path,
			Line: treeSitterLine(annotation.StartPosition().Row),
			Context: "| None parameter: " + typedParameterName(
				&child,
				source,
			),
		})
	}

	return violations
}

func isTypedParameterKind(kind string) bool {
	return kind == "typed_parameter" ||
		kind == "typed_default_parameter" ||
		kind == "typed_pattern"
}

func isTestFilePath(path string, settings securityPatternsSettings) bool {
	name := filepath.Base(path)

	for _, marker := range settings.TestFileMarkers {
		if strings.HasSuffix(marker, extPy) {
			if strings.Contains(name, marker) {
				return true
			}

			continue
		}

		if strings.Contains(path, marker) || strings.Contains(name, marker) {
			return true
		}
	}

	return false
}

func sourceSnippet(path string, line int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return unknownFile
	}

	lines := strings.Split(string(content), "\n")
	if line < 1 || line > len(lines) {
		return unknownFile
	}

	return strings.TrimSpace(lines[line-1])
}

func isGetenvCall(node *ts.Node, source []byte) bool {
	if node == nil || node.Kind() != pythonNodeCall {
		return false
	}

	function := node.ChildByFieldName("function")
	if function == nil || function.Kind() != pythonNodeAttribute {
		return false
	}

	attr := pythonNodeText(function.ChildByFieldName(pythonNodeAttribute), source)

	object := function.ChildByFieldName("object")
	if attr == "getenv" && object != nil && object.Kind() == pythonNodeIdentifier &&
		pythonNodeText(object, source) == "os" {
		return true
	}

	return attr == "get" && object != nil && object.Kind() == pythonNodeAttribute &&
		pythonNodeText(object.ChildByFieldName(pythonNodeAttribute), source) == "environ"
}

func getenvDefaultValue(
	node *ts.Node,
	settings securityPatternsSettings,
	source []byte,
) *ts.Node {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}

	cursor := args.Walk()
	defer cursor.Close()

	children := args.NamedChildren(cursor)
	positional := make([]*ts.Node, 0)

	for i := range children {
		child := children[i]
		if child.Kind() == pythonNodeKeywordArg {
			name := child.ChildByFieldName("name")
			if name != nil && pythonNodeText(name, source) == "default" {
				return child.ChildByFieldName("value")
			}

			continue
		}

		positional = append(positional, &child)
	}

	if settings.MinGetenvArgsWithDefault > 0 &&
		len(positional) >= settings.MinGetenvArgsWithDefault {
		return positional[settings.MinGetenvArgsWithDefault-1]
	}

	return nil
}

func isSuspiciousSecret(value string, settings securityPatternsSettings) bool {
	lower := strings.ToLower(value)
	for _, pattern := range settings.SecretPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

func isOSEnvironSubscript(node *ts.Node, source []byte) bool {
	if node == nil || node.Kind() != "subscript" {
		return false
	}

	value := node.ChildByFieldName("value")

	return value != nil && value.Kind() == pythonNodeAttribute &&
		pythonNodeText(value.ChildByFieldName(pythonNodeAttribute), source) == "environ" &&
		pythonNodeText(value.ChildByFieldName("object"), source) == "os"
}

func stringHasInterpolation(node *ts.Node) bool {
	if node == nil {
		return false
	}

	cursor := node.Walk()
	defer cursor.Close()

	children := node.Children(cursor)
	for i := range children {
		child := children[i]
		if child.Kind() == "interpolation" {
			return true
		}
	}

	return false
}

func sqlKeywordPrefix(literal string, settings securityPatternsSettings) string {
	stripped := strings.TrimSpace(strings.ToUpper(literal))
	for _, keyword := range settings.SQLKeywords {
		if strings.HasPrefix(stripped, keyword) {
			rest := stripped[len(keyword):]
			if rest == "" || !isAlphaNumeric(rest[0]) {
				return keyword
			}
		}
	}

	return ""
}

func isAlphaNumeric(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z')
}

func findSecurityViolations(
	path string,
	settings securityPatternsSettings,
) ([]securityViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]securityViolation, 0)
	isTestFile := isTestFilePath(path, settings)

	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		switch node.Kind() {
		case pythonNodeCall:
			violations = append(
				violations,
				securityCallViolations(path, node, settings, source)...,
			)
		case pythonNodeAssignment:
			violations = append(
				violations,
				securityAssignmentViolations(path, node, settings, source, isTestFile)...,
			)
		}
	})

	return violations, nil
}

func securityCallViolations(
	path string,
	node *ts.Node,
	settings securityPatternsSettings,
	source []byte,
) []securityViolation {
	if !isGetenvCall(node, source) {
		return nil
	}

	defaultNode := getenvDefaultValue(node, settings, source)
	if defaultNode == nil || defaultNode.Kind() != pythonNodeString {
		return nil
	}

	value := stringNodeLiteralText(defaultNode, source)
	if !isSuspiciousSecret(value, settings) {
		return nil
	}

	line := treeSitterLine(defaultNode.StartPosition().Row)

	return []securityViolation{{
		File:     path,
		Line:     line,
		Category: "DEFAULT_SECRET",
		Message: "os.getenv() has default value that looks like a secret. " +
			"Secrets must come from environment with no defaults.",
		Snippet: sourceSnippet(path, line),
	}}
}

func securityAssignmentViolations(
	path string,
	node *ts.Node,
	settings securityPatternsSettings,
	source []byte,
	isTestFile bool,
) []securityViolation {
	violations := make([]securityViolation, 0)
	if violation, ok := sqlInterpolationViolation(path, node, settings, source); ok {
		violations = append(violations, violation)
	}

	if violation, ok := testEnvBypassViolation(path, node, source, isTestFile); ok {
		violations = append(violations, violation)
	}

	return violations
}

func sqlInterpolationViolation(
	path string,
	node *ts.Node,
	settings securityPatternsSettings,
	source []byte,
) (securityViolation, bool) {
	right := node.ChildByFieldName("right")
	if right == nil || right.Kind() != pythonNodeString || !stringHasInterpolation(right) {
		return securityViolation{}, false
	}

	keyword := sqlKeywordPrefix(stringNodeLiteralText(right, source), settings)
	if keyword == "" {
		return securityViolation{}, false
	}

	line := treeSitterLine(right.StartPosition().Row)

	return securityViolation{
		File:     path,
		Line:     line,
		Category: "SQL_INJECTION",
		Message: fmt.Sprintf(
			"F-string appears to contain SQL (%s...). Use parameterized queries instead.",
			keyword,
		),
		Snippet: sourceSnippet(path, line),
	}, true
}

func testEnvBypassViolation(
	path string,
	node *ts.Node,
	source []byte,
	isTestFile bool,
) (securityViolation, bool) {
	if !isTestFile {
		return securityViolation{}, false
	}

	left := node.ChildByFieldName("left")
	if !isOSEnvironSubscript(left, source) {
		return securityViolation{}, false
	}

	line := treeSitterLine(left.StartPosition().Row)

	return securityViolation{
		File:     path,
		Line:     line,
		Category: "TEST_ENV_BYPASS",
		Message: "os.environ assignment in test file bypasses bootstrap validation. " +
			"Use fixtures that call bootstrap().",
		Snippet: sourceSnippet(path, line),
	}, true
}
