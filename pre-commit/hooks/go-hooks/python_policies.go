// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

var (
	fileDocSentencePattern = regexp.MustCompile(`[.!?](?:\s|$)`)
	pytestSummaryPattern   = regexp.MustCompile(
		`(?P<passed>\d+) passed` +
			`(?:.*?(?P<skipped>\d+) skipped)?` +
			`(?:.*?(?P<xfailed>\d+) xfailed)?` +
			`(?:.*?(?P<failed>\d+) failed)?` +
			`(?:.*?(?P<errors>\d+) error)?`,
	)
	sqlSelectFromPattern      = regexp.MustCompile(`(?is)\bSELECT\b.+\bFROM\b`)
	sqlUpdateSetPattern       = regexp.MustCompile(`(?is)\bUPDATE\b.+\bSET\b`)
	sqlGrantOnPattern         = regexp.MustCompile(`(?is)\bGRANT\b.+\bON\b`)
	sqlRevokeOnPattern        = regexp.MustCompile(`(?is)\bREVOKE\b.+\bON\b`)
	sqlWhereClausePattern     = regexp.MustCompile(`(?is)\bWHERE\b.+[=<>]`)
	sqlInsertIntoPattern      = regexp.MustCompile(`(?i)\bINSERT\s+INTO\b`)
	sqlDeleteFromPattern      = regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`)
	sqlCreateTablePattern     = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\b`)
	sqlCreateIndexPattern     = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\b`)
	sqlCreateExtensionPattern = regexp.MustCompile(`(?i)\bCREATE\s+EXTENSION\b`)
	sqlCreateOrReplacePattern = regexp.MustCompile(`(?i)\bCREATE\s+OR\s+REPLACE\b`)
	sqlCreatePolicyPattern    = regexp.MustCompile(`(?i)\bCREATE\s+POLICY\b`)
	sqlCreateGraphPattern     = regexp.MustCompile(`(?i)\bCREATE\s+GRAPH\b`)
	sqlAlterTablePattern      = regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`)
	sqlDropTablePattern       = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)
	sqlDropIndexPattern       = regexp.MustCompile(`(?i)\bDROP\s+INDEX\b`)
	sqlDropPolicyPattern      = regexp.MustCompile(`(?i)\bDROP\s+POLICY\b`)
	sqlDropGraphPattern       = regexp.MustCompile(`(?i)\bDROP\s+GRAPH\b`)
	sqlTruncatePattern        = regexp.MustCompile(`(?i)\bTRUNCATE\s+\w+`)
	sqlEnableRLSPattern       = regexp.MustCompile(
		`(?i)\bENABLE\s+ROW\s+LEVEL\s+SECURITY\b`,
	)
	sqlForceRLSPattern = regexp.MustCompile(
		`(?i)\bFORCE\s+ROW\s+LEVEL\s+SECURITY\b`,
	)
	sqlSetLocalPattern      = regexp.MustCompile(`(?i)\bSET\s+LOCAL\b`)
	sqlSetSearchPathPattern = regexp.MustCompile(`(?i)\bSET\s+SEARCH_PATH\b`)
	sqlLoadExtensionPattern = regexp.MustCompile(`(?i)\bLOAD\s+'`)
	sqlExecuteFormatPattern = regexp.MustCompile(`(?i)\bEXECUTE\s+format\b`)
	sqlCypherCreatePattern  = regexp.MustCompile(`(?i)\bCREATE\s*\(`)
	sqlCypherMatchPattern   = regexp.MustCompile(`(?i)\bMATCH\s*\(`)
	sqlCypherMergePattern   = regexp.MustCompile(`(?i)\bMERGE\s*\(`)
	sqlCypherReturnPattern  = regexp.MustCompile(`(?i)\bRETURN\s+id\s*\(`)
	sqlParameterizedPattern = regexp.MustCompile(`\$\d+`)
	sqlValuesPattern        = regexp.MustCompile(`(?i)\bVALUES\s*\(`)
	sqlIfNotExistsPattern   = regexp.MustCompile(`(?i)\bIF\s+NOT\s+EXISTS\b`)
	sqlIfExistsPattern      = regexp.MustCompile(`(?i)\bIF\s+EXISTS\b`)
)

const (
	pytestGateMaxOutputLines  = 30
	fileDocDefaultSentences   = 3
	sqlDefaultMinStringLength = 15
	sqlMaxSnippetLength       = 80
)

type fileDocstringsSettings struct {
	ExemptFilenames []string
	MinSentences    int
	Enabled         bool
}

type pytestGateSettings struct {
	ConsumerRoot  string
	BannedMarkers []string
	TestCommand   []string
	Enabled       bool
}

type directImportsSettings struct {
	ConsumerRoot string
	Packages     []string
	SourcePaths  []string
	Enabled      bool
}

type utilCentralizationSettings struct {
	BannedModules []bannedUtilityModule
	Enabled       bool
}

type bannedUtilityModule struct {
	Module      string
	Alternative string
	ExemptPaths []string
}

type sqlCentralizationSettings struct {
	ModuleName           string
	CentralPaths         []string
	MigrationMarkers     []string
	ErrorContextKeywords []string
	MinStringLength      int
	Enabled              bool
}

type pythonImportAlias struct {
	Name  string
	Alias string
}

type pythonImportStatement struct {
	Kind     string
	Module   string
	Names    []pythonImportAlias
	Line     int
	Relative bool
}

type pythonMarkerViolation struct {
	File   string
	Marker string
	Line   int
}

type fileDocViolation struct {
	File   string
	Reason string
	Count  int
}

type directImportViolation struct {
	File       string
	Statement  string
	Suggestion string
	Line       int
}

type sqlViolation struct {
	File    string
	Pattern string
	Snippet string
	Line    int
}

type sqlPattern struct {
	Regex *regexp.Regexp
	Name  string
}

type pytestRunResult struct {
	Counts     map[string]int
	Stdout     string
	Stderr     string
	ReturnCode int
}

type structuredLoggingViolation struct {
	File    string
	Method  string
	Preview string
	Line    int
}

type conditionalImportViolation struct {
	File    string
	Module  string
	Pattern string
	Line    int
}

type typeCheckingViolation struct {
	File    string
	Pattern string
	Line    int
}

type catchSilenceViolation struct {
	File          string
	ExceptionType string
	HandlerBody   string
	Line          int
}

type optionalTypeViolation struct {
	File    string
	Context string
	Line    int
}

type securityViolation struct {
	File     string
	Category string
	Message  string
	Snippet  string
	Line     int
}

