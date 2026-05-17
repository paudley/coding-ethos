// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const gitStatusFieldCount = 2

func fileChangeInputs(
	cwd string,
	files []string,
	protectedPaths []string,
) []FileChangeInput {
	statuses := gitFileStatuses(cwd)

	inputs := make([]FileChangeInput, 0, len(files))
	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if cleanFile == "" {
			continue
		}

		inputs = append(
			inputs,
			fileChangeInput(cwd, cleanFile, statuses[cleanFile], protectedPaths),
		)
	}

	return inputs
}

func fileChangeInput(
	cwd string,
	file string,
	status gitFileStatus,
	protectedPaths []string,
) FileChangeInput {
	statusCode := strings.TrimSpace(status.Code)
	sizeBytes, lineCount, binary := fileSizeAndLines(cwd, file)
	nonBlankLineCount := currentNonBlankLineCount(cwd, file, binary)
	originalNonBlankLineCount := originalNonBlankLineCount(cwd, file)

	return FileChangeInput{
		Base:                      path.Base(file),
		Dir:                       path.Dir(file),
		Ext:                       strings.ToLower(path.Ext(file)),
		File:                      file,
		OldFile:                   status.OldFile,
		Status:                    statusCode,
		IsAdded:                   strings.Contains(statusCode, "A"),
		IsBinary:                  binary,
		IsDeleted:                 strings.Contains(statusCode, "D"),
		IsGenerated:               isGeneratedPath(file),
		IsModified:                strings.Contains(statusCode, "M"),
		IsProtected:               isProtectedPath(file, protectedPaths),
		IsRenamed:                 strings.Contains(statusCode, "R"),
		IsTest:                    isTestPath(file),
		LineCount:                 int64(lineCount),
		OriginalLineCount:         int64(originalLineCount(cwd, file)),
		NonBlankLineCount:         int64(nonBlankLineCount),
		OriginalNonBlankLineCount: int64(originalNonBlankLineCount),
		NonBlankLineDelta:         int64(nonBlankLineCount - originalNonBlankLineCount),
		SizeBytes:                 sizeBytes,
		NonBlankLineCountGrows: originalNonBlankLineCount >= 0 &&
			nonBlankLineCount > originalNonBlankLineCount,
		NonBlankLineCountShrinks: originalNonBlankLineCount >= 0 &&
			nonBlankLineCount < originalNonBlankLineCount,
	}
}

type gitFileStatus struct {
	Code    string
	OldFile string
}

func gitFileStatuses(cwd string) map[string]gitFileStatus {
	output, err := gitOutput(cwd, "diff", "--cached", "--name-status", "-M")
	if err != nil {
		return nil
	}

	statuses := map[string]gitFileStatus{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < gitStatusFieldCount {
			continue
		}

		status := gitFileStatus{Code: fields[0]}

		file := fields[1]
		if strings.HasPrefix(status.Code, "R") && len(fields) >= 3 {
			status.OldFile = fields[1]
			file = fields[2]
		}

		statuses[cleanInputFile(file)] = status
	}

	return statuses
}

func fileSizeAndLines(cwd, file string) (int64, int, bool) {
	path := resolveFilePath(cwd, file)

	content, err := os.ReadFile(path)
	if err != nil {
		return 0, -1, false
	}

	if bytes.Contains(content, []byte{0}) {
		return int64(len(content)), -1, true
	}

	return int64(len(content)), countLines(string(content)), false
}

func originalLineCount(cwd, file string) int {
	output, err := gitOutput(cwd, "show", "HEAD:"+file)
	if err != nil {
		return -1
	}

	return countLines(output)
}

func currentNonBlankLineCount(cwd, file string, binary bool) int {
	if binary {
		return -1
	}

	path := resolveFilePath(cwd, file)

	content, err := os.ReadFile(path)
	if err != nil || bytes.Contains(content, []byte{0}) {
		return -1
	}

	return countNonBlankLines(string(content))
}

