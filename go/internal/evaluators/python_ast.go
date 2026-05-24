// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

//nolint:gochecknoglobals
var pythonSuppressionCommentPatterns = []pythonSuppressionPattern{
	{
		regex: regexp.MustCompile(`(?i)#\s*ruff:\s*noqa\b`),
		label: "ruff: noqa",
	},
	{
		regex: regexp.MustCompile(`(?i)#\s*mypy:\s*ignore-errors\b`),
		label: "mypy: ignore-errors",
	},
	{
		regex: regexp.MustCompile(`(?i)#\s*pyright:\s*ignore\b`),
		label: "pyright: ignore",
	},
	{
		regex: regexp.MustCompile(`(?i)#\s*pylint:\s*disable\b`),
		label: "pylint: disable",
	},
	{
		regex: regexp.MustCompile(`(?i)#\s*type:\s*ignore\b`),
		label: "type: ignore",
	},
	{
		regex: regexp.MustCompile(`(?i)#\s*noqa\b`),
		label: "noqa",
	},
}

var pythonUnexplainedTypeIgnorePattern = regexp.MustCompile(
	`(?i)#\s*type:\s*ignore\s*$`,
)

type pythonSuppressionPattern struct {
	regex *regexp.Regexp
	label string
}

type pythonASTFact struct {
	File                    string
	Language                string
	NodeKind                string
	SymbolKind              string
	SymbolName              string
	SymbolPath              string
	ParentSymbolPath        string
	EnclosingFunction       string
	EnclosingSymbol         string
	Text                    string
	ReturnAnnotation        string
	ExceptionType           string
	ExceptionAction         string
	ImportModule            string
	CallName                string
	AnnotationRole          string
	SuppressionLabel        string
	LoggerName              string
	LoggerMethod            string
	Line                    int
	Column                  int
	EndLine                 int
	ParameterCount          int
	HasVarargs              bool
	HasKwargs               bool
	ModuleLevel             bool
	UnderClass              bool
	UnderConditional        bool
	UnderFunction           bool
	UnderTry                bool
	UnderTypeChecking       bool
	IsImport                bool
	IsImportFallback        bool
	IsDynamicImport         bool
	IsAssignedLambda        bool
	IsClosureFactory        bool
	IsSuppression           bool
	IsOptionalReturn        bool
	IsBareExcept            bool
	IsSilentExcept          bool
	IsStructuredLog         bool
	IsDirectImport          bool
	IsUnexplainedTypeIgnore bool
}

type pythonASTIssue struct {
	SymbolKind       string
	Code             string
	Detail           string
	Language         string
	NodeKind         string
	Snippet          string
	File             string
	SymbolName       string
	SymbolPath       string
	ParentSymbolPath string
	Line             int
	Column           int
	EndLine          int
}

type pythonASTIssueFunc func([]pythonASTFact) *pythonASTIssue

const (
	pythonLanguage               = "python"
	pythonKindAnnotatedAssign    = "annotated_assignment"
	pythonKindAssign             = "assignment"
	pythonKindCall               = "call"
	pythonKindClassDef           = "class_definition"
	pythonKindComment            = "comment"
	pythonKindExceptClause       = "except_clause"
	pythonKindFunctionDef        = "function_definition"
	pythonKindImportFrom         = "import_from_statement"
	pythonKindImport             = "import_statement"
	pythonKindLambda             = "lambda"
	pythonKindModule             = "module"
	pythonSymbolCall             = "call"
	pythonSymbolFunction         = "function"
	pythonSymbolImport           = "import"
	pythonModuleGetattr          = "__getattr__"
	pythonImportlibImportModule  = "importlib.import_module"
	pythonBuiltinImport          = "__import__"
	pythonTypeCheckingImportCode = "type-checking-import"
	pythonConditionalImportCode  = "conditional-import"
	pythonImportFallbackCode     = "import-error-fallback"
	pythonDynamicGetattrCode     = "dynamic-getattr-import"
	pythonDynamicImportCallCode  = "dynamic-import-call"
	pythonAssignedLambdaCode     = "assigned-lambda"
	pythonClosureFactoryCode     = "closure-factory"
)

func EvaluatePythonConditionalImports(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonConditionalImportIssue)
}

func EvaluatePythonFunctionalIdioms(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonFunctionalIdiomIssue)
}

