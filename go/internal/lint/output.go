// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func OutputDiagnostics(result Result) []diagnostics.Diagnostic {
	if len(result.Diagnostics) > 0 {
		return diagnostics.Dedupe(normalizedResultDiagnostics(result))
	}

	if len(result.Findings) > 0 {
		return FindingDiagnostics(result.Findings, result.Blocked())
	}

	decisions := result.Decisions
	if result.Blocked() {
		blocking := []int{}

		for index, decision := range decisions {
			if decision.Decision == decisionBlock ||
				decision.Severity == decisionBlock {
				blocking = append(blocking, index)
			}
		}

		if len(blocking) > 0 {
			blockingDecisions := decisions[:0:0]
			for _, index := range blocking {
				blockingDecisions = append(blockingDecisions, decisions[index])
			}

			decisions = blockingDecisions
		}
	}

	findings := make([]diagnostics.Diagnostic, 0, len(decisions))
	for _, decision := range decisions {
		if len(decision.Diagnostics) > 0 {
			findings = append(findings, decision.EvidenceDiagnostics()...)

			continue
		}

		findings = append(findings, diagnosticFromDecision(decision))
	}

	return sortDiagnostics(diagnostics.Dedupe(findings))
}

func normalizedResultDiagnostics(result Result) []diagnostics.Diagnostic {
	items := make([]diagnostics.Diagnostic, 0, len(result.Diagnostics))
	decisionDiagnostics := rawDecisionDiagnostics(result.Decisions)

	for _, item := range result.Diagnostics {
		if slices.ContainsFunc(decisionDiagnostics, func(raw diagnostics.Diagnostic) bool {
			return sameDiagnostic(item, raw)
		}) {
			continue
		}

		items = append(items, item)
	}

	for _, decision := range result.Decisions {
		items = append(items, decision.EvidenceDiagnostics()...)
	}

	return items
}

func rawDecisionDiagnostics(decisions []policy.Decision) []diagnostics.Diagnostic {
	items := []diagnostics.Diagnostic{}
	for _, decision := range decisions {
		items = append(items, decision.Diagnostics...)
	}

	return items
}

func sameDiagnostic(left, right diagnostics.Diagnostic) bool {
	return left.File == right.File &&
		left.Line == right.Line &&
		left.Column == right.Column &&
		left.Tool == right.Tool &&
		left.Code == right.Code &&
		left.PolicyID == right.PolicyID &&
		left.Severity == right.Severity &&
		left.SkillID == right.SkillID &&
		left.Message == right.Message &&
		left.Advice == right.Advice &&
		left.Detail == right.Detail
}

func diagnosticFromDecision(decision policy.Decision) diagnostics.Diagnostic {
	files := filesFromDecision(decision, nil)
	file := ""
	detail := ""

	if len(files) == 1 {
		file = files[0]
	} else if len(files) > 1 {
		detail = "files=" + strings.Join(files, ",")
	}

	return diagnostics.Diagnostic{
		Tool:     "policy",
		File:     file,
		Severity: decision.Severity,
		PolicyID: decision.PolicyID,
		Message:  decision.Message,
		Advice:   decision.Suggestion,
		Detail:   detail,
	}
}

func ResultTool(result Result) string {
	if tool, ok := strings.CutPrefix(result.Scope, "tool:"); ok && tool != "" {
		return tool
	}

	return "policy-lint"
}

func ResultStatus(result Result) string {
	if result.Blocked() {
		return "FAIL"
	}

	if result.Warned() {
		return "WARN"
	}

	return "PASS"
}

func (result Result) Warned() bool {
	if strings.EqualFold(result.Status, "warn") ||
		strings.EqualFold(result.Status, "warning") {
		return true
	}

	for _, finding := range result.Findings {
		if finding.Blocking {
			continue
		}

		if strings.EqualFold(finding.Severity, "warn") ||
			strings.EqualFold(finding.Severity, "warning") ||
			strings.EqualFold(finding.Status, "warn") ||
			strings.EqualFold(finding.Status, "warning") {
			return true
		}
	}

	for _, diagnostic := range result.Diagnostics {
		if strings.EqualFold(diagnostic.Severity, "warn") ||
			strings.EqualFold(diagnostic.Severity, "warning") {
			return true
		}
	}

	return false
}

