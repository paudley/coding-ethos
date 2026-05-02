// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"sync"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

type MetadataInput struct {
	AdminApproved bool   `json:"admin_approved"`
	SchemaVersion int64  `json:"schema_version"`
	Tool          string `json:"tool"`
}

type CommandInput struct {
	Argv         []string `json:"argv"`
	Raw          string   `json:"raw"`
	Tool         string   `json:"tool"`
	HasInlineEnv bool     `json:"has_inline_env"`
}

type EventInput struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Provider string `json:"provider"`
	Scope    string `json:"scope"`
	Tool     string `json:"tool"`
}

type PathInput struct {
	File         string `json:"file"`
	Dir          string `json:"dir"`
	Base         string `json:"base"`
	Ext          string `json:"ext"`
	IsTest       bool   `json:"is_test"`
	IsGenerated  bool   `json:"is_generated"`
	InSourceRoot bool   `json:"in_source_root"`
}

type DiagnosticInput struct {
	Tool     string `json:"tool"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file"`
	Line     int64  `json:"line"`
	Column   int64  `json:"column"`
	Severity string `json:"severity"`
	PolicyID string `json:"policy_id"`
}

type FindingInput struct {
	Tool         string   `json:"tool"`
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	File         string   `json:"file"`
	Line         int64    `json:"line"`
	Severity     string   `json:"severity"`
	PolicyID     string   `json:"policy_id"`
	SkillID      string   `json:"skill_id"`
	PrincipleIDs []string `json:"principle_ids"`
}

type RepoInput struct {
	ConfigCandidates  []string `json:"config_candidates"`
	ProtectedBranches []string `json:"protected_branches"`
	ProtectedPaths    []string `json:"protected_paths"`
	PythonVersion     string   `json:"python_version"`
	Root              string   `json:"root"`
	SourceRoots       []string `json:"source_roots"`
}

type ConfigInput struct {
	Candidates []string `json:"candidates"`
	Present    []string `json:"present"`
}

type GitInput struct {
	CurrentBranch      string   `json:"current_branch"`
	OnProtectedBranch  bool     `json:"on_protected_branch"`
	ProtectedBranches  []string `json:"protected_branches"`
	ProtectedPathFiles []string `json:"protected_path_files"`
	StagedFiles        []string `json:"staged_files"`
	ChangedFiles       []string `json:"changed_files"`
}

type DiffInput struct {
	ChangedFiles []string `json:"changed_files"`
	Files        []string `json:"files"`
	HasChanges   bool     `json:"has_changes"`
	StagedFiles  []string `json:"staged_files"`
}

type ActivationInput struct {
	Argv              []string
	Command           string
	ConfigCandidates  []string
	CurrentBranch     string
	Cwd               string
	EventName         string
	Files             []string
	ChangedFiles      []string
	StagedFiles       []string
	Scope             string
	Provider          string
	Mode              string
	Tool              string
	AdminApproved     bool
	Diagnostic        *diagnostics.Diagnostic
	Diagnostics       []diagnostics.Diagnostic
	Finding           *FindingActivation
	Findings          []FindingActivation
	ProtectedPaths    []string
	ProtectedBranches []string
	SourceRoots       []string
	PythonVersion     string
}

type FindingActivation struct {
	Tool         string
	Code         string
	Message      string
	File         string
	Severity     string
	PolicyID     string
	SkillID      string
	PrincipleIDs []string
	Column       int
	Line         int
}

const SchemaVersion int64 = 1

var (
	environmentOnce sync.Once
	environment     *cel.Env
	environmentErr  error
	programCache    sync.Map
)

type programCacheKey struct {
	PolicyID string
	Source   string
}

func InputSchema() []string {
	return []string{
		"argv: list(string)",
		"command: string",
		"command_fact: {raw, tool, argv, has_inline_env}",
		"config: {candidates, present}",
		"cwd: string",
		"diff: {files, changed_files, staged_files, has_changes}",
		"event: {name, provider, tool, scope, mode}",
		"files: list(string)",
		"git: {current_branch, on_protected_branch, protected_branches, protected_path_files, staged_files, changed_files}",
		"scope: string",
		"metadata: {admin_approved, schema_version, tool}",
		"path: {file, dir, base, ext, is_test, is_generated, in_source_root}",
		"paths: list({file, dir, base, ext, is_test, is_generated, in_source_root})",
		"diagnostic: {tool, code, message, file, line, column, severity, policy_id}",
		"diagnostics: list({tool, code, message, file, line, column, severity, policy_id})",
		"finding: {tool, code, message, file, line, severity, policy_id, skill_id, principle_ids}",
		"findings: list({tool, code, message, file, line, severity, policy_id, skill_id, principle_ids})",
		"repo: {root, source_roots, python_version, config_candidates, protected_paths, protected_branches}",
	}
}

