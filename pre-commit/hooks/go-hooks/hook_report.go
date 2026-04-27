// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