type structuredLoggingSettings struct {
	Methods      []string
	LoggerNames  []string
	ExemptKwargs []string
	Enabled      bool
}

type conditionalImportsSettings struct {
	CapabilityPrefix string
	ExceptionNames   []string
	Enabled          bool
}

type typeCheckingImportsSettings struct {
	FutureImportName  string
	TypeCheckingNames []string
	Enabled           bool
}

type catchSilenceSettings struct {
	Enabled bool
}

type optionalReturnsSettings struct {
	ExemptMethodNames []string
	Enabled           bool
}

type securityPatternsSettings struct {
	SQLKeywords              []string
	SecretPatterns           []string
	TestFileMarkers          []string
	MinGetenvArgsWithDefault int
	Enabled                  bool
}

func loadBundleConsumerAndConfig() (string, string, map[string]any, error) {
	bundleRoot, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return "", "", nil, err
	}

	return bundleRoot, consumerRoot(filepath.Dir(bundleRoot)), rootConfig, nil
}

func pythonLanguage() *ts.Language {
	return ts.NewLanguage(tspython.Language())
}

func sqlPatterns() []sqlPattern {
	return []sqlPattern{
		{Name: "SELECT...FROM", Regex: sqlSelectFromPattern},
		{Name: "INSERT INTO", Regex: sqlInsertIntoPattern},
		{Name: "DELETE FROM", Regex: sqlDeleteFromPattern},
		{Name: "UPDATE...SET", Regex: sqlUpdateSetPattern},
		{Name: "CREATE TABLE", Regex: sqlCreateTablePattern},
		{Name: "CREATE INDEX", Regex: sqlCreateIndexPattern},
		{Name: "CREATE EXTENSION", Regex: sqlCreateExtensionPattern},
		{Name: "CREATE OR REPLACE", Regex: sqlCreateOrReplacePattern},
		{Name: "CREATE POLICY", Regex: sqlCreatePolicyPattern},
		{Name: "CREATE GRAPH", Regex: sqlCreateGraphPattern},
		{Name: "ALTER TABLE", Regex: sqlAlterTablePattern},
		{Name: "DROP TABLE", Regex: sqlDropTablePattern},
		{Name: "DROP INDEX", Regex: sqlDropIndexPattern},
		{Name: "DROP POLICY", Regex: sqlDropPolicyPattern},
		{Name: "DROP GRAPH", Regex: sqlDropGraphPattern},
		{Name: "TRUNCATE", Regex: sqlTruncatePattern},
		{Name: "ENABLE RLS", Regex: sqlEnableRLSPattern},
		{Name: "FORCE RLS", Regex: sqlForceRLSPattern},
		{Name: "GRANT...ON", Regex: sqlGrantOnPattern},
		{Name: "REVOKE...ON", Regex: sqlRevokeOnPattern},
		{Name: "SET LOCAL", Regex: sqlSetLocalPattern},
		{Name: "SET SEARCH_PATH", Regex: sqlSetSearchPathPattern},
		{Name: "LOAD extension", Regex: sqlLoadExtensionPattern},
		{Name: "EXECUTE format", Regex: sqlExecuteFormatPattern},
		{Name: "Cypher CREATE", Regex: sqlCypherCreatePattern},
		{Name: "Cypher MATCH", Regex: sqlCypherMatchPattern},
		{Name: "Cypher MERGE", Regex: sqlCypherMergePattern},
		{Name: "Cypher RETURN", Regex: sqlCypherReturnPattern},
		{Name: "Parameterized $N", Regex: sqlParameterizedPattern},
		{Name: "VALUES(...)", Regex: sqlValuesPattern},
		{Name: "IF NOT EXISTS", Regex: sqlIfNotExistsPattern},
		{Name: "IF EXISTS", Regex: sqlIfExistsPattern},
		{Name: "WHERE clause", Regex: sqlWhereClausePattern},
	}
}

func decodeConfigSection(rootConfig map[string]any, path string, target any) error {
	value, ok := rootConfigValue(rootConfig, path)
	if !ok {
		return nil
	}

	return decodeYAMLValue(value, target)
}

func decodePolicySection(
	rootConfig map[string]any,
	path string,
	label string,
	target any,
) error {
	err := decodeConfigSection(rootConfig, path, target)
	if err != nil {
		return fmt.Errorf("parse %s config: %w", label, err)
	}

	return nil
}

func loadFileDocstringsSettings() (fileDocstringsSettings, error) {
	var settings fileDocstringsSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.file_docstrings",
		"file_docstrings",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.ExemptFilenames) == 0 {
		settings.ExemptFilenames = []string{"__init__.py", "conftest.py"}
	}
	if settings.MinSentences <= 0 {
		settings.MinSentences = fileDocDefaultSentences
	}

	return settings, nil
}

func loadPytestGateSettings() (pytestGateSettings, error) {
	var settings pytestGateSettings
	_, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.pytest_gate",
		"pytest_gate",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.BannedMarkers) == 0 {
		settings.BannedMarkers = []string{"skip", "skipif"}
	}
	if len(settings.TestCommand) == 0 {
		settings.TestCommand = []string{
			"uv",
			"run",
			"--frozen",
			"pytest",
			"tests",
			"--strict-markers",
		}
	}
	settings.ConsumerRoot = consumer

	return settings, nil
}

func loadDirectImportsSettings() (directImportsSettings, error) {
	var settings directImportsSettings
	_, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.direct_imports",
		"direct_imports",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.Packages) == 0 {
		settings.Packages = []string{"coding_ethos"}
	}
	if raw, ok := rootConfigValue(rootConfig, "python.source_paths"); ok {
		settings.SourcePaths = normalizeStringList(raw)
	}
	settings.ConsumerRoot = consumer

	return settings, nil
}

func loadUtilCentralizationSettings() (utilCentralizationSettings, error) {
	var settings utilCentralizationSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.util_centralization",
		"util_centralization",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	return settings, nil
}

func loadSQLCentralizationSettings() (sqlCentralizationSettings, error) {
	var settings sqlCentralizationSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.sql_centralization",
		"sql_centralization",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if strings.TrimSpace(settings.ModuleName) == "" {
		settings.ModuleName = "project.sql"
	}
	if len(settings.MigrationMarkers) == 0 {
		settings.MigrationMarkers = []string{"alembic", "migrations"}
	}
	if len(settings.ErrorContextKeywords) == 0 {
		settings.ErrorContextKeywords = []string{
			"suggestion",
			"reason",
			"message",
			"match",
			"extra",
		}
	}
	if settings.MinStringLength <= 0 {
		settings.MinStringLength = sqlDefaultMinStringLength
	}

	return settings, nil
}

