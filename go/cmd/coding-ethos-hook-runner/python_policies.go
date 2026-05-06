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
	ExemptPaths  []string
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
	ExemptPaths          []string
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

	testPaths := []string{}

	err = decodeConfigSection(rootConfig, "python.test_paths", &testPaths)
	if err != nil {
		return settings, fmt.Errorf("parse sql_centralization test paths: %w", err)
	}

	settings.ExemptPaths = append(settings.ExemptPaths, testPaths...)

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
			Line:  treeSitterLine(node.StartPosition().Row),
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
			Line:     treeSitterLine(node.StartPosition().Row),
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
				Line:   treeSitterLine(node.StartPosition().Row),
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
	cmd.Env = externalToolEnv(nil)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

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

func isSamePackageFromImport(module, fileModule string) bool {
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
		append(append([]string{}, settings.CentralPaths...), settings.ExemptPaths...),
		settings.MigrationMarkers...,
	)

	normalizedPath := filepath.ToSlash(filepath.Clean(path))
	for _, marker := range markers {
		normalizedMarker := filepath.ToSlash(filepath.Clean(strings.TrimSpace(marker)))
		if normalizedMarker != "." && normalizedMarker != "" &&
			strings.Contains(normalizedPath, normalizedMarker) {
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
			Line:    treeSitterLine(node.StartPosition().Row),
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

	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "module_docstrings",
			File:    violation.File,
			Message: violation.Reason,
			Detail:  fmt.Sprintf("sentences=%d", violation.Count),
		})
	}

	fmt.Fprintln(os.Stderr, formatHookReport(hookReport{
		Tool:  "module_docstrings",
		Title: "MODULE DOCSTRING CHECK FAILED (ETHOS §18)",
		Summary: fmt.Sprintf(
			"Every Python file must have a module-level docstring of at least %d sentences.",
			settings.MinSentences,
		),
		Findings: findings,
		Guidance: []string{
			"Add a module-level docstring at the top of the file.",
			"Include a brief summary plus details about what the module provides.",
			"Include usage examples and important caveats where relevant.",
		},
	}, selectedHookOutputFormat()))

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

	if hookVerboseSuccessOutputEnabled() {
		fmt.Fprintln(os.Stderr, "Running pytest gate...")
	}

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

	if hookVerboseSuccessOutputEnabled() {
		fmt.Fprintf(
			os.Stderr,
			"Pytest gate passed: %d tests, 0 skipped%s.\n",
			result.Counts["passed"],
			xfailNote,
		)
	}

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

func reportPytestMarkerViolations(violations []pythonMarkerViolation) {
	findings := make([]hookFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, hookFinding{
			Tool:    "pytest_gate",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.Marker,
			Message: "banned pytest marker",
		})
	}

	fmt.Fprintln(os.Stderr, formatHookReport(hookReport{
		Tool:     "pytest_gate",
		Title:    "BANNED PYTEST MARKERS DETECTED (ETHOS §22)",
		Summary:  "Tests must not be skipped. Use xfail only for known temporary failures.",
		Findings: findings,
		Guidance: []string{
			"Remove the skip or skipif decorator.",
			"Fix the test or the code it tests.",
			"If the test is obsolete, delete it entirely.",
		},
	}, selectedHookOutputFormat()))
}

func reportPytestGateFailureOutput(result pytestRunResult) {
	findings := []hookFinding{{
		Tool:    "pytest_gate",
		Code:    fmt.Sprintf("exit-%d", result.ReturnCode),
		Message: "pytest gate failed",
		Detail:  trimmedPytestOutput(result),
	}}
	fmt.Fprintln(os.Stderr, formatHookReport(hookReport{
		Tool:  "pytest_gate",
		Title: "PYTEST GATE FAILED (ETHOS §22)",
		Summary: fmt.Sprintf(
			"failed=%d errors=%d skipped=%d return_code=%d",
			result.Counts["failed"],
			result.Counts["errors"],
			result.Counts["skipped"],
			result.ReturnCode,
		),
		Findings: findings,
		Guidance: []string{
			"All tests must pass with zero skips.",
			"Fix failing tests before committing.",
		},
	}, selectedHookOutputFormat()))
}

func trimmedPytestOutput(result pytestRunResult) string {
	output := strings.TrimSpace(result.Stdout)
	if strings.TrimSpace(result.Stderr) != "" {
		output = strings.TrimSpace(output + "\nStderr:\n" + strings.TrimSpace(result.Stderr))
	}

	lines := strings.Split(output, "\n")
	if len(lines) > pytestGateMaxOutputLines {
		lines = append(
			[]string{
				fmt.Sprintf("... (%d lines truncated)", len(lines)-pytestGateMaxOutputLines),
			},
			lines[len(lines)-pytestGateMaxOutputLines:]...,
		)
	}

	return strings.Join(lines, "\n")
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

		if isDirectImportExempt(path, settings) {
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

	fmt.Fprintln(os.Stderr, formatDirectImportReport(
		"direct_imports",
		"DIRECT MODULE IMPORT DETECTED",
		"Import from package __init__.py, not internal modules.",
		violations,
	))

	return 1
}
