// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

const (
	hookOutputFormatAuto  = "auto"
	hookOutputFormatHuman = "human"
	hookOutputFormatJSON  = "json"
	hookOutputFormatSARIF = "sarif"
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
	ConfigError        string   `mapstructure:"-"`
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
	Format          string        `json:"format"`
	Tool            string        `json:"tool"`
	Title           string        `json:"title"`
	Status          string        `json:"status"`
	TraceID         string        `json:"trace_id,omitempty"`
	Summary         string        `json:"summary,omitempty"`
	RawOutput       []string      `json:"raw_output,omitempty"`
	Guidance        []string      `json:"guidance,omitempty"`
	Findings        []hookFinding `json:"findings"`
	DisplayFindings []hookFinding `json:"-"`
}

type hookDiagnosticProducer interface {
	HookReport() hookReport
}

func (report hookReport) HookReport() hookReport {
	return report
}

func formatHookReport(report hookReport, format string) string {
	report = prepareHookReport(report, format)

	switch format {
	case hookOutputFormatSARIF:
		return formatHookReportSARIF(report)
	case hookOutputFormatJSON:
		return formatHookReportJSON(report)
	case hookOutputFormatTOON:
		return formatHookReportTOON(report)
	default:
		return formatHookReportHuman(report)
	}
}

func emitHookReport(writer io.Writer, producer hookDiagnosticProducer, format string) {
	report := prepareHookReport(producer.HookReport(), format)
	result := hookReportLintResult(report)

	tracePath, err := lint.LogResult(repoRoot(), result)
	if err != nil {
		writeText(os.Stderr, "warning: hook report trace not written: "+err.Error())
	} else {
		err = writeHookReportSARIFSidecar(tracePath, result)
		if err != nil {
			writeText(
				os.Stderr,
				"warning: hook report SARIF sidecar not written: "+err.Error(),
			)
		}

		err = outputsurface.AutoPruneSurface(
			context.Background(),
			repoRoot(),
			"lint_traces",
			false,
		)
		if err != nil {
			writeText(os.Stderr, "warning: hook report trace auto-prune failed: "+err.Error())
		}
	}

	err = feedback.WriteRendered(writer, formatHookReport(report, format), format)
	if err != nil {
		writeText(os.Stderr, "warning: hook report not rendered: "+err.Error())
	}
}

func writeHookReportSARIFSidecar(tracePath string, result lint.Result) error {
	err := hookoutput.WriteLintSARIFSidecar(tracePath, result)
	if err != nil {
		return fmt.Errorf("write hook report SARIF sidecar: %w", err)
	}

	return nil
}

func prepareHookReport(report hookReport, format string) hookReport {
	report.Format = format
	if report.Status == "" {
		report.Status = statusFail
	}

	report = ensureHookReportTraceID(report)
	report = normalizeHookReportPaths(report)
	report = applyHookReportDiagnosticDefaults(report)

	return report
}

func applyHookReportDiagnosticDefaults(report hookReport) hookReport {
	defaults := hookReportDiagnosticDefaults(report.Tool)
	for index := range report.Findings {
		report.Findings[index].Severity = hookFindingSeverity(report.Findings[index])
		report.Findings[index].Code = firstNonEmpty(
			report.Findings[index].Code,
			defaults.Code,
		)
		report.Findings[index].PolicyID = firstNonEmpty(
			report.Findings[index].PolicyID,
			defaults.PolicyID,
		)

		report.Findings[index].SkillID = firstNonEmpty(
			report.Findings[index].SkillID,
			defaults.SkillID,
		)
		if len(report.Findings[index].PrincipleIDs) == 0 {
			report.Findings[index].PrincipleIDs = append(
				[]string(nil),
				defaults.PrincipleIDs...,
			)
		}
	}

	return report
}

func ensureHookReportTraceID(report hookReport) hookReport {
	if strings.TrimSpace(report.TraceID) != "" {
		return report
	}

	result := hookReportLintResult(report)
	lint.EnsureTraceID(&result)
	report.TraceID = result.TraceID

	return report
}

func normalizeHookReportPaths(report hookReport) hookReport {
	root := repoRoot()
	for i := range report.Findings {
		report.Findings[i].File = normalizeHookFindingPath(
			root,
			report.Findings[i].File,
		)
	}

	return report
}