func HelperSchema() []string {
	return []string{
		"has_prefix(value, prefix)",
		"has_suffix(value, suffix)",
		"glob_match(pattern, value)",
		"is_test_path(path)",
		"is_generated_path(path)",
		"is_protected_path(path, protected_paths)",
		"in_source_root(path, source_roots)",
		"lint_code_matches(code, pattern)",
		"command_invokes(command, tool)",
		"argv_invokes(argv, tool)",
		"argv_command_is(argv, tool)",
		"has_inline_env(command, name)",
		"repo_config_present(files, candidates)",
		"is_protected_branch(branch, protected_branches)",
		"list_contains(values, value)",
		"any_glob_match(patterns, value)",
		"any_has_prefix(values, prefix)",
		"any_has_suffix(values, suffix)",
	}
}

func Environment() (*cel.Env, error) {
	environmentOnce.Do(func() {
		environment, environmentErr = newEnvironment()
	})

	return environment, environmentErr
}

func newEnvironment() (*cel.Env, error) {
	options := []cel.EnvOption{
		ext.NativeTypes(
			reflect.TypeOf(MetadataInput{}),
			reflect.TypeOf(CommandInput{}),
			reflect.TypeOf(PathInput{}),
			reflect.TypeOf(DiagnosticInput{}),
			reflect.TypeOf(FindingInput{}),
			reflect.TypeOf(RepoInput{}),
			reflect.TypeOf(ConfigInput{}),
			reflect.TypeOf(GitInput{}),
			reflect.TypeOf(EventInput{}),
			reflect.TypeOf(DiffInput{}),
			ext.ParseStructTag("json"),
		),
		cel.Variable("argv", cel.ListType(cel.StringType)),
		cel.Variable("command", cel.StringType),
		cel.Variable("cwd", cel.StringType),
		cel.Variable("files", cel.ListType(cel.StringType)),
		cel.Variable("scope", cel.StringType),
		cel.Variable("metadata", cel.ObjectType("celexpr.MetadataInput")),
		cel.Variable("command_fact", cel.ObjectType("celexpr.CommandInput")),
		cel.Variable("event", cel.ObjectType("celexpr.EventInput")),
		cel.Variable("diff", cel.ObjectType("celexpr.DiffInput")),
		cel.Variable("path", cel.ObjectType("celexpr.PathInput")),
		cel.Variable(
			"paths",
			cel.ListType(cel.ObjectType("celexpr.PathInput")),
		),
		cel.Variable("diagnostic", cel.ObjectType("celexpr.DiagnosticInput")),
		cel.Variable(
			"diagnostics",
			cel.ListType(cel.ObjectType("celexpr.DiagnosticInput")),
		),
		cel.Variable("finding", cel.ObjectType("celexpr.FindingInput")),
		cel.Variable(
			"findings",
			cel.ListType(cel.ObjectType("celexpr.FindingInput")),
		),
		cel.Variable("repo", cel.ObjectType("celexpr.RepoInput")),
		cel.Variable("config", cel.ObjectType("celexpr.ConfigInput")),
		cel.Variable("git", cel.ObjectType("celexpr.GitInput")),
	}
	options = append(options, helperFunctions()...)

	return cel.NewEnv(options...)
}

func Validate(policyID string, source string) error {
	_, err := Program(policyID, source)

	return err
}

func Program(policyID string, source string) (cel.Program, error) {
	key := programCacheKey{
		PolicyID: policyID,
		Source:   strings.TrimSpace(source),
	}
	if cached, ok := programCache.Load(key); ok {
		return cached.(cel.Program), nil
	}

	program, err := compileProgram(key.PolicyID, key.Source)
	if err != nil {
		return nil, err
	}

	cached, _ := programCache.LoadOrStore(key, program)

	return cached.(cel.Program), nil
}

func compileProgram(policyID string, source string) (cel.Program, error) {
	if source == "" {
		return nil, fmt.Errorf("CEL expression policy %q missing when", policyID)
	}

	env, err := Environment()
	if err != nil {
		return nil, fmt.Errorf("prepare CEL environment for %q: %w", policyID, err)
	}

	ast, issues := env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile CEL policy %q: %w", policyID, issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf(
			"compile CEL policy %q: when expression must return bool, got %s",
			policyID,
			ast.OutputType(),
		)
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("prepare CEL program for %q: %w", policyID, err)
	}

	return program, nil
}