// FindingDiagnostics converts lint findings into normalized diagnostics.
func FindingDiagnostics(findings []Finding, blocked bool) []diagnostics.Diagnostic {
	selected := findings
	if blocked {
		blocking := blockingFindings(findings)
		if len(blocking) > 0 {
			selected = blocking
		}
	} else {
		selected = visibleFindings(findings)
	}

	items := make([]diagnostics.Diagnostic, 0, len(selected))
	for _, finding := range selected {
		items = append(items, diagnosticFromFinding(finding))
	}

	return sortDiagnostics(diagnostics.Dedupe(items))
}

func visibleFindings(findings []Finding) []Finding {
	visible := []Finding{}

	for _, finding := range findings {
		if actionableFinding(finding) {
			visible = append(visible, finding)
		}
	}

	return visible
}

func actionableFinding(finding Finding) bool {
	if finding.Blocking || finding.Status == statusFail ||
		finding.Severity == decisionBlock || finding.Severity == severityError {
		return true
	}

	if strings.EqualFold(finding.Severity, "warn") ||
		strings.EqualFold(finding.Severity, "warning") ||
		strings.EqualFold(finding.Status, "warn") ||
		strings.EqualFold(finding.Status, "warning") {
		return true
	}

	return !findingIsRecordOnly(finding)
}

func findingIsRecordOnly(finding Finding) bool {
	return strings.EqualFold(finding.Severity, "record") ||
		strings.EqualFold(finding.Status, "record") ||
		strings.EqualFold(finding.Status, "pass")
}

func blockingFindings(findings []Finding) []Finding {
	blocking := []Finding{}

	for _, finding := range findings {
		if finding.Blocking || finding.Status == statusFail ||
			finding.Severity == decisionBlock || finding.Severity == severityError {
			blocking = append(blocking, finding)
		}
	}

	return blocking
}

func diagnosticFromFinding(finding Finding) diagnostics.Diagnostic {
	file := finding.File
	if file == "" && len(finding.Files) == 1 {
		file = finding.Files[0]
	}

	return diagnostics.Diagnostic{
		Tool:     firstOutputNonEmpty(finding.SourceTool, "policy"),
		File:     file,
		Line:     finding.Line,
		Column:   finding.Column,
		Severity: finding.Severity,
		Code:     finding.Code,
		PolicyID: firstOutputNonEmpty(finding.PolicyID, finding.CheckID),
		SkillID:  finding.SkillID,
		Message:  finding.Message,
		Advice:   finding.Advice,
		Detail:   findingDetail(finding),
		PrincipleIDs: append(
			[]string(nil),
			finding.EthosIDs...,
		),
	}
}

func findingDetail(finding Finding) string {
	parts := []string{}
	if finding.File == "" && len(finding.Files) > 0 {
		parts = append(parts, "files="+strings.Join(finding.Files, ","))
	}

	appendRawOutcomeString := func(key, label string) {
		value, ok := finding.RawOutcome[key]
		if !ok || value == nil {
			return
		}

		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return
		}

		parts = append(parts, label+"="+text)
	}

	appendRawOutcomeString("category", "category")
	appendRawOutcomeString("exit_code", "exit_code")
	appendRawOutcomeString("output", "output")
	appendRawOutcomeString("stdout", "stdout")
	appendRawOutcomeString("stderr", "stderr")
	appendRawOutcomeString("error", "error")
	appendRawOutcomeString("runner_failure", "runner_failure")
	appendRawOutcomeString("timed_out", "timed_out")

	return strings.Join(parts, "; ")
}

func sortDiagnostics(items []diagnostics.Diagnostic) []diagnostics.Diagnostic {
	slices.SortStableFunc(items, compareDiagnostics)

	return items
}

func compareDiagnostics(left, right diagnostics.Diagnostic) int {
	leftBlock := diagnosticBlocks(left)

	rightBlock := diagnosticBlocks(right)
	if leftBlock != rightBlock {
		if leftBlock {
			return -1
		}

		return 1
	}

	return cmp.Compare(diagnosticSortKey(left), diagnosticSortKey(right))
}

func diagnosticBlocks(finding diagnostics.Diagnostic) bool {
	return finding.Severity == "block" || finding.Severity == "error"
}

func diagnosticSortKey(finding diagnostics.Diagnostic) string {
	return strings.Join([]string{
		finding.File,
		strconv.Itoa(finding.Line),
		finding.PolicyID,
		finding.Tool,
		finding.Code,
		finding.Message,
	}, "\x00")
}

func firstOutputNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
