// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

import (
	goast "go/ast"
	"go/parser"
	"go/token"
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
		scope := hookCommandScope(symbol, calls)

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

func hookCommandScope(symbol astfacts.Symbol, calls []string) hookCommandScopeResult {
	for _, rule := range hookCommandScopeRules() {
		if !slices.Contains(rule.CommandFunctions, symbol.SymbolName) {
			continue
		}

		runIndex := firstMatchingCallIndex(calls, rule.PathSensitiveRunCalls)
		runsPathSensitive := runIndex >= 0

		return hookCommandScopeResult{
			CommandFunction:        true,
			RunsPathSensitiveCheck: runsPathSensitive,
			ChangedFileScopeBeforeRun: runsPathSensitive &&
				scopeGuardDominatesPathSensitiveRun(symbol.RawText, rule),
		}
	}

	return hookCommandScopeResult{}
}

func scopeGuardDominatesPathSensitiveRun(
	rawFunction string,
	rule hookCommandScopeRule,
) bool {
	fileSet := token.NewFileSet()

	parsed, err := parser.ParseFile(
		fileSet,
		"hook_command.go",
		[]byte("package hookrunnercli\n"+rawFunction),
		0,
	)
	if err != nil {
		return false
	}

	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*goast.FuncDecl)
		if ok && function.Body != nil {
			unsafe, _, _ := analyzeHookCommandStatements(function.Body.List, false, rule)

			return !unsafe
		}
	}

	return false
}

func analyzeHookCommandStatements(
	statements []goast.Stmt,
	scoped bool,
	rule hookCommandScopeRule,
) (bool, bool, bool) {
	alive := true

	for _, statement := range statements {
		if !alive {
			break
		}

		unsafe, nextScoped, nextAlive := analyzeHookCommandStatement(statement, scoped, rule)
		if unsafe {
			return true, nextScoped, nextAlive
		}

		scoped = nextScoped
		alive = nextAlive
	}

	return false, scoped, alive
}

func analyzeHookCommandStatement(
	statement goast.Stmt,
	scoped bool,
	rule hookCommandScopeRule,
) (bool, bool, bool) {
	switch typed := statement.(type) {
	case *goast.IfStmt:
		return analyzeHookCommandIfStatement(typed, scoped, rule)
	case *goast.BlockStmt:
		return analyzeHookCommandStatements(typed.List, scoped, rule)
	case *goast.ReturnStmt:
		unsafe, nextScoped := analyzeHookCommandNodeCalls(typed, scoped, rule)

		return unsafe, nextScoped, false
	default:
		unsafe, nextScoped := analyzeHookCommandNodeCalls(typed, scoped, rule)

		return unsafe, nextScoped, true
	}
}

func analyzeHookCommandIfStatement(
	statement *goast.IfStmt,
	scoped bool,
	rule hookCommandScopeRule,
) (bool, bool, bool) {
	if statement.Init != nil {
		unsafe, nextScoped, alive := analyzeHookCommandStatement(statement.Init, scoped, rule)
		if unsafe || !alive {
			return unsafe, nextScoped, alive
		}

		scoped = nextScoped
	}

	unsafe, _ := analyzeHookCommandNodeCalls(statement.Cond, scoped, rule)
	if unsafe {
		return true, scoped, true
	}

	bodyScoped := scoped || expressionIsScopeGuard(statement.Cond, rule.ScopeGuardCalls)
	elseScoped := scoped ||
		expressionIsNegatedScopeGuard(statement.Cond, rule.ScopeGuardCalls)

	bodyUnsafe, bodyOutScoped, bodyAlive := analyzeHookCommandStatements(
		statement.Body.List,
		bodyScoped,
		rule,
	)
	if bodyUnsafe {
		return true, bodyOutScoped, bodyAlive
	}

	elseUnsafe, elseOutScoped, elseAlive := analyzeHookCommandElse(
		statement.Else,
		elseScoped,
		rule,
	)
	if elseUnsafe {
		return true, elseOutScoped, elseAlive
	}

	return mergeHookCommandBranches(
		scoped,
		hookCommandBranch{Scoped: bodyOutScoped, Alive: bodyAlive},
		hookCommandBranch{Scoped: elseOutScoped, Alive: elseAlive},
	)
}

type hookCommandBranch struct {
	Scoped bool
	Alive  bool
}

func mergeHookCommandBranches(
	fallbackScoped bool,
	body hookCommandBranch,
	otherwise hookCommandBranch,
) (bool, bool, bool) {
	if body.Alive && otherwise.Alive {
		return false, body.Scoped && otherwise.Scoped, true
	}

	if body.Alive {
		return false, body.Scoped, true
	}

	if otherwise.Alive {
		return false, otherwise.Scoped, true
	}

	return false, fallbackScoped, false
}

func analyzeHookCommandElse(
	statement goast.Stmt,
	scoped bool,
	rule hookCommandScopeRule,
) (bool, bool, bool) {
	if statement == nil {
		return false, scoped, true
	}

	return analyzeHookCommandStatement(statement, scoped, rule)
}

func analyzeHookCommandNodeCalls(
	node goast.Node,
	scoped bool,
	rule hookCommandScopeRule,
) (bool, bool) {
	unsafe := false

	goast.Inspect(node, func(candidate goast.Node) bool {
		if unsafe {
			return false
		}

		if _, ok := candidate.(*goast.FuncLit); ok {
			return false
		}

		call, ok := candidate.(*goast.CallExpr)
		if !ok {
			return true
		}

		name := goCallName(call)
		switch {
		case slices.Contains(rule.ScopeGuardCalls, name):
			scoped = true
		case slices.Contains(rule.PathSensitiveRunCalls, name) && !scoped:
			unsafe = true
		}

		return true
	})

	return unsafe, scoped
}

func expressionIsScopeGuard(expression goast.Expr, names []string) bool {
	name := expressionCallName(expression)

	return name != "" && slices.Contains(names, name)
}

func expressionIsNegatedScopeGuard(expression goast.Expr, names []string) bool {
	unary, ok := expression.(*goast.UnaryExpr)
	if !ok || unary.Op.String() != "!" {
		return false
	}

	return expressionIsScopeGuard(unary.X, names)
}

func expressionCallName(expression goast.Expr) string {
	switch typed := expression.(type) {
	case *goast.CallExpr:
		return goCallName(typed)
	case *goast.ParenExpr:
		return expressionCallName(typed.X)
	default:
		return ""
	}
}

func goCallName(call *goast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *goast.Ident:
		return function.Name
	case *goast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
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