func normalizeHookFindingPath(root, path string) string {
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
	if rootConfigSectionHasNormalizedKey(rootConfig, "hooks", "enabled_groups") {
		settings.ConfigError = "hooks.enabled_groups has been removed; " +
			"hook groups are policy-owned"

		return settings
	}

	err = decodeConfigSection(rootConfig, "hooks", &settings)
	if err != nil {
		settings.ConfigError = fmt.Sprintf("parse hooks config: %v", err)

		return settings
	}

	if settings.OutputFormat == "" {
		settings.OutputFormat = hookOutputFormatAuto
	}

	if settings.SuccessOutput == "" {
		settings.SuccessOutput = hookSuccessSilent
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

func rejectInvalidHookSettings() int {
	settings := loadHookSettings()
	if strings.TrimSpace(settings.ConfigError) == "" {
		return 0
	}

	writef(os.Stderr, "FATAL: %s\n", settings.ConfigError)

	return 1
}

func defaultHookSettings() hookSettings {
	return hookSettings{
		OutputFormat:       hookOutputFormatAuto,
		SuccessOutput:      hookSuccessSilent,
		ParallelGroups:     true,
		ToolTimeoutSeconds: defaultToolTimeoutSecs,
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

func selectedHookOutputFormat() string {
	settings := loadHookSettings()

	format := strings.ToLower(strings.TrimSpace(os.Getenv(hookOutputFormatEnv)))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(settings.OutputFormat))
	}

	switch format {
	case "", hookOutputFormatAuto:
		return hookOutputFormatTOON
	case hookOutputFormatHuman,
		hookOutputFormatJSON,
		hookOutputFormatSARIF,
		hookOutputFormatTOON:
		return format
	default:
		return hookOutputFormatTOON
	}
}

func formatHookReportJSON(report hookReport) string {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return formatHookReportHuman(report)
	}

	return string(content)
}

func formatHookReportSARIF(report hookReport) string {
	output, err := hookoutput.FormatLintResult(
		hookReportLintResult(report),
		hookoutput.FormatSARIF,
	)
	if err != nil {
		return formatHookReportJSON(report)
	}

	return output
}

func hookReportLintResult(report hookReport) lint.Result {
	report = normalizeHookReportPaths(report)

	return lint.Result{
		TraceID:     report.TraceID,
		Scope:       "tool:" + report.Tool,
		Status:      hookReportLintStatus(report),
		Diagnostics: hookReportDiagnostics(report),
		Findings:    hookReportFindings(report),
	}
}

func hookReportLintStatus(report hookReport) string {
	if strings.EqualFold(report.Status, statusPass) ||
		strings.EqualFold(report.Status, statusWarn) {
		return "resolved"
	}

	return "blocked"
}

func hookReportDiagnostics(report hookReport) []diagnostics.Diagnostic {
	items := make([]diagnostics.Diagnostic, 0, len(report.Findings))
	for _, finding := range report.Findings {
		items = append(items, hookFindingDiagnostic(report, finding))
	}

	return diagnostics.Dedupe(items)
}

func hookFindingDiagnostic(
	report hookReport,
	finding hookFinding,
) diagnostics.Diagnostic {
	defaults := hookReportDiagnosticDefaults(report.Tool)

	return diagnostics.Diagnostic{
		Metadata:     finding.Metadata,
		Advice:       finding.Advice,
		Confidence:   finding.Confidence,
		Tool:         firstNonEmpty(finding.Tool, report.Tool),
		File:         finding.File,
		Severity:     hookFindingSeverity(finding),
		Code:         firstNonEmpty(finding.Code, defaults.Code),
		PolicyID:     firstNonEmpty(finding.PolicyID, defaults.PolicyID),
		SkillID:      firstNonEmpty(finding.SkillID, defaults.SkillID),
		Message:      finding.Message,
		Meaning:      finding.Meaning,
		Detail:       finding.Detail,
		AdviceSteps:  append([]string(nil), finding.AdviceSteps...),
		PrincipleIDs: appendHookReportPrinciples(finding, defaults),
		Rerun:        append([]string(nil), finding.Rerun...),
		Tags:         append([]string(nil), finding.Tags...),
		Line:         finding.Line,
		Column:       finding.Column,
	}
}

func hookReportFindings(report hookReport) []lint.Finding {
	findings := make([]lint.Finding, 0, len(report.Findings))
	defaults := hookReportDiagnosticDefaults(report.Tool)

	for _, finding := range report.Findings {
		findings = append(findings, lint.Finding{
			RawOutcome: hookFindingRawOutcome(finding),
			Advice:     finding.Advice,
			CheckID:    firstNonEmpty(finding.PolicyID, defaults.PolicyID),
			Code:       firstNonEmpty(finding.Code, defaults.Code),
			File:       finding.File,
			Message:    finding.Message,
			PolicyID:   firstNonEmpty(finding.PolicyID, defaults.PolicyID),
			Severity:   hookFindingSeverity(finding),
			SkillID:    firstNonEmpty(finding.SkillID, defaults.SkillID),
			SourceTool: firstNonEmpty(finding.Tool, report.Tool),
			Status:     hookReportFindingStatus(finding),
			EthosIDs:   appendHookReportPrinciples(finding, defaults),
			Blocking:   hookReportFindingBlocks(finding),
			Column:     finding.Column,
			Line:       finding.Line,
		})
	}

	return findings
}

func hookFindingRawOutcome(finding hookFinding) map[string]any {
	if len(finding.Metadata) == 0 {
		return nil
	}

	raw := make(map[string]any, len(finding.Metadata))
	maps.Copy(raw, finding.Metadata)

	return raw
}

func hookReportFindingStatus(finding hookFinding) string {
	if hookReportFindingBlocks(finding) {
		return "fail"
	}

	if isHookWarningSeverity(finding.Severity) {
		return "warn"
	}

	return "pass"
}

func hookReportFindingBlocks(finding hookFinding) bool {
	severity := strings.ToLower(strings.TrimSpace(finding.Severity))

	return severity == "block" || severity == "error" || severity == "fatal"
}

func isHookWarningSeverity(severity string) bool {
	severity = strings.ToLower(strings.TrimSpace(severity))

	return severity == "warn" || severity == "warning"
}

type hookReportDiagnosticDefault struct {
	Code         string
	PolicyID     string
	SkillID      string
	PrincipleIDs []string
}

func hookReportDiagnosticDefaults(tool string) hookReportDiagnosticDefault {
	defaults, found := hookReportDefaultPolicy(tool)
	if found {
		return defaults
	}

	return hookReportPolicy("hook."+tool, "violation")
}

func hookReportDefaultPolicy(tool string) (hookReportDiagnosticDefault, bool) {
	for _, candidate := range hookReportDefaultPolicyCandidates() {
		if candidate.tool == tool {
			return hookReportPolicy(candidate.policyID, candidate.code), true
		}
	}

	return hookReportDiagnosticDefault{}, false
}

type hookReportDefaultPolicyCandidate struct {
	tool     string
	policyID string
	code     string
}

func hookReportDefaultPolicyCandidates() []hookReportDefaultPolicyCandidate {
	return []hookReportDefaultPolicyCandidate{
		{"catch_and_silence", "python.catch_and_silence", "errors"},
		{"comment_suppressions", "python.comment_suppressions", "linting"},
		{"conditional_imports", "python.conditional_imports", "dependency"},
		{"docstring_coverage", "python.docstring_coverage", "documentation"},
		{"manifest_validation", "manifest.validation", "configuration"},
		{"module_docs", "python.module_docs", "documentation"},
		{"module_docstrings", "python.module_docs", "documentation"},
		{"optional_returns", "python.optional_returns", "typing"},
		{"plan_completion", "agent.plan_completion", "workflow"},
		{"pytest_gate", "testing.pytest_gate", "testing"},
		{"python_version_consistency", "python.version_consistency", "configuration"},
		{"security_patterns", "python.security_patterns", "security"},
		{"sql_centralization", "python.sql_centralization", "architecture"},
		{"structured_logging", "python.structured_logging", "observability"},
		{"type_checking_imports", "python.type_checking_imports", "typing"},
		{"util_centralization", "python.util_centralization", "architecture"},
	}
}

func hookReportPolicy(policyID, code string) hookReportDiagnosticDefault {
	return hookReportDiagnosticDefault{
		Code:     code,
		PolicyID: policyID,
		SkillID:  "agent-operating-discipline",
	}
}

func appendHookReportPrinciples(
	finding hookFinding,
	defaults hookReportDiagnosticDefault,
) []string {
	if len(finding.PrincipleIDs) > 0 {
		return append([]string(nil), finding.PrincipleIDs...)
	}

	return append([]string(nil), defaults.PrincipleIDs...)
}

func hookFindingSeverity(finding hookFinding) string {
	if strings.TrimSpace(finding.Severity) != "" {
		return finding.Severity
	}

	return "error"
}

func formatHookReportTOON(report hookReport) string {
	findingsHeader := "findings[%d]{tool,file,line,column,severity,code," +
		"policy_id,skill_id,message,advice,detail}:"
	findings := hookReportDisplayFindings(report)

	lines := []string{
		"tool: " + toonCell(report.Tool),
		"status: " + toonCell(report.Status),
		"trace_id: " + toonCell(report.TraceID),
		"title: " + toonCell(report.Title),
	}
	if report.Summary != "" {
		lines = append(lines, "summary: "+toonCell(report.Summary))
	}

	lines = append(
		lines,
		fmt.Sprintf(
			findingsHeader,
			len(findings),
		),
	)
	for _, finding := range findings {
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
	findings := hookReportDisplayFindings(report)

	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		report.Title,
		strings.Repeat("=", reportDividerWidth),
		"trace_id: " + report.TraceID,
	}
	if report.Summary != "" {
		lines = append(lines, "", report.Summary)
	}

	if len(findings) > 0 {
		lines = append(lines, "", "Violations found:")
		for _, finding := range findings {
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

func hookReportDisplayFindings(report hookReport) []hookFinding {
	if len(report.DisplayFindings) > 0 {
		return report.DisplayFindings
	}

	return report.Findings
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