func Activation(input ActivationInput) map[string]any {
	sourceRoots := cleanSourceRoots(input.SourceRoots)
	paths := pathInputs(input.Files, sourceRoots)
	files := cleanStringSlice(input.Files)
	changedFiles := cleanStringSlice(input.ChangedFiles)
	stagedFiles := cleanStringSlice(input.StagedFiles)
	protectedPaths := cleanStringSlice(input.ProtectedPaths)
	protectedBranches := cleanStringSlice(input.ProtectedBranches)
	configCandidates := cleanStringSlice(input.ConfigCandidates)
	presentConfigs := presentRepoConfigs(files, configCandidates)
	primaryPath := PathInput{}
	if len(paths) == 1 {
		primaryPath = paths[0]
	} else if len(input.Files) == 1 {
		primaryPath = newPathInput(input.Files[0], sourceRoots)
	}

	return map[string]any{
		"argv":    append([]string(nil), input.Argv...),
		"command": input.Command,
		"command_fact": CommandInput{
			Argv:         append([]string(nil), input.Argv...),
			Raw:          input.Command,
			Tool:         input.Tool,
			HasInlineEnv: commandHasInlineEnv(input.Command, ""),
		},
		"config": ConfigInput{
			Candidates: configCandidates,
			Present:    presentConfigs,
		},
		"cwd": input.Cwd,
		"diff": DiffInput{
			ChangedFiles: changedFiles,
			Files:        files,
			HasChanges:   len(files) > 0 || len(changedFiles) > 0 || len(stagedFiles) > 0,
			StagedFiles:  stagedFiles,
		},
		"event": EventInput{
			Name:     input.EventName,
			Mode:     input.Mode,
			Provider: input.Provider,
			Scope:    input.Scope,
			Tool:     input.Tool,
		},
		"files": files,
		"git": GitInput{
			CurrentBranch:      input.CurrentBranch,
			OnProtectedBranch:  isProtectedBranch(input.CurrentBranch, protectedBranches),
			ChangedFiles:       changedFiles,
			ProtectedBranches:  protectedBranches,
			ProtectedPathFiles: protectedPathFiles(files, protectedPaths),
			StagedFiles:        stagedFiles,
		},
		"metadata": MetadataInput{
			AdminApproved: input.AdminApproved,
			SchemaVersion: SchemaVersion,
			Tool:          input.Tool,
		},
		"scope":       input.Scope,
		"path":        primaryPath,
		"paths":       paths,
		"diagnostic":  diagnosticInput(input.Diagnostic),
		"diagnostics": diagnosticInputs(input.Diagnostics, input.Diagnostic),
		"finding":     findingInput(input.Finding),
		"findings":    findingInputs(input.Findings, input.Finding),
		"repo": RepoInput{
			ConfigCandidates:  configCandidates,
			ProtectedBranches: protectedBranches,
			ProtectedPaths:    protectedPaths,
			PythonVersion:     input.PythonVersion,
			Root:              input.Cwd,
			SourceRoots:       sourceRoots,
		},
	}
}

func diagnosticInputs(
	diagnosticList []diagnostics.Diagnostic,
	primary *diagnostics.Diagnostic,
) []DiagnosticInput {
	inputs := make([]DiagnosticInput, 0, len(diagnosticList)+1)
	for _, diagnostic := range diagnosticList {
		inputs = append(inputs, diagnosticInput(&diagnostic))
	}
	if primary != nil && !diagnosticAlreadyPresent(inputs, primary) {
		inputs = append(inputs, diagnosticInput(primary))
	}

	return inputs
}

func diagnosticAlreadyPresent(
	inputs []DiagnosticInput,
	diagnostic *diagnostics.Diagnostic,
) bool {
	candidate := diagnosticInput(diagnostic)
	for _, input := range inputs {
		if input == candidate {
			return true
		}
	}

	return false
}

func findingInputs(
	findings []FindingActivation,
	primary *FindingActivation,
) []FindingInput {
	inputs := make([]FindingInput, 0, len(findings)+1)
	for _, finding := range findings {
		finding := finding
		inputs = append(inputs, findingInput(&finding))
	}
	if primary != nil {
		inputs = append(inputs, findingInput(primary))
	}

	return inputs
}

func pathInputs(files []string, sourceRoots []string) []PathInput {
	paths := make([]PathInput, 0, len(files))
	for _, file := range files {
		pathInput := newPathInput(file, sourceRoots)
		if pathInput.File != "" {
			paths = append(paths, pathInput)
		}
	}

	return paths
}