func loadStructuredLoggingSettings() (structuredLoggingSettings, error) {
	var settings structuredLoggingSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.structured_logging",
		"structured_logging",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.Methods) == 0 {
		settings.Methods = []string{"debug", "info", "warning", "error", "critical"}
	}
	if len(settings.LoggerNames) == 0 {
		settings.LoggerNames = []string{"logger", "_logger", "log", "_log"}
	}
	if len(settings.ExemptKwargs) == 0 {
		settings.ExemptKwargs = []string{"exc_info", "stack_info", "stacklevel"}
	}

	return settings, nil
}

func loadConditionalImportsSettings() (conditionalImportsSettings, error) {
	var settings conditionalImportsSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.conditional_imports",
		"conditional_imports",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.ExceptionNames) == 0 {
		settings.ExceptionNames = []string{"ImportError", "ModuleNotFoundError"}
	}
	if strings.TrimSpace(settings.CapabilityPrefix) == "" {
		settings.CapabilityPrefix = "HAS_"
	}

	return settings, nil
}

func loadTypeCheckingImportsSettings() (typeCheckingImportsSettings, error) {
	var settings typeCheckingImportsSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.type_checking_imports",
		"type_checking_imports",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.TypeCheckingNames) == 0 {
		settings.TypeCheckingNames = []string{"TYPE_CHECKING"}
	}
	if strings.TrimSpace(settings.FutureImportName) == "" {
		settings.FutureImportName = "annotations"
	}

	return settings, nil
}

func loadCatchSilenceSettings() (catchSilenceSettings, error) {
	var settings catchSilenceSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.catch_and_silence",
		"catch_and_silence",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	return settings, nil
}

func loadOptionalReturnsSettings() (optionalReturnsSettings, error) {
	var settings optionalReturnsSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.optional_returns",
		"optional_returns",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.ExemptMethodNames) == 0 {
		settings.ExemptMethodNames = []string{"__exit__", "__aexit__"}
	}

	return settings, nil
}

func loadSecurityPatternsSettings() (securityPatternsSettings, error) {
	var settings securityPatternsSettings
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	err = decodePolicySection(
		rootConfig,
		"python.security_patterns",
		"security_patterns",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if len(settings.SQLKeywords) == 0 {
		settings.SQLKeywords = []string{
			"SELECT",
			"INSERT",
			"UPDATE",
			"DELETE",
			"DROP",
			"CREATE",
			"ALTER",
			"TRUNCATE",
			"EXECUTE",
			"EXEC",
		}
	}
	if len(settings.SecretPatterns) == 0 {
		settings.SecretPatterns = []string{
			"sk-",
			"pk-",
			"api_",
			"key_",
			"token_",
			"secret_",
			"password",
			"passwd",
			"credential",
		}
	}
	if len(settings.TestFileMarkers) == 0 {
		settings.TestFileMarkers = []string{"tests", "conftest", "test_", "_test.py"}
	}
	if settings.MinGetenvArgsWithDefault <= 0 {
		settings.MinGetenvArgsWithDefault = 2
	}

	return settings, nil
}

func parsePythonFile(path string) ([]byte, *ts.Tree, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	parser := ts.NewParser()
	defer parser.Close()
	err = parser.SetLanguage(pythonLanguage())
	if err != nil {
		return nil, nil, fmt.Errorf("set python parser language: %w", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, fmt.Errorf("%w: %s", errPythonParse, path)
	}

	return source, tree, nil
}

func pythonNodeText(node *ts.Node, source []byte) string {
	if node == nil {
		return ""
	}

	return strings.TrimSpace(node.Utf8Text(source))
}

func walkPythonNodes(node *ts.Node, visit func(*ts.Node)) {
	if node == nil {
		return
	}
	visit(node)
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	for i := range children {
		child := children[i]
		walkPythonNodes(&child, visit)
	}
}

func parsePythonImportAlias(node *ts.Node, source []byte) pythonImportAlias {
	if node == nil {
		return pythonImportAlias{}
	}
	if node.Kind() == "aliased_import" {
		return pythonImportAlias{
			Name:  pythonNodeText(node.ChildByFieldName("name"), source),
			Alias: pythonNodeText(node.ChildByFieldName("alias"), source),
		}
	}

	return pythonImportAlias{Name: pythonNodeText(node, source)}
}

func collectPythonImports(root *ts.Node, source []byte) []pythonImportStatement {
	imports := make([]pythonImportStatement, 0)
	walkPythonNodes(root, func(node *ts.Node) {
		statement, ok := pythonImportStatementFromNode(node, source)
		if ok {
			imports = append(imports, statement)
		}
	})

	return imports
}

func pythonImportStatementFromNode(
	node *ts.Node,
	source []byte,
) (pythonImportStatement, bool) {
	switch node.Kind() {
	case "import_statement":
		names := pythonImportAliases(node, source)
		if len(names) == 0 {
			return pythonImportStatement{}, false
		}

		return pythonImportStatement{
			Kind:  pythonNodeImport,
			Names: names,
			Line:  int(node.StartPosition().Row) + 1,
		}, true
	case pythonNodeImportFrom:
		moduleNode := node.ChildByFieldName("module_name")
		if moduleNode == nil {
			return pythonImportStatement{}, false
		}

		return pythonImportStatement{
			Kind:     "from",
			Module:   pythonNodeText(moduleNode, source),
			Names:    pythonImportAliases(node, source),
			Line:     int(node.StartPosition().Row) + 1,
			Relative: moduleNode.Kind() == "relative_import",
		}, true
	default:
		return pythonImportStatement{}, false
	}
}

func pythonImportAliases(node *ts.Node, source []byte) []pythonImportAlias {
	cursor := node.Walk()
	defer cursor.Close()
	nameNodes := node.ChildrenByFieldName("name", cursor)
	names := make([]pythonImportAlias, 0, len(nameNodes))
	for nameIndex := range nameNodes {
		name := parsePythonImportAlias(&nameNodes[nameIndex], source)
		if strings.TrimSpace(name.Name) != "" {
			names = append(names, name)
		}
	}

	return names
}

func pythonAttributeChain(node *ts.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case pythonNodeCall:
		return pythonAttributeChain(node.ChildByFieldName("function"), source)
	case pythonNodeAttribute:
		chain := pythonAttributeChain(node.ChildByFieldName("object"), source)
		attr := pythonNodeText(node.ChildByFieldName(pythonNodeAttribute), source)
		if attr != "" {
			chain = append(chain, attr)
		}

		return chain
	case pythonNodeIdentifier:
		text := pythonNodeText(node, source)
		if text == "" {
			return nil
		}

		return []string{text}
	default:
		return nil
	}
}

func findPytestMarkerViolations(
	path string,
	settings pytestGateSettings,
) ([]pythonMarkerViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	banned := stringSet(settings.BannedMarkers)
	violations := make([]pythonMarkerViolation, 0)
	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if node.Kind() != "decorator" {
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) == 0 {
			return
		}
		chain := pythonAttributeChain(&children[0], source)
		if len(chain) < minCollectionItems || chain[len(chain)-2] != "mark" {
			return
		}
		marker := chain[len(chain)-1]
		if banned[marker] {
			violations = append(violations, pythonMarkerViolation{
				File:   path,
				Line:   int(node.StartPosition().Row) + 1,
				Marker: "pytest.mark." + marker,
			})
		}
	})

	return violations, nil
}