func evaluatePythonAST(
	policyDef policy.Policy,
	context Context,
	findIssue pythonASTIssueFunc,
) ([]policy.Decision, error) {
	sources, err := pythonSources(context)
	if err != nil {
		return nil, err
	}

	for _, source := range sources {
		facts, err := collectPythonASTFacts(source)
		if err != nil {
			return nil, err
		}

		issue := findIssue(facts)
		if issue != nil {
			return []policy.Decision{
				pythonDecisionWithIssue(policyDef, source, *issue),
			}, nil
		}
	}

	return nil, nil
}

func collectPythonASTFacts(source pythonSource) ([]pythonASTFact, error) {
	contents := []byte(source.Text)

	tree, found, err := astfacts.Parse(source.Path, contents)
	if err != nil {
		return nil, fmt.Errorf(
			"parse python source %s with tree-sitter: %w",
			source.Path,
			err,
		)
	}

	if !found {
		return pythonSnippetFallbackASTFacts(source), nil
	}

	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return pythonSnippetFallbackASTFacts(source), nil
	}

	facts := []pythonASTFact{}
	closureFactories := pythonClosureFactorySymbols(root, contents)
	pythonWalkAllNodes(root, func(node *tree_sitter.Node) {
		if fact, found := pythonASTFactFromNode(
			source,
			node,
			contents,
			closureFactories,
		); found {
			facts = append(facts, fact)
		}
	})

	if len(facts) == 0 || root.HasError() ||
		pythonSourceNeedsSnippetFallback(source.Text) {
		facts = append(facts, pythonSnippetFallbackASTFacts(source)...)
	}

	return facts, nil
}

func pythonWalkAllNodes(root *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if root == nil {
		return
	}

	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		visit(node)

		stack = appendPythonTraversalChildren(stack, node)
	}
}

func appendPythonTraversalChildren(
	stack []*tree_sitter.Node,
	node *tree_sitter.Node,
) []*tree_sitter.Node {
	for index := node.NamedChildCount(); index > 0; index-- {
		child := node.NamedChild(index - 1)
		if child != nil && child.Kind() != pythonKindComment {
			stack = append(stack, child)
		}
	}

	for index := node.ChildCount(); index > 0; index-- {
		child := node.Child(index - 1)
		if child != nil && child.Kind() == pythonKindComment {
			stack = append(stack, child)
		}
	}

	return stack
}

func pythonSnippetFallbackASTFacts(source pythonSource) []pythonASTFact {
	facts := []pythonASTFact{}

	for index, line := range strings.Split(source.Text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if fact, found := pythonSnippetFallbackFact(source, line, trimmed, index+1); found {
			facts = append(facts, fact)
		}
	}

	return facts
}

func pythonSourceNeedsSnippetFallback(source string) bool {
	for line := range strings.SplitSeq(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		return strings.HasPrefix(line, " ") ||
			strings.HasPrefix(line, "\t") ||
			strings.HasPrefix(trimmed, "except ")
	}

	return false
}

func pythonSnippetFallbackFact(
	source pythonSource,
	line string,
	trimmed string,
	lineNumber int,
) (pythonASTFact, bool) {
	indent := leadingSpaces(line)

	fact := pythonASTFact{
		File:        source.Path,
		Language:    pythonLanguage,
		Text:        trimmed,
		Line:        lineNumber,
		Column:      indent + 1,
		EndLine:     lineNumber,
		ModuleLevel: indent == 0,
	}
	switch {
	case strings.HasPrefix(trimmed, "import "):
		fact.NodeKind = pythonKindImport
		fact.SymbolKind = pythonSymbolImport
		fact.ImportModule = trimmed
		fact.IsImport = true
		fact.UnderFunction = indent > 0
		fact.UnderConditional = indent > 0

		return fact, true
	case strings.HasPrefix(trimmed, "from "):
		fact.NodeKind = pythonKindImportFrom
		fact.SymbolKind = pythonSymbolImport
		fact.ImportModule = trimmed
		fact.IsImport = true
		fact.UnderFunction = indent > 0
		fact.UnderConditional = indent > 0

		return fact, true
	case strings.HasPrefix(trimmed, "except ") &&
		(strings.Contains(trimmed, "ImportError") ||
			strings.Contains(trimmed, "ModuleNotFoundError")):
		fact.NodeKind = pythonKindExceptClause
		fact.SymbolKind = "except"
		fact.IsImportFallback = true

		return fact, true
	case strings.Contains(trimmed, "importlib.import_module("):
		fact.NodeKind = pythonKindCall
		fact.SymbolKind = pythonSymbolCall
		fact.CallName = pythonImportlibImportModule
		fact.IsDynamicImport = true

		return fact, true
	case strings.Contains(trimmed, "__import__("):
		fact.NodeKind = pythonKindCall
		fact.SymbolKind = pythonSymbolCall
		fact.CallName = pythonBuiltinImport
		fact.IsDynamicImport = true

		return fact, true
	case strings.HasPrefix(trimmed, "def __getattr__("):
		fact.NodeKind = pythonKindFunctionDef
		fact.SymbolKind = pythonSymbolFunction
		fact.SymbolName = pythonModuleGetattr
		fact.SymbolPath = pythonModuleGetattr

		return fact, true
	default:
		return pythonASTFact{}, false
	}
}

func firstPythonConditionalImportIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		switch {
		case fact.IsImport && fact.UnderTypeChecking:
			return newPythonASTIssueFromFact(
				fact,
				pythonTypeCheckingImportCode,
				pythonASTIssueText(
					"TYPE_CHECKING import branches mask required runtime",
					"dependencies and are forbidden by the import policy.",
				),
			)
		case fact.IsImport && !fact.ModuleLevel:
			return newPythonASTIssueFromFact(
				fact,
				pythonConditionalImportCode,
				pythonASTIssueText(
					"Required imports must stay at module scope; runtime,",
					"nested, or branch-gated imports hide dependency and",
					"design failures.",
				),
			)
		case fact.IsImportFallback:
			return newPythonASTIssueFromFact(
				fact,
				pythonImportFallbackCode,
				pythonASTIssueText(
					"ImportError and ModuleNotFoundError fallback paths",
					"create soft dependencies instead of deterministic",
					"startup failure.",
				),
			)
		case fact.NodeKind == pythonKindFunctionDef &&
			fact.ModuleLevel &&
			fact.SymbolName == pythonModuleGetattr:
			return newPythonASTIssueFromFact(
				fact,
				pythonDynamicGetattrCode,
				pythonASTIssueText(
					"Module-level __getattr__ hides imports behind dynamic",
					"attribute lookup and bypasses deterministic dependency",
					"validation.",
				),
			)
		case fact.IsDynamicImport:
			return newPythonASTIssueFromFact(
				fact,
				pythonDynamicImportCallCode,
				pythonASTIssueText(
					"Dynamic import calls bypass module-level dependency",
					"validation and are forbidden for required dependencies.",
				),
			)
		}
	}

	return nil
}

func firstPythonFunctionalIdiomIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if fact.IsAssignedLambda {
			return newPythonASTIssueFromFact(
				fact,
				pythonAssignedLambdaCode,
				pythonASTIssueText(
					"Assigned lambdas obscure reusable behavior; use",
					"functools.partial, operator helpers, or a named",
					"function.",
				),
			)
		}

		if fact.IsClosureFactory {
			return newPythonASTIssueFromFact(
				fact,
				pythonClosureFactoryCode,
				fmt.Sprintf(
					pythonASTIssueText(
						"Nested function %q is returned or assigned from",
						"its container; prefer functools.partial or an",
						"explicit helper.",
					),
					fact.SymbolName,
				),
			)
		}
	}

	return nil
}

func pythonASTIssueText(parts ...string) string {
	return strings.Join(parts, " ")
}

