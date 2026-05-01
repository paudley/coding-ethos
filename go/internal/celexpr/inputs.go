// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

type MetadataInput struct {
	AdminApproved bool   `json:"admin_approved"`
	Tool          string `json:"tool"`
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
	Root          string   `json:"root"`
	SourceRoots   []string `json:"source_roots"`
	PythonVersion string   `json:"python_version"`
}

type ActivationInput struct {
	Argv          []string
	Command       string
	Cwd           string
	Files         []string
	Scope         string
	Tool          string
	AdminApproved bool
	SourceRoots   []string
	PythonVersion string
}

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
		"cwd: string",
		"files: list(string)",
		"scope: string",
		"metadata: {admin_approved, tool}",
		"path: {file, dir, base, ext, is_test, is_generated, in_source_root}",
		"diagnostic: {tool, code, message, file, line, column, severity, policy_id}",
		"finding: {tool, code, message, file, line, severity, policy_id, skill_id, principle_ids}",
		"repo: {root, source_roots, python_version}",
	}
}

func HelperSchema() []string {
	return []string{
		"has_prefix(value, prefix)",
		"has_suffix(value, suffix)",
		"glob_match(pattern, value)",
		"is_test_path(path)",
		"is_generated_path(path)",
		"in_source_root(path, source_roots)",
		"list_contains(values, value)",
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
			reflect.TypeOf(PathInput{}),
			reflect.TypeOf(DiagnosticInput{}),
			reflect.TypeOf(FindingInput{}),
			reflect.TypeOf(RepoInput{}),
			ext.ParseStructTag("json"),
		),
		cel.Variable("argv", cel.ListType(cel.StringType)),
		cel.Variable("command", cel.StringType),
		cel.Variable("cwd", cel.StringType),
		cel.Variable("files", cel.ListType(cel.StringType)),
		cel.Variable("scope", cel.StringType),
		cel.Variable("metadata", cel.ObjectType("celexpr.MetadataInput")),
		cel.Variable("path", cel.ObjectType("celexpr.PathInput")),
		cel.Variable("diagnostic", cel.ObjectType("celexpr.DiagnosticInput")),
		cel.Variable("finding", cel.ObjectType("celexpr.FindingInput")),
		cel.Variable("repo", cel.ObjectType("celexpr.RepoInput")),
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
	primaryPath := newPathInput("", sourceRoots)
	if len(input.Files) > 0 {
		primaryPath = newPathInput(input.Files[0], sourceRoots)
	}

	return map[string]any{
		"argv":    append([]string(nil), input.Argv...),
		"command": input.Command,
		"cwd":     input.Cwd,
		"files":   append([]string(nil), input.Files...),
		"metadata": MetadataInput{
			AdminApproved: input.AdminApproved,
			Tool:          input.Tool,
		},
		"scope":      input.Scope,
		"path":       primaryPath,
		"diagnostic": diagnosticInput(input, primaryPath),
		"finding":    findingInput(input, primaryPath),
		"repo": RepoInput{
			Root:          input.Cwd,
			SourceRoots:   sourceRoots,
			PythonVersion: input.PythonVersion,
		},
	}
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

func diagnosticInput(
	input ActivationInput,
	primaryPath PathInput,
) DiagnosticInput {
	return DiagnosticInput{
		Tool: input.Tool,
		File: primaryPath.File,
	}
}

func findingInput(
	input ActivationInput,
	primaryPath PathInput,
) FindingInput {
	return FindingInput{
		Tool:         input.Tool,
		File:         primaryPath.File,
		PrincipleIDs: []string{},
	}
}

func cleanSourceRoots(sourceRoots []string) []string {
	cleaned := make([]string, 0, len(sourceRoots))
	for _, sourceRoot := range sourceRoots {
		sourceRoot = strings.TrimPrefix(path.Clean(strings.TrimSpace(sourceRoot)), "./")
		if sourceRoot != "" && sourceRoot != "." && sourceRoot != "/" {
			cleaned = append(cleaned, sourceRoot)
		}
	}

	return cleaned
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
