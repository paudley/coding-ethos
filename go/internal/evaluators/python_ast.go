// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/policy"
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

	tree, ok, err := astfacts.Parse(source.Path, contents)
	if err != nil {
		return nil, fmt.Errorf(
			"parse python source %s with tree-sitter: %w",
			source.Path,
			err,
		)
	}

	if !ok {
		return pythonSnippetFallbackASTFacts(source), nil
	}

	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return pythonSnippetFallbackASTFacts(source), nil
	}

	facts := []pythonASTFact{}
	closureFactories := pythonClosureFactorySymbols(root, contents)
	astfacts.Walk(root, func(node *tree_sitter.Node) {
		if fact, ok := pythonASTFactFromNode(source, node, contents, closureFactories); ok {
			facts = append(facts, fact)
		}
	})

	if len(facts) == 0 || root.HasError() ||
		pythonSourceNeedsSnippetFallback(source.Text) {
		facts = append(facts, pythonSnippetFallbackASTFacts(source)...)
	}

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
		Language:    "python",
		NodeKind:    kind,
		SymbolKind:  pythonSymbolKind(node),
		SymbolName:  pythonNodeSymbolName(node, contents),
		Text:        text,
		Line:        line,
		Column:      int(node.StartPosition().Column) + 1,
		EndLine:     endLine,
		ModuleLevel: pythonNodeIsModuleLevel(node),
		UnderClass:  pythonHasAncestorKind(node, "class_definition"),
		UnderConditional: pythonHasAncestorKind(
			node,
			"if_statement",
			"for_statement",
			"while_statement",
			"match_statement",
		),
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
		fact.IsClosureFactory = closureFactories[pythonNodeKey(node, contents)]
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
		case "assignment", "annotated_assignment":
			return true
		case "module", "function_definition", "class_definition":
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
		if node.Kind() != "function_definition" {
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
		case "function_definition":
			if ancestor := nearestPythonFunctionAncestor(
				child,
			); ancestor != nil &&
				ancestor.Equals(*container) {
				name := pythonFunctionName(child, contents)
				if name != "" {
					nestedByName[name] = append(nestedByName[name], pythonNodeKey(child, contents))
				}
			}
		case "return_statement", "assignment", "annotated_assignment":
			if name, ok := pythonStatementReferencedIdentifier(child, contents); ok {
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
	case "assignment", "annotated_assignment":
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