func pythonASTFactFromNode(
	source pythonSource,
	node *tree_sitter.Node,
	contents []byte,
	closureFactories map[string]bool,
) (pythonASTFact, bool) {
	kind := node.Kind()
	if !pythonASTNodeIsFactCandidate(kind) {
		return pythonASTFact{}, false
	}

	line, endLine, _ := astfacts.NodeRowSpan(node)
	text := strings.TrimSpace(node.Utf8Text(contents))
	fact := pythonASTFact{
		File:        source.Path,
		Language:    pythonLanguage,
		NodeKind:    kind,
		SymbolKind:  pythonSymbolKind(node),
		SymbolName:  pythonNodeSymbolName(node, contents),
		Text:        text,
		Line:        line,
		Column:      pythonNodeColumn(node),
		EndLine:     endLine,
		ModuleLevel: pythonNodeIsModuleLevel(node),
		UnderClass:  pythonHasAncestorKind(node, pythonKindClassDef),
		UnderConditional: pythonHasAncestorKind(
			node,
			"if_statement",
			"for_statement",
			"while_statement",
			"match_statement",
		),
		UnderFunction: pythonHasAncestorKind(node, pythonKindFunctionDef),
		UnderTry: pythonHasAncestorKind(
			node,
			"try_statement",
			pythonKindExceptClause,
		),
		UnderTypeChecking: pythonUnderTypeChecking(node, contents),
	}
	fact.SymbolPath, fact.ParentSymbolPath = pythonSymbolPaths(node, contents)
	fact.EnclosingFunction, fact.EnclosingSymbol = pythonEnclosingFunction(
		node,
		contents,
	)

	if !populatePythonASTFactDetails(&fact, kind, node, contents, closureFactories) {
		return pythonASTFact{}, false
	}

	return fact, true
}

func populatePythonASTFactDetails(
	fact *pythonASTFact,
	kind string,
	node *tree_sitter.Node,
	contents []byte,
	closureFactories map[string]bool,
) bool {
	switch kind {
	case pythonKindComment:
		fact.IsSuppression, fact.SuppressionLabel = pythonSuppressionComment(fact.Text)
		if !fact.IsSuppression {
			return false
		}

		fact.IsUnexplainedTypeIgnore = pythonSuppressionIsUnexplainedTypeIgnore(fact.Text)
	case pythonKindImport, pythonKindImportFrom:
		fact.IsImport = true
		fact.ImportModule = fact.Text
		fact.IsDirectImport = pythonImportTargetsProtectedPackage(fact.Text)
	case pythonKindExceptClause:
		fact.IsImportFallback = strings.Contains(fact.Text, "ImportError") ||
			strings.Contains(fact.Text, "ModuleNotFoundError")
		fact.ExceptionType = pythonExceptionType(node, contents)
		fact.ExceptionAction = pythonExceptionAction(node, contents)
		fact.IsBareExcept = fact.ExceptionType == ""
		fact.IsSilentExcept = pythonExceptionActionIsSilent(fact.ExceptionAction)
	case pythonKindCall:
		fact.CallName = pythonCallName(node, contents)
		fact.IsDynamicImport = fact.CallName == pythonBuiltinImport ||
			fact.CallName == pythonImportlibImportModule
		fact.LoggerName, fact.LoggerMethod = pythonLoggerCallParts(fact.CallName)
		fact.IsStructuredLog = pythonCallHasUnstructuredLogMessage(
			node,
			contents,
			fact.LoggerName,
			fact.LoggerMethod,
		)
	case pythonKindLambda:
		fact.IsAssignedLambda = pythonLambdaIsAssigned(node)
	case pythonKindFunctionDef:
		fact.ParameterCount, fact.HasVarargs, fact.HasKwargs = pythonFunctionParameters(
			node,
		)
		fact.ReturnAnnotation = pythonReturnAnnotation(node, contents)
		fact.IsOptionalReturn = pythonAnnotationIsOptional(fact.ReturnAnnotation)
		fact.IsClosureFactory = closureFactories[pythonNodeKey(node, contents)]
	}

	return true
}

func pythonNodeColumn(node *tree_sitter.Node) int {
	const maxIntValue = int(^uint(0) >> 1)

	column := node.StartPosition().Column
	if column > uint(maxIntValue) {
		return maxIntValue
	}

	return int(column) + 1
}

func pythonASTNodeIsFactCandidate(kind string) bool {
	switch kind {
	case pythonKindAnnotatedAssign,
		pythonKindAssign,
		pythonKindCall,
		pythonKindClassDef,
		pythonKindComment,
		pythonKindExceptClause,
		pythonKindFunctionDef,
		pythonKindImportFrom,
		pythonKindImport,
		pythonKindLambda:
		return true
	default:
		return false
	}
}

func pythonNodeIsModuleLevel(node *tree_sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}

	if parent.Kind() == pythonKindModule {
		return true
	}

	if parent.Kind() == "decorated_definition" {
		grandparent := parent.Parent()

		return grandparent != nil && grandparent.Kind() == pythonKindModule
	}

	return false
}

