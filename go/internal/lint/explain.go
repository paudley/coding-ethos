// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

type ExplainResult struct {
	Scope         string         `json:"scope"`
	Files         []string       `json:"files,omitempty"`
	Checks        []ExplainCheck `json:"checks"`
	Tools         []ExplainTool  `json:"tools,omitempty"`
	Selected      int            `json:"selected"`
	SelectedTools int            `json:"selected_tools"`
}

type ExplainCheck struct {
	CheckID      string   `json:"check_id"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason"`
	Severity     string   `json:"severity,omitempty"`
	Evaluators   []string `json:"evaluators,omitempty"`
	EthosIDs     []string `json:"ethos_ids,omitempty"`
	FilePatterns []string `json:"file_patterns,omitempty"`
	Languages    []string `json:"languages,omitempty"`
}

type ExplainTool struct {
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	Reason           string   `json:"reason"`
	Category         string   `json:"category"`
	Runtime          string   `json:"runtime"`
	Parser           string   `json:"parser"`
	OutputFormat     string   `json:"output_format"`
	RepoConfig       string   `json:"repo_config,omitempty"`
	Command          []string `json:"command,omitempty"`
	CaptureArgs      []string `json:"capture_args,omitempty"`
	FileExtensions   []string `json:"file_extensions,omitempty"`
	FilePrefixes     []string `json:"file_prefixes,omitempty"`
	BasenamePrefixes []string `json:"basename_prefixes,omitempty"`
	Languages        []string `json:"languages,omitempty"`
	EnabledByDefault bool     `json:"enabled_by_default"`
}

type ExplainOptions struct {
	Scope string
	Files []string
}

func Explain(bundle policy.Bundle, scope string) (ExplainResult, error) {
	return ExplainWithOptions(bundle, ExplainOptions{Scope: scope})
}

func ExplainWithOptions(bundle policy.Bundle, options ExplainOptions) (ExplainResult, error) {
	scope := options.Scope
	if scope == "" {
		scope = ScopeFiles
	}

	policyIDs, err := policyIDsForScope(bundle, scope)
	if err != nil {
		return ExplainResult{}, err
	}

	result := ExplainResult{
		Scope:    scope,
		Files:    normalizeExplainFiles(options.Files),
		Checks:   make([]ExplainCheck, 0, len(policyIDs)),
		Selected: len(policyIDs),
	}

	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return ExplainResult{}, fmt.Errorf(
				"%w: %q references %q",
				errUnknownScopePolicy,
				scope,
				policyID,
			)
		}

		result.Checks = append(result.Checks, ExplainCheck{
			CheckID:      policyID,
			Status:       "selected",
			Reason:       "selected by lint scope dispatch",
			Severity:     policyDef.DefaultSeverity,
			Evaluators:   evaluatorNames(policyDef.Evaluators),
			EthosIDs:     append([]string(nil), policyDef.PrincipleIDs...),
			FilePatterns: append([]string(nil), policyDef.AppliesTo.FilePatterns...),
			Languages:    append([]string(nil), policyDef.AppliesTo.Languages...),
		})
	}

	sort.Slice(result.Checks, func(left int, right int) bool {
		return result.Checks[left].CheckID < result.Checks[right].CheckID
	})

	result.Tools = explainTools(result.Files)
	for _, tool := range result.Tools {
		if tool.Status == "selected" {
			result.SelectedTools++
		}
	}

	return result, nil
}

func EncodeExplainResult(
	writer io.Writer,
	result ExplainResult,
	format string,
) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")

		return encoder.Encode(result)
	case "toon":
		_, err := fmt.Fprintln(writer, FormatExplainResultTOON(result))

		return err
	default:
		_, err := fmt.Fprintln(writer, FormatExplainResultHuman(result))

		return err
	}
}

func FormatExplainResultHuman(result ExplainResult) string {
	lines := []string{
		"lint scope: " + result.Scope,
		fmt.Sprintf("selected checks: %d", result.Selected),
		fmt.Sprintf("selected tools: %d", result.SelectedTools),
	}
	if len(result.Files) > 0 {
		lines = append(lines, "files: "+strings.Join(result.Files, ", "))
	}
	for _, check := range result.Checks {
		detail := []string{check.Status}
		if check.Severity != "" {
			detail = append(detail, check.Severity)
		}
		if len(check.Evaluators) > 0 {
			detail = append(detail, "evaluators="+strings.Join(check.Evaluators, "+"))
		}

		lines = append(
			lines,
			fmt.Sprintf("- %s: %s", check.CheckID, strings.Join(detail, ", ")),
			"  reason: "+check.Reason,
		)
	}
	if len(result.Tools) > 0 {
		lines = append(lines, "tools:")
	}
	for _, tool := range result.Tools {
		lines = append(
			lines,
			fmt.Sprintf(
				"- %s: %s, category=%s, parser=%s, output=%s",
				tool.Name,
				tool.Status,
				tool.Category,
				tool.Parser,
				tool.OutputFormat,
			),
			"  reason: "+tool.Reason,
		)
	}

	return strings.Join(lines, "\n")
}