func originalNonBlankLineCount(cwd, file string) int {
	output, err := gitOutput(cwd, "show", "HEAD:"+file)
	if err != nil {
		return -1
	}

	return countNonBlankLines(output)
}

func countLines(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}

	return strings.Count(trimmed, "\n") + 1
}

func countNonBlankLines(text string) int {
	count := 0

	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if !isBlankLine(line) {
			count++
		}
	}

	return count
}

func isBlankLine(text string) bool {
	return strings.TrimSpace(text) == ""
}

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := realgit.Command(context.Background(), false, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = realgit.CleanGitLocalEnv(os.Environ())
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list git diff files: %w", err)
	}

	return string(output), nil
}

func resolveFilePath(cwd, file string) string {
	if filepath.IsAbs(file) {
		return file
	}

	return filepath.Join(cwd, filepath.FromSlash(file))
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

	return slices.Contains(inputs, candidate)
}

func findingInputs(
	findings []FindingActivation,
	primary *FindingActivation,
) []FindingInput {
	inputs := make([]FindingInput, 0, len(findings)+1)
	for _, finding := range findings {
		inputs = append(inputs, findingInput(&finding))
	}

	if primary != nil {
		inputs = append(inputs, findingInput(primary))
	}

	return inputs
}

func requiredIgnoreInputs(cwd string, paths []string) []IgnoreInput {
	inputs := make([]IgnoreInput, 0, len(paths))
	for _, requiredPath := range cleanStringValues(paths) {
		ignored, err := gitCheckIgnore(cwd, requiredPath)

		item := IgnoreInput{
			Path:    requiredPath,
			Ignored: ignored,
		}
		if err != nil {
			item.CheckFailed = true
			item.Error = err.Error()
		}

		inputs = append(inputs, item)
	}

	return inputs
}

func gitCheckIgnore(cwd, path string) (bool, error) {
	if cwd == "" || path == "" {
		return false, nil
	}

	cmd := realgit.Command(
		context.Background(),
		false,
		"check-ignore",
		"--quiet",
		"--no-index",
		path,
	)
	cmd.Dir = cwd

	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	exitError := &exec.ExitError{}

	ok := errors.As(err, &exitError)
	if ok && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("check git repository state: %w", err)
}

func pathInputs(cwd string, files, sourceRoots []string) []PathInput {
	paths := make([]PathInput, 0, len(files))
	for _, file := range files {
		pathInput := newPathInput(cwd, file, sourceRoots)
		if pathInput.File != "" {
			paths = append(paths, pathInput)
		}
	}

	return paths
}

func newPathInput(cwd, file string, sourceRoots []string) PathInput {
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

	symlinkTarget, isSymlink := symlinkTargetInput(cwd, cleanFile)

	return PathInput{
		File:          cleanFile,
		Dir:           dir,
		Base:          base,
		Ext:           ext,
		SymlinkTarget: symlinkTarget,
		IsSymlink:     isSymlink,
		IsGenerated:   isGeneratedPath(cleanFile),
		IsTest:        isTestPath(cleanFile),
		InSourceRoot:  inSourceRoot(cleanFile, sourceRoots),
	}
}

func symlinkTargetInput(cwd, cleanFile string) (string, bool) {
	if cwd == "" || cleanFile == "" {
		return "", false
	}

	resolved := filepath.FromSlash(cleanFile)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}

	resolved = filepath.Clean(resolved)

	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}

	target, err := os.Readlink(resolved)
	if err != nil {
		return "", true
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(resolved), target)
	}

	target = filepath.Clean(target)

	relative, err := filepath.Rel(cwd, target)
	if err == nil && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		relative != ".." {
		return filepath.ToSlash(relative), true
	}

	return filepath.ToSlash(target), true
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