func runPytestCommand(settings pytestGateSettings) (pytestRunResult, error) {
	result := pytestRunResult{
		Counts: map[string]int{
			"passed":  0,
			"skipped": 0,
			"xfailed": 0,
			"failed":  0,
			"errors":  0,
		},
	}
	if len(settings.TestCommand) == 0 {
		return result, errPytestGateCommandEmpty
	}
	cmd := exec.CommandContext(
		context.Background(),
		settings.TestCommand[0],
		settings.TestCommand[1:]...,
	)
	cmd.Dir = settings.ConsumerRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ReturnCode = exitErr.ExitCode()
			result.Counts = parsePytestSummary(result.Stdout)

			return result, nil
		}

		return result, fmt.Errorf("run pytest gate command: %w", err)
	}
	result.Counts = parsePytestSummary(result.Stdout)

	return result, nil
}

func parsePytestSummary(output string) map[string]int {
	counts := map[string]int{
		"passed":  0,
		"skipped": 0,
		"xfailed": 0,
		"failed":  0,
		"errors":  0,
	}
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		match := pytestSummaryPattern.FindStringSubmatch(lines[i])
		if match == nil {
			continue
		}
		names := pytestSummaryPattern.SubexpNames()
		for idx, name := range names {
			if idx == 0 || name == "" || match[idx] == "" {
				continue
			}
			value, err := strconv.Atoi(match[idx])
			if err != nil {
				continue
			}
			counts[name] = value
		}

		break
	}

	return counts
}

func pythonFileModulePath(path string) string {
	parts := make([]string, 0)
	current := filepath.Clean(filepath.Dir(path))
	for current != "." && current != "/" {
		_, err := os.Stat(filepath.Join(current, "__init__.py"))
		if err == nil {
			parts = append([]string{filepath.Base(current)}, parts...)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent

			continue
		}

		break
	}

	return strings.Join(parts, ".")
}

func pythonTopLevelPackage(path string) string {
	module := pythonFileModulePath(path)
	if module == "" {
		return ""
	}

	return strings.Split(module, ".")[0]
}

func isSamePackageFromImport(module string, fileModule string) bool {
	return fileModule != "" &&
		(strings.HasPrefix(module, fileModule) || strings.HasPrefix(fileModule, module))
}

func directImportSearchRoots(path string, settings directImportsSettings) []string {
	roots := make([]string, 0)
	seen := map[string]bool{}
	add := func(candidate string) {
		candidate = filepath.Clean(candidate)
		if candidate == "." || candidate == "" {
			candidate = settings.ConsumerRoot
		}
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(settings.ConsumerRoot, candidate)
		}
		candidate = filepath.Clean(candidate)
		if !seen[candidate] {
			seen[candidate] = true
			roots = append(roots, candidate)
		}
	}

	add(settings.ConsumerRoot)
	addSourceSearchRoots(add, settings)
	addTopLevelSearchRoot(add, path)

	return roots
}

func addSourceSearchRoots(
	add func(string),
	settings directImportsSettings,
) {
	for _, sourcePath := range settings.SourcePaths {
		sourcePath = strings.TrimSpace(sourcePath)
		if sourcePath == "" {
			continue
		}
		full := sourcePath
		if !filepath.IsAbs(full) {
			full = filepath.Join(settings.ConsumerRoot, full)
		}
		add(full)
		add(filepath.Dir(full))
	}
}

func addTopLevelSearchRoot(add func(string), path string) {
	topLevel := pythonTopLevelPackage(path)
	if topLevel == "" {
		return
	}

	current := filepath.Clean(filepath.Dir(path))
	for current != "." && current != "/" {
		if filepath.Base(current) == topLevel {
			add(filepath.Dir(current))

			return
		}
		parent := filepath.Dir(current)
		if parent == current {
			return
		}
		current = parent
	}
}

func resolvePythonModuleKind(module string, searchRoots []string) string {
	parts := strings.Split(strings.TrimSpace(module), ".")
	if len(parts) == 0 {
		return ""
	}
	for _, root := range searchRoots {
		modulePath := filepath.Join(append([]string{root}, parts...)...)
		info, err := os.Stat(modulePath + extPy)
		if err == nil && !info.IsDir() {
			return pythonNodeModule
		}
		info, err = os.Stat(filepath.Join(modulePath, "__init__.py"))
		if err == nil && !info.IsDir() {
			return "package"
		}
	}

	return ""
}

func statementImportNames(names []pythonImportAlias) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if name.Alias != "" {
			parts = append(parts, fmt.Sprintf("%s as %s", name.Name, name.Alias))

			continue
		}
		parts = append(parts, name.Name)
	}

	return strings.Join(parts, ", ")
}

func findDirectImportViolations(
	path string,
	settings directImportsSettings,
) ([]directImportViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	searchRoots := directImportSearchRoots(path, settings)
	packages := stringSet(settings.Packages)
	fileModule := pythonFileModulePath(path)
	topLevelPackage := pythonTopLevelPackage(path)
	imports := collectPythonImports(tree.RootNode(), source)
	violations := make([]directImportViolation, 0)

	for _, stmt := range imports {
		violations = append(
			violations,
			directImportViolationsForStatement(
				path,
				stmt,
				packages,
				fileModule,
				topLevelPackage,
				searchRoots,
			)...,
		)
	}

	return violations, nil
}

func directImportViolationsForStatement(
	path string,
	stmt pythonImportStatement,
	packages map[string]bool,
	fileModule string,
	topLevelPackage string,
	searchRoots []string,
) []directImportViolation {
	switch stmt.Kind {
	case "from":
		return directFromImportViolations(
			path,
			stmt,
			packages,
			fileModule,
			searchRoots,
		)
	case pythonNodeImport:
		return directImportStatementViolations(
			path,
			stmt,
			packages,
			topLevelPackage,
			searchRoots,
		)
	default:
		return nil
	}
}

