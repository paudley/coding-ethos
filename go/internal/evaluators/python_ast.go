// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/policy"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

type pythonASTFact struct {
	File              string
	Language          string
	NodeKind          string
	SymbolKind        string
	SymbolName        string
	SymbolPath        string
	ParentSymbolPath  string
	Text              string
	ImportModule      string
	CallName          string
	AnnotationRole    string
	Line              int
	Column            int
	EndLine           int
	ParameterCount    int
	HasVarargs        bool
	HasKwargs         bool
	ModuleLevel       bool
	UnderClass        bool
	UnderConditional  bool
	UnderFunction     bool
	UnderTry          bool
	UnderTypeChecking bool
	IsImport          bool
	IsImportFallback  bool
	IsDynamicImport   bool
	IsAssignedLambda  bool
	IsClosureFactory  bool
}

type pythonASTIssue struct {
	File             string
	Line             int
	Column           int
	EndLine          int
	Code             string
	Detail           string
	Language         string
	NodeKind         string
	Snippet          string
	SymbolKind       string
	SymbolName       string
	SymbolPath       string
	ParentSymbolPath string
}

type pythonASTIssueFunc func([]pythonASTFact) *pythonASTIssue

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
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(
		tree_sitter.NewLanguage(tree_sitter_python.Language()),
	); err != nil {
		return nil, fmt.Errorf("set python tree-sitter language for %s: %w", source.Path, err)
	}
	tree := parser.Parse(contents, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse python source %s with tree-sitter returned nil tree", source.Path)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return pythonSnippetFallbackASTFacts(source), nil
	}

	facts := []pythonASTFact{}
	astfacts.Walk(root, func(node *tree_sitter.Node) {
		if fact, ok := pythonASTFactFromNode(source, node, contents); ok {
			facts = append(facts, fact)
		}
	})
	facts = append(facts, pythonSnippetFallbackASTFacts(source)...)

	return facts, nil
}

func pythonSnippetFallbackASTFacts(source pythonSource) []pythonASTFact {
	facts := []pythonASTFact{}
	for index, line := range strings.Split(source.Text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if fact, ok := pythonSnippetFallbackFact(source, line, trimmed, index+1); ok {
			facts = append(facts, fact)
		}
	}

	return facts
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
		Language:    "python",
		Text:        trimmed,
		Line:        lineNumber,
		Column:      indent + 1,
		EndLine:     lineNumber,
		ModuleLevel: indent == 0,
	}
	switch {
	case strings.HasPrefix(trimmed, "import "):
		fact.NodeKind = "import_statement"
		fact.SymbolKind = "import"
		fact.ImportModule = trimmed
		fact.IsImport = true
		fact.UnderFunction = indent > 0
		fact.UnderConditional = indent > 0

		return fact, true
	case strings.HasPrefix(trimmed, "from "):
		fact.NodeKind = "import_from_statement"
		fact.SymbolKind = "import"
		fact.ImportModule = trimmed
		fact.IsImport = true
		fact.UnderFunction = indent > 0
		fact.UnderConditional = indent > 0

		return fact, true
	case strings.HasPrefix(trimmed, "except ") &&
		(strings.Contains(trimmed, "ImportError") ||
			strings.Contains(trimmed, "ModuleNotFoundError")):
		fact.NodeKind = "except_clause"
		fact.SymbolKind = "except"
		fact.IsImportFallback = true

		return fact, true
	case strings.Contains(trimmed, "importlib.import_module("):
		fact.NodeKind = "call"
		fact.SymbolKind = "call"
		fact.CallName = "importlib.import_module"
		fact.IsDynamicImport = true

		return fact, true
	case strings.Contains(trimmed, "__import__("):
		fact.NodeKind = "call"
		fact.SymbolKind = "call"
		fact.CallName = "__import__"
		fact.IsDynamicImport = true

		return fact, true
	case strings.HasPrefix(trimmed, "def __getattr__("):
		fact.NodeKind = "function_definition"
		fact.SymbolKind = "function"
		fact.SymbolName = "__getattr__"
		fact.SymbolPath = "__getattr__"

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
				"type-checking-import",
				"TYPE_CHECKING import branches mask required runtime dependencies and are forbidden by the import policy.",
			)
		case fact.IsImport && !fact.ModuleLevel:
			return newPythonASTIssueFromFact(
				fact,
				"conditional-import",
				"Required imports must stay at module scope; runtime, nested, or branch-gated imports hide dependency and design failures.",
			)
		case fact.IsImportFallback:
			return newPythonASTIssueFromFact(
				fact,
				"import-error-fallback",
				"ImportError and ModuleNotFoundError fallback paths create soft dependencies instead of deterministic startup failure.",
			)
		case fact.NodeKind == "function_definition" &&
			fact.ModuleLevel &&
			fact.SymbolName == "__getattr__":
			return newPythonASTIssueFromFact(
				fact,
				"dynamic-getattr-import",
				"Module-level __getattr__ hides imports behind dynamic attribute lookup and bypasses deterministic dependency validation.",
			)
		case fact.IsDynamicImport:
			return newPythonASTIssueFromFact(
				fact,
				"dynamic-import-call",
				"Dynamic import calls bypass module-level dependency validation and are forbidden for required dependencies.",
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
				"assigned-lambda",
				"Assigned lambdas obscure reusable behavior; use functools.partial, operator helpers, or a named function.",
			)
		}
		if fact.IsClosureFactory {
			return newPythonASTIssueFromFact(
				fact,
				"closure-factory",
				fmt.Sprintf(
					"Nested function %q is returned or assigned from its container; prefer functools.partial or an explicit helper.",
					fact.SymbolName,
				),
			)
		}
	}

	return nil
}