func coverageInputs(
	diagnosticList []diagnostics.Diagnostic,
	primary *diagnostics.Diagnostic,
) []CoverageInput {
	diagnostics := append([]diagnostics.Diagnostic(nil), diagnosticList...)
	if primary != nil {
		diagnostics = append(diagnostics, *primary)
	}

	inputs := make([]CoverageInput, 0, len(diagnostics))
	seen := map[string]bool{}

	for _, diagnostic := range diagnostics {
		input, ok := coverageInput(diagnostic)
		if !ok {
			continue
		}

		key := input.Tool + "\x00" + input.File + "\x00" + input.Code
		if seen[key] {
			continue
		}

		seen[key] = true

		inputs = append(inputs, input)
	}

	return inputs
}

func coverageInput(diagnostic diagnostics.Diagnostic) (CoverageInput, bool) {
	value, found := diagnostic.Metadata["coverage_percent"]
	if !found {
		return CoverageInput{}, false
	}

	percent, parsed := coveragePercent(value)
	if !parsed {
		return CoverageInput{}, false
	}

	return CoverageInput{
		Tool:    diagnostic.Tool,
		File:    cleanInputFile(diagnostic.File),
		Package: coveragePackage(diagnostic),
		Code:    diagnostic.Code,
		Percent: percent,
		Total:   diagnostic.Code == "coverage-total",
	}, true
}

func coveragePackage(diagnostic diagnostics.Diagnostic) string {
	value, ok := diagnostic.Metadata["package"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func coveragePercent(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()

		return parsed, err == nil
	default:
		return 0, false
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

func cleanStringValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}

	return cleaned
}

func presentRepoConfigs(files, candidates []string) []string {
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

func protectedPathFiles(files, protectedPaths []string) []string {
	matched := []string{}

	for _, file := range files {
		cleanFile := cleanInputFile(file)
		if isProtectedPath(cleanFile, protectedPaths) {
			matched = append(matched, cleanFile)
		}
	}

	return matched
}

func referencedFileInputs(
	cwd string,
	files []string,
	argv []string,
) []ReferencedFileInput {
	referenced := append([]string{}, files...)

	for _, arg := range argv {
		if arg == "" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}

		referenced = append(referenced, arg)
	}

	result := []ReferencedFileInput{}
	seen := map[string]bool{}

	for _, file := range referenced {
		cleanFile := cleanInputFile(file)
		if cleanFile == "" || seen[cleanFile] {
			continue
		}

		seen[cleanFile] = true

		result = append(result, referencedFileInput(cwd, file))
	}

	return result
}

func referencedFileInput(cwd, file string) ReferencedFileInput {
	cleanFile := cleanInputFile(file)

	resolved := file
	if !filepath.IsAbs(resolved) && cwd != "" {
		resolved = filepath.Join(cwd, resolved)
	}

	resolved = filepath.Clean(resolved)

	input := ReferencedFileInput{
		Base:             path.Base(cleanFile),
		Dir:              path.Dir(cleanFile),
		File:             cleanFile,
		InAgentWorkspace: inAgentWorkspacePath(cleanFile),
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return input
	}

	input.Exists = true
	input.IsRegular = info.Mode().IsRegular()

	input.SizeBytes = info.Size()
	if !input.IsRegular || info.Size() > maxReferencedFileFactBytes {
		return input
	}

	content, err := os.ReadFile(resolved)
	if err != nil || bytes.ContainsRune(content, 0) {
		return input
	}

	input.Lower = strings.ToLower(string(content))

	return input
}

const maxReferencedFileFactBytes = 1 << 20

func inAgentWorkspacePath(file string) bool {
	return strings.Contains(file, "/.claude/") ||
		strings.HasPrefix(file, ".claude/") ||
		strings.Contains(file, "/.codex/") ||
		strings.HasPrefix(file, ".codex/") ||
		strings.Contains(file, "/.gemini/") ||
		strings.HasPrefix(file, ".gemini/")
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

func commandHasInlineEnv(command, name string) bool {
	fields := strings.FieldsSeq(command)
	for field := range fields {
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