func pythonHasAncestorKind(node *tree_sitter.Node, kinds ...string) bool {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if slices.Contains(kinds, ancestor.Kind()) {
			return true
		}
	}

	return false
}

func pythonUnderTypeChecking(node *tree_sitter.Node, contents []byte) bool {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Kind() == "if_statement" &&
			pythonIfStatementConditionHasTypeChecking(ancestor, contents) {
			return true
		}
	}

	return false
}

func pythonLambdaIsAssigned(node *tree_sitter.Node) bool {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		switch ancestor.Kind() {
		case pythonKindAssign, pythonKindAnnotatedAssign:
			return true
		case pythonKindModule, pythonKindFunctionDef, pythonKindClassDef:
			return false
		}
	}

	return false
}

func pythonClosureFactorySymbols(
	root *tree_sitter.Node,
	contents []byte,
) map[string]bool {
	factories := map[string]bool{}

	astfacts.Walk(root, func(node *tree_sitter.Node) {
		if node.Kind() != pythonKindFunctionDef {
			return
		}

		for key := range pythonContainerClosureFactories(node, contents) {
			factories[key] = true
		}
	})

	return factories
}

func pythonContainerClosureFactories(
	container *tree_sitter.Node,
	contents []byte,
) map[string]bool {
	nestedByName := map[string][]string{}
	referenced := map[string]bool{}

	astfacts.Walk(container, func(child *tree_sitter.Node) {
		if child.Equals(*container) {
			return
		}

		switch child.Kind() {
		case pythonKindFunctionDef:
			if ancestor := nearestPythonFunctionAncestor(
				child,
			); ancestor != nil &&
				ancestor.Equals(*container) {
				name := pythonFunctionName(child, contents)
				if name != "" {
					nestedByName[name] = append(
						nestedByName[name],
						pythonNodeKey(child, contents),
					)
				}
			}
		case "return_statement", pythonKindAssign, pythonKindAnnotatedAssign:
			if name, found := pythonStatementReferencedIdentifier(child, contents); found {
				referenced[name] = true
			}
		}
	})

	factories := map[string]bool{}

	for name, keys := range nestedByName {
		if !referenced[name] {
			continue
		}

		for _, key := range keys {
			factories[key] = true
		}
	}

	return factories
}

func nearestPythonFunctionAncestor(node *tree_sitter.Node) *tree_sitter.Node {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Kind() == pythonKindFunctionDef {
			return ancestor
		}

		if ancestor.Kind() == pythonKindModule {
			return nil
		}
	}

	return nil
}

func pythonSymbolKind(node *tree_sitter.Node) string {
	switch node.Kind() {
	case pythonKindFunctionDef:
		return "function"
	case pythonKindClassDef:
		return "class"
	case pythonKindComment:
		return "comment"
	case "lambda":
		return "lambda"
	case "import_statement", "import_from_statement":
		return "import"
	case pythonKindCall:
		return pythonSymbolCall
	case "except_clause":
		return "except"
	default:
		return node.Kind()
	}
}

func pythonNodeSymbolName(node *tree_sitter.Node, contents []byte) string {
	switch node.Kind() {
	case pythonKindFunctionDef, pythonKindClassDef:
		return pythonFunctionName(node, contents)
	case pythonKindCall:
		return pythonCallName(node, contents)
	default:
		return ""
	}
}

func pythonSymbolPaths(node *tree_sitter.Node, contents []byte) (string, string) {
	parts := []string{}

	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Kind() != pythonKindFunctionDef &&
			ancestor.Kind() != pythonKindClassDef {
			continue
		}

		name := pythonFunctionName(ancestor, contents)
		if name != "" {
			parts = append([]string{name}, parts...)
		}
	}

	parent := strings.Join(parts, ".")

	name := pythonNodeSymbolName(node, contents)
	switch {
	case name == "":
		return parent, parent
	case parent == "":
		return name, ""
	default:
		return parent + "." + name, parent
	}
}

func pythonEnclosingFunction(
	node *tree_sitter.Node,
	contents []byte,
) (string, string) {
	function := nearestPythonFunctionAncestor(node)
	if function == nil {
		return "", ""
	}

	return pythonFunctionName(function, contents), pythonNodeKey(function, contents)
}

