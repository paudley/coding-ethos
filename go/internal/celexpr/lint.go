// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import "blackcat.ca/coding-ethos/go/diagnostics"

type LintFinding interface {
	FindingTool() string
	FindingCode() string
	FindingMessage() string
	FindingFile() string
	FindingSeverity() string
	FindingPolicyID() string
	FindingSkillID() string
	FindingPrincipleIDs() []string
	FindingColumn() int
	FindingLine() int
}

func ActivationForDiagnostic(
	input ActivationInput,
	diagnostic diagnostics.Diagnostic,
) map[string]any {
	input.Diagnostic = &diagnostic
	if len(input.Files) == 0 && diagnostic.File != "" {
		input.Files = []string{diagnostic.File}
	}
	if input.Tool == "" {
		input.Tool = diagnostic.Tool
	}

	return Activation(input)
}

func ActivationForFinding[T LintFinding](
	input ActivationInput,
	finding T,
) map[string]any {
	input.Finding = &FindingActivation{
		Tool:         finding.FindingTool(),
		Code:         finding.FindingCode(),
		Message:      finding.FindingMessage(),
		File:         finding.FindingFile(),
		Severity:     finding.FindingSeverity(),
		PolicyID:     finding.FindingPolicyID(),
		SkillID:      finding.FindingSkillID(),
		PrincipleIDs: finding.FindingPrincipleIDs(),
		Column:       finding.FindingColumn(),
		Line:         finding.FindingLine(),
	}
	if len(input.Files) == 0 && input.Finding.File != "" {
		input.Files = []string{input.Finding.File}
	}
	if input.Tool == "" {
		input.Tool = input.Finding.Tool
	}

	return Activation(input)
}