func newPathInput(file string, sourceRoots []string) PathInput {
	cleanFile := strings.TrimPrefix(path.Clean(strings.TrimSpace(file)), "./")
	if cleanFile == "." || cleanFile == "/" {
		cleanFile = ""
	}

	dir := ""
	base := ""
	ext := ""
	if cleanFile != "" {
		dir = path.Dir(cleanFile)
		if dir == "." {
			dir = ""
		}
		base = path.Base(cleanFile)
		ext = path.Ext(cleanFile)
	}

	return PathInput{
		File:         cleanFile,
		Dir:          dir,
		Base:         base,
		Ext:          ext,
		IsGenerated:  isGeneratedPath(cleanFile),
		IsTest:       isTestPath(cleanFile),
		InSourceRoot: inSourceRoot(cleanFile, sourceRoots),
	}
}

func diagnosticInput(diagnostic *diagnostics.Diagnostic) DiagnosticInput {
	if diagnostic == nil {
		return DiagnosticInput{}
	}

	return DiagnosticInput{
		Tool:     diagnostic.Tool,
		Code:     diagnostic.Code,
		Message:  diagnostic.Message,
		File:     cleanInputFile(diagnostic.File),
		Line:     int64(diagnostic.Line),
		Column:   int64(diagnostic.Column),
		Severity: diagnostic.Severity,
		PolicyID: diagnostic.PolicyID,
	}
}

func findingInput(finding *FindingActivation) FindingInput {
	if finding == nil {
		return FindingInput{PrincipleIDs: []string{}}
	}

	return FindingInput{
		Tool:         finding.Tool,
		Code:         finding.Code,
		Message:      finding.Message,
		File:         cleanInputFile(finding.File),
		Line:         int64(finding.Line),
		Severity:     finding.Severity,
		PolicyID:     finding.PolicyID,
		SkillID:      finding.SkillID,
		PrincipleIDs: append([]string(nil), finding.PrincipleIDs...),
	}
}

func cleanInputFile(file string) string {
	cleaned := strings.TrimPrefix(path.Clean(strings.TrimSpace(file)), "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}

	return cleaned
}

func cleanSourceRoots(sourceRoots []string) []string {
	return cleanStringSlice(sourceRoots)
}

func cleanStringSlice(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanInputFile(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}

	return cleaned
}

func presentRepoConfigs(files []string, candidates []string) []string {
	present := []string{}
	for _, candidate := range candidates {
		if listContainsCleanPath(files, candidate) {
			present = append(present, candidate)
		}
	}

	return present
}

func listContainsCleanPath(files []string, candidate string) bool {
	cleanCandidate := cleanInputFile(candidate)
	for _, file := range files {
		if cleanInputFile(file) == cleanCandidate {
			return true
		}
	}

	return false
}

func protectedPathFiles(files []string, protectedPaths []string) []string {
	matched := []string{}
	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if isProtectedPath(cleanFile, protectedPaths) {
			matched = append(matched, cleanFile)
		}
	}

	return matched
}

func isGeneratedPath(file string) bool {
	return strings.HasPrefix(file, "generated/") ||
		strings.Contains(file, "/generated/") ||
		strings.HasPrefix(file, ".generated/") ||
		strings.Contains(file, "/.generated/") ||
		strings.Contains(path.Base(file), ".generated.")
}

func isTestPath(file string) bool {
	base := path.Base(file)

	return strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.Contains(file, "/tests/") ||
		strings.HasPrefix(file, "tests/")
}

func inSourceRoot(file string, sourceRoots []string) bool {
	if file == "" {
		return false
	}

	for _, sourceRoot := range sourceRoots {
		if file == sourceRoot || strings.HasPrefix(file, sourceRoot+"/") {
			return true
		}
	}

	return len(sourceRoots) == 0
}

func isProtectedPath(file string, protectedPaths []string) bool {
	cleanFile := cleanInputFile(file)
	for _, protectedPath := range protectedPaths {
		cleanProtectedPath := cleanInputFile(protectedPath)
		if cleanProtectedPath == "" {
			continue
		}
		if cleanFile == cleanProtectedPath ||
			strings.HasPrefix(cleanFile, cleanProtectedPath+"/") ||
			strings.Contains(cleanFile, "/"+cleanProtectedPath) {
			return true
		}
	}

	return false
}

func isProtectedBranch(branch string, protectedBranches []string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}
	for _, protectedBranch := range protectedBranches {
		if branch == strings.TrimSpace(protectedBranch) {
			return true
		}
	}

	return false
}

func commandHasInlineEnv(command string, name string) bool {
	fields := strings.Fields(command)
	for _, field := range fields {
		if !strings.Contains(field, "=") {
			return false
		}
		if name == "" {
			return true
		}
		if strings.HasPrefix(field, name+"=") {
			return true
		}
	}

	return false
}