func pythonASTFactFromNode(
	source pythonSource,
	node *tree_sitter.Node,
	contents []byte,
) (pythonASTFact, bool) {
	kind := node.Kind()
	if !pythonASTNodeIsFactCandidate(kind) {
		return pythonASTFact{}, false
	}
	line, endLine, _ := astfacts.NodeRowSpan(node)
	text := strings.TrimSpace(node.Utf8Text(contents))
	fact := pythonASTFact{
		File:              source.Path,
		Language:          "python",
		NodeKind:          kind,
		SymbolKind:        pythonSymbolKind(node),
		SymbolName:        pythonNodeSymbolName(node, contents),
		Text:              text,
		Line:              line,
		Column:            int(node.StartPosition().Column) + 1,
		EndLine:           endLine,
		ModuleLevel:       pythonNodeIsModuleLevel(node),
		UnderClass:        pythonHasAncestorKind(node, "class_definition"),
		UnderConditional:  pythonHasAncestorKind(node, "if_statement", "for_statement", "while_statement", "match_statement"),
		UnderFunction:     pythonHasAncestorKind(node, "function_definition"),
		UnderTry:          pythonHasAncestorKind(node, "try_statement", "except_clause"),
		UnderTypeChecking: pythonUnderTypeChecking(node, contents),
	}
	fact.SymbolPath, fact.ParentSymbolPath = pythonSymbolPaths(node, contents)
	switch kind {
	case "import_statement", "import_from_statement":
		fact.IsImport = true
		fact.ImportModule = text
	case "except_clause":
		fact.IsImportFallback = strings.Contains(text, "ImportError") ||
			strings.Contains(text, "ModuleNotFoundError")
	case "call":
		fact.CallName = pythonCallName(node, contents)
		fact.IsDynamicImport = fact.CallName == "__import__" ||
			fact.CallName == "importlib.import_module"
	case "lambda":
		fact.IsAssignedLambda = pythonLambdaIsAssigned(node)
	case "function_definition":
		fact.ParameterCount, fact.HasVarargs, fact.HasKwargs = pythonFunctionParameters(node)
		fact.IsClosureFactory = pythonNestedFactoryIssue(node, contents)
	}

	return fact, true
}

func pythonASTNodeIsFactCandidate(kind string) bool {
	switch kind {
	case "annotated_assignment", "assignment", "call", "class_definition",
		"except_clause", "function_definition", "import_from_statement",
		"import_statement", "lambda":
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
	if parent.Kind() == "module" {
		return true
	}
	if parent.Kind() == "decorated_definition" {
		grandparent := parent.Parent()

		return grandparent != nil && grandparent.Kind() == "module"
	}

	return false
}

func pythonHasAncestorKind(node *tree_sitter.Node, kinds ...string) bool {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		for _, kind := range kinds {
			if ancestor.Kind() == kind {
				return true
			}
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
		case "assignment", "annotated_assignment":
			return true
		case "module", "function_definition", "class_definition":
			return false
		}
	}

	return false
}

func pythonNestedFactoryIssue(node *tree_sitter.Node, contents []byte) bool {
	container := nearestPythonFunctionAncestor(node)
	if container == nil {
		return false
	}
	name := pythonFunctionName(node, contents)
	if name == "" {
		return false
	}

	found := false
	astfacts.Walk(container, func(child *tree_sitter.Node) {
		if found || child.Equals(*node) {
			return
		}
		switch child.Kind() {
		case "return_statement", "assignment":
			if pythonStatementReferencesName(child, contents, name) {
				found = true
			}
		}
	})

	return found
}

func nearestPythonFunctionAncestor(node *tree_sitter.Node) *tree_sitter.Node {
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Kind() == "function_definition" {
			return ancestor
		}
		if ancestor.Kind() == "module" {
			return nil
		}
	}

	return nil
}

func pythonSymbolKind(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "function_definition":
		return "function"
	case "class_definition":
		return "class"
	case "lambda":
		return "lambda"
	case "import_statement", "import_from_statement":
		return "import"
	case "call":
		return "call"
	case "except_clause":
		return "except"
	default:
		return node.Kind()
	}
}

func pythonNodeSymbolName(node *tree_sitter.Node, contents []byte) string {
	switch node.Kind() {
	case "function_definition", "class_definition":
		return pythonFunctionName(node, contents)
	case "call":
		return pythonCallName(node, contents)
	default:
		return ""
	}
}

func pythonSymbolPaths(node *tree_sitter.Node, contents []byte) (string, string) {
	parts := []string{}
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Kind() != "function_definition" && ancestor.Kind() != "class_definition" {
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

func pythonFunctionParameters(node *tree_sitter.Node) (int, bool, bool) {
	parameters := node.ChildByFieldName("parameters")
	if parameters == nil {
		return 0, false, false
	}
	count := 0
	hasVarargs := false
	hasKwargs := false
	for index := uint(0); index < parameters.NamedChildCount(); index++ {
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

func pythonIfStatementConditionHasTypeChecking(node *tree_sitter.Node, contents []byte) bool {
	condition := node.ChildByFieldName("condition")

	return condition != nil && strings.Contains(condition.Utf8Text(contents), "TYPE_CHECKING")
}

func pythonStatementReferencesName(node *tree_sitter.Node, contents []byte, name string) bool {
	text := strings.TrimSpace(node.Utf8Text(contents))

	return text == "return "+name ||
		strings.HasPrefix(text, "return "+name+" ") ||
		strings.HasSuffix(text, " = "+name) ||
		strings.Contains(text, " = "+name+" ")
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
