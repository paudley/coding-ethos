// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	hookOutputFormatAuto  = "auto"
	hookOutputFormatHuman = "human"
	hookOutputFormatJSON  = "json"
	hookOutputFormatTOON  = "toon"
	hookOutputFormatEnv   = "CODE_ETHOS_HOOK_OUTPUT_FORMAT"
)

type hookSettings struct {
	OutputFormat       string   `mapstructure:"output_format"`
	AgentEnvMarkers    []string `mapstructure:"agent_env_markers"`
	ToolTimeoutSeconds int      `mapstructure:"tool_timeout_seconds"`
	EnabledGroups      []string `mapstructure:"enabled_groups"`
	FailSeverityLevels []string `mapstructure:"fail_severity_levels"`
	WarnSeverityLevels []string `mapstructure:"warn_severity_levels"`
}

type hookFinding struct {
	Tool     string `json:"tool"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Severity string `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
}

type hookReport struct {
	Format   string        `json:"format"`
	Tool     string        `json:"tool"`
	Title    string        `json:"title"`
	Status   string        `json:"status"`
	Summary  string        `json:"summary,omitempty"`
	Guidance []string      `json:"guidance,omitempty"`
	Findings []hookFinding `json:"findings"`
}

func formatHookReport(report hookReport, format string) string {
	report.Format = format
	if report.Status == "" {
		report.Status = statusFail
	}
	switch format {
	case hookOutputFormatJSON:
		return formatHookReportJSON(report)
	case hookOutputFormatTOON:
		return formatHookReportTOON(report)
	default:
		return formatHookReportHuman(report)
	}
}

func loadHookSettings() hookSettings {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return defaultHookSettings()
	}
	settings := defaultHookSettings()
	if err := decodeConfigSection(rootConfig, "hooks", &settings); err != nil {
		return defaultHookSettings()
	}
	if settings.OutputFormat == "" {
		settings.OutputFormat = hookOutputFormatAuto
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
		ToolTimeoutSeconds: 300,
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
			"ai",
			"commit-msg",
		},
		FailSeverityLevels: []string{"error", "fatal", "critical"},
		WarnSeverityLevels: []string{"warning", "warn"},
	}
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
	lines := []string{
		fmt.Sprintf("format: %s", report.Format),
		fmt.Sprintf("tool: %s", toonCell(report.Tool)),
		fmt.Sprintf("status: %s", toonCell(report.Status)),
		fmt.Sprintf("title: %s", toonCell(report.Title)),
	}
	if report.Summary != "" {
		lines = append(lines, "summary: "+toonCell(report.Summary))
	}
	lines = append(
		lines,
		fmt.Sprintf(
			"findings[%d]{tool,file,line,column,severity,code,message,detail}:",
			len(report.Findings),
		),
	)
	for _, finding := range report.Findings {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%d,%d,%s,%s,%s,%s",
				toonCell(firstNonEmpty(finding.Tool, report.Tool)),
				toonCell(finding.File),
				finding.Line,
				finding.Column,
				toonCell(finding.Severity),
				toonCell(finding.Code),
				toonCell(finding.Message),
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

	return fmt.Sprintf("%s: %s%s%s", prefix, code, finding.Message, detail)
}