func pythonSuppressionComment(text string) (bool, string) {
	for _, pattern := range pythonSuppressionCommentPatterns {
		if pattern.regex.MatchString(text) {
			return true, pattern.label
		}
	}

	return false, ""
}

func pythonSuppressionIsUnexplainedTypeIgnore(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.Contains(strings.ToLower(trimmed), "type: ignore") {
		return false
	}

	return pythonUnexplainedTypeIgnorePattern.MatchString(trimmed)
}

func pythonImportTargetsProtectedPackage(text string) bool {
	trimmed := strings.TrimSpace(text)

	return strings.HasPrefix(trimmed, "from coding_ethos.") ||
		strings.HasPrefix(trimmed, "import coding_ethos.")
}

func pythonFunctionName(node *tree_sitter.Node, contents []byte) string {
	name := node.ChildByFieldName("name")
	if name == nil {
		return ""
	}

	return strings.TrimSpace(name.Utf8Text(contents))
}

func pythonCallName(node *tree_sitter.Node, contents []byte) string {
	function := node.ChildByFieldName("function")
	if function == nil {
		return ""
	}

	return strings.TrimSpace(function.Utf8Text(contents))
}

func pythonLoggerCallParts(callName string) (string, string) {
	receiver, method, found := strings.Cut(callName, ".")
	if !found {
		return "", ""
	}

	switch receiver {
	case "logger", "_logger", "log", "_log":
	default:
		return "", ""
	}

	switch method {
	case "debug", "info", "warning", "error", "critical":
		return receiver, method
	default:
		return "", ""
	}
}

func pythonCallHasUnstructuredLogMessage(
	node *tree_sitter.Node,
	contents []byte,
	loggerName string,
	loggerMethod string,
) bool {
	if loggerName == "" || loggerMethod == "" {
		return false
	}

	text := strings.TrimSpace(node.Utf8Text(contents))
	_, args, found := strings.Cut(text, "(")

	if !found {
		return false
	}

	args = strings.TrimSpace(strings.TrimSuffix(args, ")"))

	return strings.HasPrefix(args, "f\"") ||
		strings.HasPrefix(args, "f'") ||
		strings.Contains(args, ".format(") ||
		strings.Contains(args, "%")
}

func pythonReturnAnnotation(node *tree_sitter.Node, contents []byte) string {
	returnType := node.ChildByFieldName("return_type")
	if returnType != nil {
		return strings.TrimSpace(returnType.Utf8Text(contents))
	}

	text := node.Utf8Text(contents)
	header, _, _ := strings.Cut(text, ":")

	_, annotation, found := strings.Cut(header, "->")
	if !found {
		return ""
	}

	return strings.TrimSpace(annotation)
}

func pythonAnnotationIsOptional(annotation string) bool {
	normalized := strings.ReplaceAll(annotation, " ", "")

	return strings.Contains(normalized, "|None") ||
		strings.Contains(normalized, "None|") ||
		strings.Contains(normalized, "Optional[") ||
		strings.Contains(normalized, "Union[") &&
			strings.Contains(normalized, "None")
}

func pythonExceptionType(node *tree_sitter.Node, contents []byte) string {
	text := strings.TrimSpace(node.Utf8Text(contents))
	if text == "" {
		return ""
	}

	header, _, _ := strings.Cut(text, ":")
	header = strings.TrimSpace(strings.TrimPrefix(header, "except"))
	header, _, _ = strings.Cut(header, " as ")

	return strings.TrimSpace(header)
}

func pythonExceptionAction(node *tree_sitter.Node, contents []byte) string {
	body := node.ChildByFieldName("body")
	if body != nil {
		for index := range body.NamedChildCount() {
			child := body.NamedChild(index)
			if child == nil {
				continue
			}

			text := strings.TrimSpace(child.Utf8Text(contents))
			if text != "" {
				return text
			}
		}
	}

	for line := range strings.SplitSeq(node.Utf8Text(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "except") {
			continue
		}

		return trimmed
	}

	return ""
}

func pythonExceptionActionIsSilent(action string) bool {
	switch strings.TrimSpace(action) {
	case "pass", "return", "return None":
		return true
	default:
		return false
	}
}