func directFromImportViolations(
	path string,
	stmt pythonImportStatement,
	packages map[string]bool,
	fileModule string,
	searchRoots []string,
) []directImportViolation {
	if stmt.Relative || stmt.Module == "" {
		return nil
	}
	topLevel := strings.Split(stmt.Module, ".")[0]
	if !packages[topLevel] || isSamePackageFromImport(stmt.Module, fileModule) {
		return nil
	}
	if resolvePythonModuleKind(stmt.Module, searchRoots) != pythonNodeModule {
		return nil
	}
	moduleParts := strings.Split(stmt.Module, ".")
	if len(moduleParts) < minCollectionItems {
		return nil
	}
	parentModule := strings.Join(moduleParts[:len(moduleParts)-1], ".")
	names := statementImportNames(stmt.Names)

	return []directImportViolation{{
		File:       path,
		Line:       stmt.Line,
		Statement:  fmt.Sprintf("from %s import %s", stmt.Module, names),
		Suggestion: fmt.Sprintf("from %s import %s", parentModule, names),
	}}
}

func directImportStatementViolations(
	path string,
	stmt pythonImportStatement,
	packages map[string]bool,
	topLevelPackage string,
	searchRoots []string,
) []directImportViolation {
	violations := make([]directImportViolation, 0)
	for _, alias := range stmt.Names {
		module := alias.Name
		parts := strings.Split(module, ".")
		if len(parts) < minCollectionItems || !packages[parts[0]] {
			continue
		}
		if topLevelPackage != "" && parts[0] == topLevelPackage {
			continue
		}
		if resolvePythonModuleKind(module, searchRoots) != pythonNodeModule {
			continue
		}
		parentModule := strings.Join(parts[:len(parts)-1], ".")
		statement := "import " + module
		if alias.Alias != "" {
			statement += " as " + alias.Alias
		}
		violations = append(violations, directImportViolation{
			File:       path,
			Line:       stmt.Line,
			Statement:  statement,
			Suggestion: "import " + parentModule,
		})
	}

	return violations
}

func findBannedUtility(
	module string,
	bannedModules []bannedUtilityModule,
) *bannedUtilityModule {
	for i := range bannedModules {
		banned := &bannedModules[i]
		if module == banned.Module {
			return banned
		}
		if strings.Contains(banned.Module, ".") &&
			strings.HasPrefix(module, banned.Module+".") {
			return banned
		}
	}

	return nil
}

func isUtilityImportExempt(path string, banned bannedUtilityModule) bool {
	if len(banned.ExemptPaths) == 0 {
		return false
	}
	for _, marker := range banned.ExemptPaths {
		if marker != "" && strings.Contains(path, marker) {
			return true
		}
	}

	return false
}

func findUtilityViolations(
	path string,
	settings utilCentralizationSettings,
) ([]directImportViolation, error) {
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	imports := collectPythonImports(tree.RootNode(), source)
	violations := make([]directImportViolation, 0)
	for _, stmt := range imports {
		violations = append(
			violations,
			utilityViolationsForStatement(path, stmt, settings.BannedModules)...,
		)
	}

	return violations, nil
}

func utilityViolationsForStatement(
	path string,
	stmt pythonImportStatement,
	bannedModules []bannedUtilityModule,
) []directImportViolation {
	switch stmt.Kind {
	case pythonNodeImport:
		return utilityImportViolations(path, stmt, bannedModules)
	case "from":
		return utilityFromImportViolations(path, stmt, bannedModules)
	default:
		return nil
	}
}

func utilityImportViolations(
	path string,
	stmt pythonImportStatement,
	bannedModules []bannedUtilityModule,
) []directImportViolation {
	violations := make([]directImportViolation, 0)
	for _, alias := range stmt.Names {
		banned := findBannedUtility(alias.Name, bannedModules)
		if banned == nil || isUtilityImportExempt(path, *banned) {
			continue
		}
		statement := "import " + alias.Name
		if alias.Alias != "" {
			statement += " as " + alias.Alias
		}
		violations = append(violations, directImportViolation{
			File:       path,
			Line:       stmt.Line,
			Statement:  statement,
			Suggestion: banned.Alternative,
		})
	}

	return violations
}

func utilityFromImportViolations(
	path string,
	stmt pythonImportStatement,
	bannedModules []bannedUtilityModule,
) []directImportViolation {
	if stmt.Relative || stmt.Module == "" {
		return nil
	}
	if banned := findBannedUtility(stmt.Module, bannedModules); banned != nil &&
		!isUtilityImportExempt(path, *banned) {
		return []directImportViolation{{
			File: path,
			Line: stmt.Line,
			Statement: fmt.Sprintf(
				"from %s import %s",
				stmt.Module,
				statementImportNames(stmt.Names),
			),
			Suggestion: banned.Alternative,
		}}
	}

	violations := make([]directImportViolation, 0)
	for _, alias := range stmt.Names {
		qualified := stmt.Module + "." + alias.Name
		banned := findBannedUtility(qualified, bannedModules)
		if banned == nil || isUtilityImportExempt(path, *banned) {
			continue
		}
		name := alias.Name
		if alias.Alias != "" {
			name += " as " + alias.Alias
		}
		violations = append(violations, directImportViolation{
			File:       path,
			Line:       stmt.Line,
			Statement:  fmt.Sprintf("from %s import %s", stmt.Module, name),
			Suggestion: banned.Alternative,
		})
	}

	return violations
}

func sqlModuleHint(settings sqlCentralizationSettings) string {
	if len(settings.CentralPaths) > 0 {
		return settings.CentralPaths[0]
	}

	return strings.ReplaceAll(settings.ModuleName, ".", "/")
}

func isSQLExemptPath(path string, settings sqlCentralizationSettings) bool {
	markers := append(
		append([]string{}, settings.CentralPaths...),
		settings.MigrationMarkers...,
	)
	for _, marker := range markers {
		if marker != "" && strings.Contains(path, marker) {
			return true
		}
	}

	return false
}

