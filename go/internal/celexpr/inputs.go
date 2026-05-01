// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"fmt"
	"path"
	"strings"

	"github.com/google/cel-go/cel"
)

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

func InputSchema() []string {
	return []string{
		"argv: list(string)",
		"command: string",
		"cwd: string",
		"files: list(string)",
		"scope: string",
		"metadata: map(string, dyn)",
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
	inputObject := cel.MapType(cel.StringType, cel.DynType)
	options := []cel.EnvOption{
		cel.Variable("argv", cel.ListType(cel.StringType)),
		cel.Variable("command", cel.StringType),
		cel.Variable("cwd", cel.StringType),
		cel.Variable("files", cel.ListType(cel.StringType)),
		cel.Variable("scope", cel.StringType),
		cel.Variable("metadata", inputObject),
		cel.Variable("path", inputObject),
		cel.Variable("diagnostic", inputObject),
		cel.Variable("finding", inputObject),
		cel.Variable("repo", inputObject),
	}
	options = append(options, helperFunctions()...)

	return cel.NewEnv(options...)
}

func Validate(policyID string, source string) error {
	env, err := Environment()
	if err != nil {
		return fmt.Errorf("prepare CEL environment for %q: %w", policyID, err)
	}

	ast, issues := env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("compile CEL policy %q: %w", policyID, issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return fmt.Errorf(
			"compile CEL policy %q: when expression must return bool, got %s",
			policyID,
			ast.OutputType(),
		)
	}

	return nil
}

func Activation(input ActivationInput) map[string]any {
	sourceRoots := cleanSourceRoots(input.SourceRoots)
	primaryPath := pathInput("", sourceRoots)
	if len(input.Files) > 0 {
		primaryPath = pathInput(input.Files[0], sourceRoots)
	}

	return map[string]any{
		"argv":    append([]string(nil), input.Argv...),
		"command": input.Command,
		"cwd":     input.Cwd,
		"files":   append([]string(nil), input.Files...),
		"metadata": map[string]any{
			"admin_approved": input.AdminApproved,
			"tool":           input.Tool,
		},
		"scope":      input.Scope,
		"path":       primaryPath,
		"diagnostic": diagnosticInput(input, primaryPath),
		"finding":    findingInput(input, primaryPath),
		"repo": map[string]any{
			"root":           input.Cwd,
			"source_roots":   sourceRoots,
			"python_version": input.PythonVersion,
		},
	}
}

func pathInput(file string, sourceRoots []string) map[string]any {
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

	return map[string]any{
		"file":           cleanFile,
		"dir":            dir,
		"base":           base,
		"ext":            ext,
		"is_generated":   isGeneratedPath(cleanFile),
		"is_test":        isTestPath(cleanFile),
		"in_source_root": inSourceRoot(cleanFile, sourceRoots),
	}
}

func diagnosticInput(
	input ActivationInput,
	primaryPath map[string]any,
) map[string]any {
	return map[string]any{
		"tool":      input.Tool,
		"code":      "",
		"message":   "",
		"file":      primaryPath["file"],
		"line":      int64(0),
		"column":    int64(0),
		"severity":  "",
		"policy_id": "",
	}
}

func findingInput(
	input ActivationInput,
	primaryPath map[string]any,
) map[string]any {
	return map[string]any{
		"tool":          input.Tool,
		"code":          "",
		"message":       "",
		"file":          primaryPath["file"],
		"line":          int64(0),
		"severity":      "",
		"policy_id":     "",
		"skill_id":      "",
		"principle_ids": []string{},
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
	return strings.Contains(file, "/generated/") ||
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