func FormatExplainResultTOON(result ExplainResult) string {
	lines := []string{
		"format: toon",
		"tool: policy-lint",
		"operation: explain",
		"scope: " + toonCell(result.Scope),
		fmt.Sprintf("selected_checks: %d", result.Selected),
		fmt.Sprintf("selected_tools: %d", result.SelectedTools),
	}
	if len(result.Files) > 0 {
		lines = append(lines, fmt.Sprintf("files[%d]{path}:", len(result.Files)))
		for _, file := range result.Files {
			lines = append(lines, "  "+toonCell(file))
		}
	}
	lines = append(
		lines,
		fmt.Sprintf("checks[%d]{check_id,status,severity,evaluators,reason}:", len(result.Checks)),
	)
	for _, check := range result.Checks {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%s",
			toonCell(check.CheckID),
			toonCell(check.Status),
			toonCell(check.Severity),
			toonCell(strings.Join(check.Evaluators, "+")),
			toonCell(check.Reason),
		))
	}
	lines = append(
		lines,
		fmt.Sprintf("tools[%d]{name,status,category,parser,output_format,reason}:", len(result.Tools)),
	)
	for _, tool := range result.Tools {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%s,%s",
			toonCell(tool.Name),
			toonCell(tool.Status),
			toonCell(tool.Category),
			toonCell(tool.Parser),
			toonCell(tool.OutputFormat),
			toonCell(tool.Reason),
		))
	}

	return strings.Join(lines, "\n")
}

func evaluatorNames(evaluators []policy.Evaluator) []string {
	names := make([]string, 0, len(evaluators))
	for _, evaluator := range evaluators {
		if evaluator.Name == "" {
			continue
		}
		names = append(names, evaluator.Name)
	}

	return names
}

func explainTools(files []string) []ExplainTool {
	tools := toolcatalog.HookOwnedTools()
	result := make([]ExplainTool, 0, len(tools))
	for _, tool := range tools {
		status, reason := explainToolStatus(tool, files)
		captureArgs, _ := tool.CaptureArgs(nil)
		result = append(result, ExplainTool{
			Name:             tool.Name,
			Status:           status,
			Reason:           reason,
			Category:         tool.Category,
			Runtime:          string(tool.Runtime),
			Parser:           tool.Parser,
			OutputFormat:     tool.OutputFormat,
			RepoConfig:       tool.RepoConfig,
			Command:          append([]string(nil), tool.Command...),
			CaptureArgs:      captureArgs,
			FileExtensions:   append([]string(nil), tool.FileExtensions...),
			FilePrefixes:     append([]string(nil), tool.FilePrefixes...),
			BasenamePrefixes: append([]string(nil), tool.BaseNamePrefixes...),
			Languages:        append([]string(nil), tool.Languages...),
			EnabledByDefault: tool.EnabledByDefault,
		})
	}

	sort.Slice(result, func(left int, right int) bool {
		if result[left].Status != result[right].Status {
			return result[left].Status == "selected"
		}

		return result[left].Name < result[right].Name
	})

	return result
}

func explainToolStatus(tool toolcatalog.Tool, files []string) (string, string) {
	if !tool.EnabledByDefault {
		return "skipped", "tool is disabled by default"
	}
	if len(files) == 0 {
		return "selected", "enabled by default for this scope"
	}
	for _, file := range files {
		if toolMatchesFile(tool, file) {
			return "selected", "file selector matched " + file
		}
	}

	return "skipped", "no file selector matched"
}

func toolMatchesFile(tool toolcatalog.Tool, file string) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(file), "./")
	base := filepath.Base(normalized)
	if len(tool.FilePrefixes) > 0 {
		for _, prefix := range tool.FilePrefixes {
			if strings.HasPrefix(normalized, strings.TrimPrefix(filepath.ToSlash(prefix), "./")) {
				return true
			}
		}

		return false
	}
	for _, basePrefix := range tool.BaseNamePrefixes {
		if strings.HasPrefix(base, basePrefix) {
			return true
		}
	}
	extension := strings.ToLower(filepath.Ext(normalized))
	for _, candidate := range tool.FileExtensions {
		if extension == strings.ToLower(candidate) {
			return true
		}
	}
	language := languageForFile(normalized)
	for _, candidate := range tool.Languages {
		if language != "" && language == strings.ToLower(candidate) {
			return true
		}
	}

	return false
}

func languageForFile(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".py", ".pyi":
		return "python"
	case ".go":
		return "go"
	case ".sh", ".bash", ".zsh", ".ksh":
		return "shell"
	case ".yaml", ".yml":
		if strings.HasPrefix(file, ".github/workflows/") {
			return "github-actions"
		}

		return "yaml"
	case ".md":
		return "markdown"
	default:
		if strings.HasPrefix(filepath.Base(file), "Dockerfile") {
			return "dockerfile"
		}

		return ""
	}
}

func normalizeExplainFiles(files []string) []string {
	normalized := []string{}
	seen := map[string]bool{}
	for _, file := range files {
		cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(strings.TrimSpace(file))), "./")
		if cleaned == "" || cleaned == "." || seen[cleaned] {
			continue
		}
		normalized = append(normalized, cleaned)
		seen[cleaned] = true
	}

	return normalized
}

func toonCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}