func stringNodeLiteralText(node *ts.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case pythonNodeString:
		cursor := node.Walk()
		defer cursor.Close()
		children := node.Children(cursor)
		parts := make([]string, 0, len(children))
		for childIndex := range children {
			child := children[childIndex]
			switch child.Kind() {
			case "string_content", "escape_sequence":
				parts = append(parts, child.Utf8Text(source))
			case "interpolation":
				parts = append(parts, " ")
			}
		}
		if len(parts) == 0 {
			return node.Utf8Text(source)
		}

		return strings.Join(parts, "")
	case pythonNodeConcatString:
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		parts := make([]string, 0, len(children))
		for i := range children {
			parts = append(parts, stringNodeLiteralText(&children[i], source))
		}

		return strings.Join(parts, "")
	default:
		return ""
	}
}

func isStringDocstringOrStandalone(node *ts.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != pythonNodeExprStmt {
		return false
	}
	container := parent.Parent()
	if container == nil {
		return false
	}
	switch container.Kind() {
	case pythonNodeModule, pythonNodeBlock:
	default:
		return false
	}
	cursor := container.Walk()
	defer cursor.Close()
	children := container.NamedChildren(cursor)
	for childIndex := range children {
		child := children[childIndex]
		if !child.Equals(*parent) {
			continue
		}
		if childIndex == 0 {
			return true
		}
		prev := children[childIndex-1]
		if prev.Kind() != pythonNodeExprStmt {
			return false
		}
		prevExpr := prev.NamedChild(0)

		return prevExpr != nil && prevExpr.Kind() == pythonNodeAssignment
	}

	return false
}

func isStringErrorContext(
	node *ts.Node,
	settings sqlCentralizationSettings,
	source []byte,
) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != pythonNodeKeywordArg {
		return false
	}
	name := pythonNodeText(parent.ChildByFieldName("name"), source)

	return stringSet(settings.ErrorContextKeywords)[name]
}

func findSQLPattern(text string, settings sqlCentralizationSettings) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if len(collapsed) < settings.MinStringLength {
		return ""
	}
	for _, pattern := range sqlPatterns() {
		if pattern.Regex.MatchString(collapsed) {
			return pattern.Name
		}
	}

	return ""
}

func truncateSQLSnippet(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if len(collapsed) <= sqlMaxSnippetLength {
		return collapsed
	}

	return collapsed[:sqlMaxSnippetLength-3] + "..."
}

func findSQLViolations(
	path string,
	settings sqlCentralizationSettings,
) ([]sqlViolation, error) {
	if isSQLExemptPath(path, settings) {
		return nil, nil
	}
	source, tree, err := parsePythonFile(path)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	violations := make([]sqlViolation, 0)
	walkPythonNodes(tree.RootNode(), func(node *ts.Node) {
		if node.Kind() != pythonNodeString && node.Kind() != pythonNodeConcatString {
			return
		}
		parent := node.Parent()
		if parent != nil && parent.Kind() == pythonNodeConcatString {
			return
		}
		if isStringDocstringOrStandalone(node) ||
			isStringErrorContext(node, settings, source) {
			return
		}
		text := stringNodeLiteralText(node, source)
		pattern := findSQLPattern(text, settings)
		if pattern == "" {
			return
		}
		violations = append(violations, sqlViolation{
			File:    path,
			Line:    int(node.StartPosition().Row) + 1,
			Pattern: pattern,
			Snippet: truncateSQLSnippet(text),
		})
	})

	return violations, nil
}

func countDocstringSentences(text string) int {
	return len(fileDocSentencePattern.FindAllStringIndex(text, -1))
}

func checkSingleFileDocstring(
	path string,
	settings fileDocstringsSettings,
) (fileDocViolation, error) {
	docstring, err := extractModuleDocstringFromFile(path)
	if err != nil {
		return fileDocViolation{}, err
	}
	if strings.TrimSpace(docstring) == "" {
		return fileDocViolation{File: path, Reason: "missing module docstring"}, nil
	}
	count := countDocstringSentences(docstring)
	if count < settings.MinSentences {
		return fileDocViolation{
			File: path,
			Reason: fmt.Sprintf(
				"module docstring has %d sentence(s), need %d",
				count,
				settings.MinSentences,
			),
			Count: count,
		}, nil
	}

	return fileDocViolation{}, nil
}

func checkFileDocstringsCommand(_ Config, args []string) int {
	settings, err := loadFileDocstringsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	exempt := stringSet(settings.ExemptFilenames)
	violations := make([]fileDocViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy || exempt[filepath.Base(path)] {
			continue
		}
		violation, err := checkSingleFileDocstring(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		if violation.Reason != "" {
			violations = append(violations, violation)
		}
	}

	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "MODULE DOCSTRING CHECK FAILED (ETHOS §18)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintf(
		os.Stderr,
		"Per ETHOS §18 (Documentation as Contract): every Python file\n"+
			"must have a module-level docstring of at least %d sentences.\n\n",
		settings.MinSentences,
	)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "  %s: %s\n", violation.File, violation.Reason)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  Add a module-level docstring at the top of the file:")
	fmt.Fprintln(os.Stderr, `  """Brief summary of the module.`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  More detail about what the module provides. Include")
	fmt.Fprintln(os.Stderr, "  usage examples and important caveats.")
	fmt.Fprintln(os.Stderr, `  """`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}

func checkPytestGateCommand(_ Config, args []string) int {
	settings, err := loadPytestGateSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled {
		return 0
	}

	markerViolations := collectPytestGateMarkerViolations(args, settings)
	if len(markerViolations) > 0 {
		reportPytestMarkerViolations(markerViolations)

		return 1
	}

	fmt.Fprintln(os.Stderr, "Running pytest gate...")
	result, err := runPytestCommand(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	hasFailures := result.ReturnCode != 0
	hasSkips := result.Counts["skipped"] > 0
	if hasFailures || hasSkips {
		reportPytestGateFailureOutput(result)

		return 1
	}
	xfailNote := ""
	if result.Counts["xfailed"] > 0 {
		xfailNote = fmt.Sprintf(", %d xfailed", result.Counts["xfailed"])
	}
	fmt.Fprintf(
		os.Stderr,
		"Pytest gate passed: %d tests, 0 skipped%s.\n",
		result.Counts["passed"],
		xfailNote,
	)

	return 0
}

func collectPytestGateMarkerViolations(
	args []string,
	settings pytestGateSettings,
) []pythonMarkerViolation {
	markerViolations := make([]pythonMarkerViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		pathViolations, pathErr := findPytestMarkerViolations(path, settings)
		if pathErr != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, pathErr)

			continue
		}
		markerViolations = append(markerViolations, pathViolations...)
	}

	return markerViolations
}

