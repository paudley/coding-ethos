// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	hookOutputFormatAuto  = "auto"
	hookOutputFormatHuman = "human"
	hookOutputFormatJSON  = "json"
	hookOutputFormatTOON  = "toon"
	hookOutputFormatEnv   = "CODE_ETHOS_HOOK_OUTPUT_FORMAT"
	hookSuccessOutputEnv  = "CODE_ETHOS_HOOK_SUCCESS_OUTPUT"
	hookSuccessSilent     = "silent"
	hookSuccessMinimal    = "minimal"
	hookSuccessVerbose    = "verbose"
)

type hookSettings struct {
	OutputFormat       string   `mapstructure:"output_format"`
	SuccessOutput      string   `mapstructure:"success_output"`
	AgentEnvMarkers    []string `mapstructure:"agent_env_markers"`
	EnabledGroups      []string `mapstructure:"enabled_groups"`
	FailSeverityLevels []string `mapstructure:"fail_severity_levels"`
	WarnSeverityLevels []string `mapstructure:"warn_severity_levels"`
	ToolTimeoutSeconds int      `mapstructure:"tool_timeout_seconds"`
	ParallelGroups     bool     `mapstructure:"parallel_groups"`
}

type hookFinding struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	Advice       string         `json:"advice,omitempty"`
	Confidence   string         `json:"confidence,omitempty"`
	Tool         string         `json:"tool"`
	File         string         `json:"file,omitempty"`
	Severity     string         `json:"severity,omitempty"`
	Code         string         `json:"code,omitempty"`
	PolicyID     string         `json:"policy_id,omitempty"`
	SkillID      string         `json:"skill_id,omitempty"`
	Message      string         `json:"message"`
	Meaning      string         `json:"meaning,omitempty"`
	Detail       string         `json:"detail,omitempty"`
	AdviceSteps  []string       `json:"advice_steps,omitempty"`
	PrincipleIDs []string       `json:"principle_ids,omitempty"`
	Rerun        []string       `json:"rerun,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Line         int            `json:"line,omitempty"`
	Column       int            `json:"column,omitempty"`
}

type hookReport struct {
	Format    string        `json:"format"`
	Tool      string        `json:"tool"`
	Title     string        `json:"title"`
	Status    string        `json:"status"`
	Summary   string        `json:"summary,omitempty"`
	RawOutput []string      `json:"raw_output,omitempty"`
	Guidance  []string      `json:"guidance,omitempty"`
	Findings  []hookFinding `json:"findings"`
}

func formatHookReport(report hookReport, format string) string {
	report.Format = format
	if report.Status == "" {
		report.Status = statusFail
	}

	report = normalizeHookReportPaths(report)

	switch format {
	case hookOutputFormatJSON:
		return formatHookReportJSON(report)
	case hookOutputFormatTOON:
		return formatHookReportTOON(report)
	default:
		return formatHookReportHuman(report)
	}
}

func normalizeHookReportPaths(report hookReport) hookReport {
	root := repoRoot()
	for i := range report.Findings {
		report.Findings[i].File = normalizeHookFindingPath(root, report.Findings[i].File)
	}

	return report
}

func normalizeHookFindingPath(root string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path))
	}

	rel, err := filepath.Rel(root, path)
	if err != nil ||
		rel == "." ||
		rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Clean(path))
	}

	return filepath.ToSlash(rel)
}

func loadHookSettings() hookSettings {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return defaultHookSettings()
	}

	settings := defaultHookSettings()

	err = decodeConfigSection(rootConfig, "hooks", &settings)
	if err != nil {
		return defaultHookSettings()
	}

	if settings.OutputFormat == "" {
		settings.OutputFormat = hookOutputFormatAuto
	}

	if settings.SuccessOutput == "" {
		settings.SuccessOutput = hookSuccessSilent
	}

	if len(settings.AgentEnvMarkers) == 0 {
		settings.AgentEnvMarkers = defaultHookSettings().AgentEnvMarkers
	}

	if settings.ToolTimeoutSeconds <= 0 {
		settings.ToolTimeoutSeconds = defaultHookSettings().ToolTimeoutSeconds
	}

	if len(settings.FailSeverityLevels) == 0 {
		settings.FailSeverityLevels = defaultHookSettings().FailSeverityLevels
	}

	if len(settings.WarnSeverityLevels) == 0 {
		settings.WarnSeverityLevels = defaultHookSettings().WarnSeverityLevels
	}

	return settings
}

func defaultHookSettings() hookSettings {
	return hookSettings{
		OutputFormat:       hookOutputFormatAuto,
		SuccessOutput:      hookSuccessSilent,
		ParallelGroups:     true,
		ToolTimeoutSeconds: defaultToolTimeoutSecs,
		AgentEnvMarkers: []string{
			"CODEX_THREAD_ID",
			"CODEX_CI",
			"CODEX_MANAGED_BY_NPM",
			"CLAUDECODE",
			"CLAUDE_CODE_ENTRYPOINT",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
			"GEMINI_CLI",
			"AIDER_MODEL",
			"CURSOR_TRACE_ID",
		},
		EnabledGroups: []string{
			"format",
			"syntax",
			"python-policy",
			"python-static",
			"docs",
			"security",
			"shell",
			"docker",
			"workflow",
			"python-quality",
			"go",
			"ai",
			"commit-msg",
		},
		FailSeverityLevels: []string{"error", "fatal", "critical"},
		WarnSeverityLevels: []string{"warning", "warn"},
	}
}

func selectedHookSuccessOutput() string {
	settings := loadHookSettings()

	mode := strings.ToLower(strings.TrimSpace(os.Getenv(hookSuccessOutputEnv)))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(settings.SuccessOutput))
	}

	switch mode {
	case hookSuccessSilent, hookSuccessMinimal, hookSuccessVerbose:
		return mode
	case "0", "false", "off", "none":
		return hookSuccessSilent
	case "1", "true", "on":
		return hookSuccessVerbose
	default:
		return hookSuccessSilent
	}
}

func hookVerboseSuccessOutputEnabled() bool {
	return selectedHookSuccessOutput() == hookSuccessVerbose
}

func hookParallelGroupsEnabled() bool {
	return loadHookSettings().ParallelGroups
}

func selectedHookOutputFormat() string {
	settings := loadHookSettings()

	format := strings.ToLower(strings.TrimSpace(os.Getenv(hookOutputFormatEnv)))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(settings.OutputFormat))
	}

	switch format {
	case "", hookOutputFormatAuto:
		if isLLMCallerEnvironment(os.Getenv, settings.AgentEnvMarkers) {
			return hookOutputFormatTOON
		}

		return hookOutputFormatHuman
	case hookOutputFormatHuman, hookOutputFormatJSON, hookOutputFormatTOON:
		return format
	default:
		return hookOutputFormatHuman
	}
}

func isLLMCallerEnvironment(getenv func(string) string, markers []string) bool {
	if len(markers) == 0 {
		markers = defaultHookSettings().AgentEnvMarkers
	}

	for _, name := range markers {
		if strings.TrimSpace(getenv(name)) != "" {
			return true
		}
	}

	return false
}

func formatHookReportJSON(report hookReport) string {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return formatHookReportHuman(report)
	}

	return string(content)
}

func formatHookReportTOON(report hookReport) string {
	findingsHeader := "findings[%d]{tool,file,line,column,severity,code," +
		"policy_id,skill_id,message,advice,detail}:"

	lines := []string{
		"format: " + report.Format,
		"tool: " + toonCell(report.Tool),
		"status: " + toonCell(report.Status),
		"title: " + toonCell(report.Title),
	}
	if report.Summary != "" {
		lines = append(lines, "summary: "+toonCell(report.Summary))
	}

	lines = append(
		lines,
		fmt.Sprintf(
			findingsHeader,
			len(report.Findings),
		),
	)
	for _, finding := range report.Findings {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%d,%d,%s,%s,%s,%s,%s,%s,%s",
				toonCell(firstNonEmpty(finding.Tool, report.Tool)),
				toonCell(finding.File),
				finding.Line,
				finding.Column,
				toonCell(finding.Severity),
				toonCell(finding.Code),
				toonCell(finding.PolicyID),
				toonCell(finding.SkillID),
				toonCell(finding.Message),
				toonCell(finding.Advice),
				toonCell(finding.Detail),
			),
		)
	}

	if len(report.Guidance) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("guidance[%d]{message}:", len(report.Guidance)),
		)
		for _, item := range report.Guidance {
			lines = append(lines, "  "+toonCell(item))
		}
	}

	if len(report.RawOutput) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("raw_output[%d]{line}:", len(report.RawOutput)),
		)
		for _, item := range report.RawOutput {
			lines = append(lines, "  "+toonCell(item))
		}
	}

	return strings.Join(lines, "\n")
}

func formatHookReportHuman(report hookReport) string {
	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		report.Title,
		strings.Repeat("=", reportDividerWidth),
	}
	if report.Summary != "" {
		lines = append(lines, "", report.Summary)
	}

	if len(report.Findings) > 0 {
		lines = append(lines, "", "Violations found:")
		for _, finding := range report.Findings {
			lines = append(lines, "  "+formatHookFindingHuman(report.Tool, finding))
		}
	}

	if len(report.Guidance) > 0 {
		lines = append(lines, "", "How to fix:")
		for _, item := range report.Guidance {
			lines = append(lines, "  "+item)
		}
	}

	if len(report.RawOutput) > 0 {
		lines = append(lines, "", "Raw output:")
		for _, item := range report.RawOutput {
			lines = append(lines, "  "+item)
		}
	}

	lines = append(lines, strings.Repeat("=", reportDividerWidth))

	return strings.Join(lines, "\n")
}

func formatHookFindingHuman(tool string, finding hookFinding) string {
	location := finding.File
	if finding.Line > 0 {
		location += fmt.Sprintf(":%d", finding.Line)
		if finding.Column > 0 {
			location += fmt.Sprintf(":%d", finding.Column)
		}
	}

	prefix := strings.TrimSpace(location)
	if prefix == "" {
		prefix = firstNonEmpty(finding.Tool, tool)
	}

	code := finding.Code
	if code != "" {
		code = "[" + code + "] "
	}

	detail := ""
	if finding.Detail != "" {
		detail = " " + finding.Detail
	}

	line := fmt.Sprintf("%s: %s%s%s", prefix, code, finding.Message, detail)
	if finding.PolicyID != "" {
		line += " policy=" + finding.PolicyID
	}

	if finding.SkillID != "" {
		line += " skill=" + finding.SkillID
	}

	if finding.Advice != "" {
		line += " advice=" + finding.Advice
	}

	return line
}