func pythonFunctionParameters(node *tree_sitter.Node) (int, bool, bool) {
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return 0, false, false
	}

	count := 0
	hasVarargs := false
	hasKwargs := false

	childCount := parameters.NamedChildCount()
	for index := range childCount {
		child := parameters.NamedChild(index)
		switch child.Kind() {
		case "identifier", "default_parameter", "typed_parameter",
			"typed_default_parameter", "list_splat_pattern",
			"dictionary_splat_pattern":
			count++
		}

		if child.Kind() == "list_splat_pattern" {
			hasVarargs = true
		}

		if child.Kind() == "dictionary_splat_pattern" {
			hasKwargs = true
		}
	}

	return count, hasVarargs, hasKwargs
}

func pythonIfStatementConditionHasTypeChecking(
	node *tree_sitter.Node,
	contents []byte,
) bool {
	condition := node.ChildByFieldName("condition")

	return condition != nil &&
		strings.Contains(condition.Utf8Text(contents), "TYPE_CHECKING")
}

func pythonStatementReferencedIdentifier(
	node *tree_sitter.Node,
	contents []byte,
) (string, bool) {
	switch node.Kind() {
	case "return_statement":
		if node.NamedChildCount() == 0 {
			return "", false
		}

		return pythonIdentifierText(node.NamedChild(0), contents)
	case pythonKindAssign, pythonKindAnnotatedAssign:
		right := node.ChildByFieldName("right")
		if right == nil && node.NamedChildCount() > 0 {
			right = node.NamedChild(node.NamedChildCount() - 1)
		}

		return pythonIdentifierText(right, contents)
	default:
		return "", false
	}
}

func pythonIdentifierText(node *tree_sitter.Node, contents []byte) (string, bool) {
	if node == nil || node.Kind() != "identifier" {
		return "", false
	}

	name := strings.TrimSpace(node.Utf8Text(contents))

	return name, name != ""
}

func pythonNodeKey(node *tree_sitter.Node, contents []byte) string {
	line, endLine, _ := astfacts.NodeRowSpan(node)

	return fmt.Sprintf(
		"%d:%d:%d:%s",
		line,
		endLine,
		node.StartByte(),
		pythonFunctionName(node, contents),
	)
}

func newPythonASTIssueFromFact(
	fact pythonASTFact,
	code string,
	detail string,
) *pythonASTIssue {
	return &pythonASTIssue{
		File:             fact.File,
		Line:             fact.Line,
		Column:           fact.Column,
		EndLine:          fact.EndLine,
		Code:             code,
		Detail:           detail,
		Language:         fact.Language,
		NodeKind:         fact.NodeKind,
		Snippet:          fact.Text,
		SymbolKind:       fact.SymbolKind,
		SymbolName:       fact.SymbolName,
		SymbolPath:       fact.SymbolPath,
		ParentSymbolPath: fact.ParentSymbolPath,
	}
}

func pythonDecisionWithIssue(
	policyDef policy.Policy,
	source pythonSource,
	issue pythonASTIssue,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     policyDef.ID,
		File:     source.Path,
		Line:     issue.Line,
		Column:   issue.Column,
		Severity: blockDecision,
		Code:     issue.Code,
		PolicyID: policyDef.ID,
		Message:  policyDef.Message,
		Advice:   policyDef.Suggestion,
		Detail:   issue.Detail,
		Metadata: map[string]any{
			"ast_change_source":      "source",
			"ast_end_line":           issue.EndLine,
			"ast_language":           issue.Language,
			"ast_node_kind":          issue.NodeKind,
			"ast_parent_symbol_path": issue.ParentSymbolPath,
			"ast_symbol_kind":        issue.SymbolKind,
			"ast_symbol_name":        issue.SymbolName,
			"ast_symbol_path":        issue.SymbolPath,
		},
	}}

	decision.Evidence = map[string]any{
		"line":                   issue.Line,
		"column":                 issue.Column,
		"snippet":                issue.Snippet,
		"ast_change_source":      "source",
		"ast_end_line":           issue.EndLine,
		"ast_language":           issue.Language,
		"ast_node_kind":          issue.NodeKind,
		"ast_parent_symbol_path": issue.ParentSymbolPath,
		"ast_symbol_kind":        issue.SymbolKind,
		"ast_symbol_name":        issue.SymbolName,
		"ast_symbol_path":        issue.SymbolPath,
		"detail":                 issue.Detail,
	}
	if source.Path != "" {
		decision.Evidence["file"] = source.Path
	}

	return decision
}