func reportPytestGateFailure(result pytestRunResult) {
	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "PYTEST GATE FAILED (ETHOS §22)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	if result.ReturnCode != 0 {
		fmt.Fprintf(os.Stderr, "Pytest exited with code %d.\n", result.ReturnCode)
	}
	if result.Counts["failed"] > 0 {
		fmt.Fprintf(os.Stderr, "Failed tests: %d\n", result.Counts["failed"])
	}
	if result.Counts["errors"] > 0 {
		fmt.Fprintf(os.Stderr, "Errors: %d\n", result.Counts["errors"])
	}
	if result.Counts["skipped"] > 0 {
		fmt.Fprintf(os.Stderr, "Skipped tests: %d\n", result.Counts["skipped"])
	}
}

func reportPytestMarkerViolations(violations []pythonMarkerViolation) {
	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "BANNED PYTEST MARKERS DETECTED (ETHOS §22)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Per ETHOS §22: 100% pass rate is non-negotiable. Tests must",
	)
	fmt.Fprintln(
		os.Stderr,
		"not be skipped. Use @pytest.mark.xfail(reason='...') for known",
	)
	fmt.Fprintln(os.Stderr, "temporary failures instead.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: @%s\n",
			violation.File,
			violation.Line,
			violation.Marker,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  1. Remove the @pytest.mark.skip/skipif decorator")
	fmt.Fprintln(os.Stderr, "  2. Fix the test or the code it tests")
	fmt.Fprintln(
		os.Stderr,
		"  3. Use @pytest.mark.xfail(reason='...') for known gaps",
	)
	fmt.Fprintln(
		os.Stderr,
		"  4. If the test is truly obsolete, delete it entirely",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))
}

func reportPytestGateFailureOutput(result pytestRunResult) {
	reportPytestGateFailure(result)
	fmt.Fprintln(os.Stderr)
	printPytestGateLines(result.Stdout, true)
	if strings.TrimSpace(result.Stderr) != "" {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Stderr:")
		printPytestGateLines(result.Stderr, false)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "All tests must pass with zero skips.")
	fmt.Fprintln(os.Stderr, "Fix failing tests before committing.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))
}

func printPytestGateLines(output string, showTruncation bool) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > pytestGateMaxOutputLines {
		if showTruncation {
			fmt.Fprintf(
				os.Stderr,
				"... (%d lines truncated)\n",
				len(lines)-pytestGateMaxOutputLines,
			)
		}
		lines = lines[len(lines)-pytestGateMaxOutputLines:]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		}
	}
}

func checkDirectImportsCommand(_ Config, args []string) int {
	settings, err := loadDirectImportsSettings()
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
		found, err := findDirectImportViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "DIRECT MODULE IMPORT DETECTED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "Import from package __init__.py, not internal modules.")
	fmt.Fprintln(os.Stderr, "This ensures you use the package's public API.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "\n  %s:%d\n", violation.File, violation.Line)
		fmt.Fprintf(os.Stderr, "    Bad:  %s\n", violation.Statement)
		fmt.Fprintf(os.Stderr, "    Good: %s\n", violation.Suggestion)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
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

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "BANNED DIRECT IMPORT DETECTED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Production code must use the repository's configured utility",
	)
	fmt.Fprintln(
		os.Stderr,
		"wrapper modules instead of importing utility libraries directly.",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "\n  %s:%d\n", violation.File, violation.Line)
		fmt.Fprintf(os.Stderr, "    Bad:  %s\n", violation.Statement)
		fmt.Fprintf(os.Stderr, "    Good: %s\n", violation.Suggestion)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
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

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintf(os.Stderr, "SQL STRINGS FOUND OUTSIDE %s\n", settings.ModuleName)
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintf(
		os.Stderr,
		"All SQL, DDL, DML, and Cypher strings must live in %s.\n",
		settings.ModuleName,
	)
	fmt.Fprintf(
		os.Stderr,
		"Other modules import named constants from %s.\n\n",
		settings.ModuleName,
	)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: [%s] %s\n",
			violation.File,
			violation.Line,
			violation.Pattern,
			violation.Snippet,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintf(
		os.Stderr,
		"  1. Move the SQL string to %s as a Final[str] constant\n",
		sqlModuleHint(settings),
	)
	fmt.Fprintf(
		os.Stderr,
		"  2. Import it: from %s import MY_QUERY\n",
		settings.ModuleName,
	)
	fmt.Fprintf(
		os.Stderr,
		"  3. For dynamic queries, create a builder function in %s\n",
		settings.ModuleName,
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

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
				Line:    int(node.StartPosition().Row) + 1,
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
			Line:    int(node.StartPosition().Row) + 1,
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
				Line:    int(node.StartPosition().Row) + 1,
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
					Line:    int(node.StartPosition().Row) + 1,
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
				Line:    int(node.StartPosition().Row) + 1,
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
				Line:    int(node.StartPosition().Row) + 1,
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
				Line:          int(node.StartPosition().Row) + 1,
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
		Line:    int(node.StartPosition().Row) + 1,
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
		Line:    int(returnType.StartPosition().Row) + 1,
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
			Line: int(annotation.StartPosition().Row) + 1,
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
		return "<unknown>"
	}
	lines := strings.Split(string(content), "\n")
	if line < 1 || line > len(lines) {
		return "<unknown>"
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

	line := int(defaultNode.StartPosition().Row) + 1

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

	line := int(right.StartPosition().Row) + 1

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

	line := int(left.StartPosition().Row) + 1

	return securityViolation{
		File:     path,
		Line:     line,
		Category: "TEST_ENV_BYPASS",
		Message: "os.environ assignment in test file bypasses bootstrap validation. " +
			"Use fixtures that call bootstrap().",
		Snippet: sourceSnippet(path, line),
	}, true
}

func checkStructuredLoggingCommand(_ Config, args []string) int {
	settings, err := loadStructuredLoggingSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]structuredLoggingViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findStructuredLoggingViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "STRUCTURED LOGGING CHECK FAILED (ETHOS §11)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Per ETHOS §11 (Radical Visibility): every logger call must",
	)
	fmt.Fprintln(
		os.Stderr,
		"include keyword arguments for structured context. Bare string",
	)
	fmt.Fprintln(os.Stderr, "messages are insufficient for production observability.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Violations found (%d):\n", len(violations))
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: logger.%s(%q) — no structured context\n",
			violation.File,
			violation.Line,
			violation.Method,
			violation.Preview,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  Add keyword arguments for structured context:")
	fmt.Fprintln(os.Stderr, `    logger.info("event.name", key=value, other=data)`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  For exceptions, use exc_info or logger.exception():")
	fmt.Fprintln(
		os.Stderr,
		`    logger.error("operation.failed", error=str(exc), exc_info=True)`,
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}

func checkConditionalImportsCommand(_ Config, args []string) int {
	settings, err := loadConditionalImportsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]conditionalImportViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findConditionalImportViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "CONDITIONAL IMPORT CHECK FAILED (ETHOS §3)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Per ETHOS §3: if a module requires a library, that library must",
	)
	fmt.Fprintln(
		os.Stderr,
		"be present. Do not wrap imports in try/except to hide missing",
	)
	fmt.Fprintln(
		os.Stderr,
		"dependencies. The application must crash at the import stage.",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: conditional import of %q (%s)\n",
			violation.File,
			violation.Line,
			violation.Module,
			violation.Pattern,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  Remove the try/except and import directly:")
	fmt.Fprintln(os.Stderr, "    import some_library  # Crash if missing — good.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Add the dependency to pyproject.toml if needed.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}

func checkTypeCheckingImportsCommand(_ Config, args []string) int {
	settings, err := loadTypeCheckingImportsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]typeCheckingViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findTypeCheckingImportViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "STRING ANNOTATION PATTERN DETECTED (ETHOS §3, §12)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Both TYPE_CHECKING and `from __future__ import annotations` make",
	)
	fmt.Fprintln(
		os.Stderr,
		"types exist only at check time, not at runtime. TYPE_CHECKING",
	)
	fmt.Fprintln(
		os.Stderr,
		"creates a conditional import path. PEP 563 future annotations",
	)
	fmt.Fprintln(os.Stderr, "turn all annotations into lazy strings and break runtime")
	fmt.Fprintln(os.Stderr, "introspection. On Python 3.13+ neither pattern is needed.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: %s\n",
			violation.File,
			violation.Line,
			violation.Pattern,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  1. Remove `from __future__ import annotations`.")
	fmt.Fprintln(os.Stderr, "  2. Extract shared types into a shared protocols module.")
	fmt.Fprintln(
		os.Stderr,
		"  3. Use Protocol-first design or Dependency Inversion to break cycles.",
	)
	fmt.Fprintln(os.Stderr, "  4. Keep types runtime-visible.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}

func checkCatchAndSilenceCommand(_ Config, args []string) int {
	settings, err := loadCatchSilenceSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]catchSilenceViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findCatchSilenceViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "CATCH-AND-SILENCE CHECK FAILED (ETHOS §23)")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Per ETHOS §23: exceptions must never be silently swallowed.",
	)
	fmt.Fprintln(os.Stderr, "Every except handler must handle, transform+re-raise, or")
	fmt.Fprintln(os.Stderr, "log+re-raise the exception.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: except %s: %s\n",
			violation.File,
			violation.Line,
			violation.ExceptionType,
			violation.HandlerBody,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  Replace silencing patterns with proper handling:")
	fmt.Fprintln(os.Stderr, "    except SomeError as exc:")
	fmt.Fprintln(
		os.Stderr,
		`        logger.warning("operation_failed", error=str(exc))`,
	)
	fmt.Fprintln(os.Stderr, "        raise  # or raise DifferentError(...) from exc")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}

func checkOptionalReturnsCommand(_ Config, args []string) int {
	settings, err := loadOptionalReturnsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := make([]optionalTypeViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findOptionalTypeViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}
	if len(violations) == 0 {
		return 0
	}

	for _, violation := range violations {
		fmt.Fprintf(
			os.Stderr,
			"ERROR: %s:%d: %s\n",
			violation.File,
			violation.Line,
			violation.Context,
		)
	}
	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", compactDividerWidth))
	fmt.Fprintln(os.Stderr, "Optional type annotation check FAILED")
	fmt.Fprintln(
		os.Stderr,
		"All types must be non-optional. Use exceptions, not | None.",
	)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", compactDividerWidth))

	return 1
}

func checkSecurityPatternsCommand(_ Config, args []string) int {
	settings, err := loadSecurityPatternsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	violations := collectSecurityPatternViolations(args, settings)
	if len(violations) == 0 {
		return 0
	}

	reportSecurityPatternViolations(violations)

	return 1
}

func collectSecurityPatternViolations(
	args []string,
	settings securityPatternsSettings,
) []securityViolation {
	violations := make([]securityViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}
		found, err := findSecurityViolations(path, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", path, err)

			continue
		}
		violations = append(violations, found...)
	}

	return violations
}

func reportSecurityPatternViolations(violations []securityViolation) {
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", compactDividerWidth))
	fmt.Fprintln(os.Stderr, "SECURITY ANTI-PATTERNS DETECTED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", compactDividerWidth))
	for _, category := range securityViolationOrder() {
		printSecurityCategoryViolations(violations, category)
	}
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", compactDividerWidth))
}

func securityViolationOrder() []string {
	return []string{"DEFAULT_SECRET", "SQL_INJECTION", "TEST_ENV_BYPASS"}
}

func securityViolationDescriptions() map[string][2]string {
	return map[string][2]string{
		"SQL_INJECTION": {
			"SQL Injection Risk (ETHOS §24):",
			"Use parameterized queries instead of f-strings for SQL.",
		},
		"DEFAULT_SECRET": {
			"Default Secret Values (ETHOS §24):",
			"Remove default values from secret-related getenv() calls.",
		},
		"TEST_ENV_BYPASS": {
			"Test Environment Bypass (ETHOS §9):",
			"Use fixtures that call bootstrap() instead of direct env assignment.",
		},
	}
}

func printSecurityCategoryViolations(
	violations []securityViolation,
	category string,
) {
	categoryViolations := securityViolationsByCategory(violations, category)
	if len(categoryViolations) == 0 {
		return
	}

	title, description := securityViolationDescription(category)
	fmt.Fprintln(os.Stderr, title)
	fmt.Fprintf(os.Stderr, "  %s\n\n", description)
	for _, violation := range categoryViolations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: [%s] %s\n    > %s\n",
			violation.File,
			violation.Line,
			violation.Category,
			violation.Message,
			violation.Snippet,
		)
	}
	fmt.Fprintln(os.Stderr)
}

func securityViolationsByCategory(
	violations []securityViolation,
	category string,
) []securityViolation {
	filtered := make([]securityViolation, 0)
	for _, violation := range violations {
		if violation.Category == category {
			filtered = append(filtered, violation)
		}
	}

	return filtered
}

func securityViolationDescription(category string) (string, string) {
	descriptions := securityViolationDescriptions()
	title := descriptions[category][0]
	description := descriptions[category][1]
	if title == "" {
		return category + ":", "Security issue detected."
	}

	return title, description
}
