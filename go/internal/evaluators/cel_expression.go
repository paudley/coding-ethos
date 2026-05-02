// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateCELExpression(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	source := strings.TrimSpace(stringOption(context.EvaluatorOptions, "when", ""))
	if source == "" {
		return nil, fmt.Errorf("CEL expression policy %q missing when", policyDef.ID)
	}

	program, err := celexpr.Program(policyDef.ID, source)
	if err != nil {
		return nil, err
	}

	output, _, err := program.Eval(celActivation(context))
	if err != nil {
		return nil, fmt.Errorf("evaluate CEL expression: %w", err)
	}

	matched, ok := output.Value().(bool)
	if !ok || !matched {
		return nil, nil
	}

	decisionMode := strings.TrimSpace(policyDef.DefaultSeverity)
	if decisionMode == "" {
		decisionMode = blockDecision
	}
	decision := policy.NewDecision(decisionMode, policyDef)
	decision.Evidence = map[string]any{
		"argv":                 append([]string(nil), context.Argv...),
		"command":              context.Command,
		"files":                append([]string(nil), context.Files...),
		"implementation":       "cel",
		"input_schema_version": celexpr.SchemaVersion,
		"scope":                context.Scope,
		"when":                 source,
	}
	if context.Tool != "" {
		decision.Evidence["tool"] = context.Tool
	}
	if policyDef.Source.File != "" {
		decision.Evidence["policy_source"] = policySource(policyDef)
	}
	if skillID := stringOption(context.EvaluatorOptions, "skill_id", ""); skillID != "" {
		decision.Evidence["skill_id"] = skillID
	}
	decision.Diagnostics = []diagnostics.Diagnostic{celDiagnostic(
		context,
		policyDef,
		decisionMode,
		source,
	)}

	return []policy.Decision{decision}, nil
}

func celDiagnostic(
	context Context,
	policyDef policy.Policy,
	decisionMode string,
	source string,
) diagnostics.Diagnostic {
	diagnostic := diagnostics.Diagnostic{
		Tool:         "policy",
		Severity:     decisionMode,
		PolicyID:     policyDef.ID,
		SkillID:      stringOption(context.EvaluatorOptions, "skill_id", ""),
		Message:      policyDef.Message,
		Advice:       policyDef.Suggestion,
		PrincipleIDs: append([]string(nil), policyDef.PrincipleIDs...),
		Metadata: map[string]any{
			"implementation":       "cel",
			"input_schema_version": celexpr.SchemaVersion,
			"policy_source":        policySource(policyDef),
			"when":                 source,
		},
	}
	if context.Diagnostic != nil {
		diagnostic.Tool = context.Diagnostic.Tool
		diagnostic.Code = context.Diagnostic.Code
		diagnostic.File = context.Diagnostic.File
		diagnostic.Line = context.Diagnostic.Line
		diagnostic.Column = context.Diagnostic.Column
		diagnostic.Metadata["matched_diagnostic_policy_id"] = context.Diagnostic.PolicyID
		diagnostic.Metadata["matched_diagnostic_severity"] = context.Diagnostic.Severity

		return diagnostic
	}
	if len(context.Files) == 1 {
		diagnostic.File = context.Files[0]
	}

	return diagnostic
}

func policySource(policyDef policy.Policy) string {
	if policyDef.Source.Path != "" {
		return policyDef.Source.File + ":" + policyDef.Source.Path
	}

	return policyDef.Source.File
}

func celActivation(context Context) map[string]any {
	return celexpr.Activation(celexpr.ActivationInput{
		Argv:             context.Argv,
		Command:          context.Command,
		Cwd:              context.Cwd,
		EventName:        context.EventName,
		EventMatcher:     context.EventMatcher,
		EventSource:      context.EventSource,
		Files:            context.Files,
		ChangedFiles:     context.ChangedFiles,
		StagedFiles:      context.StagedFiles,
		Provider:         context.Provider,
		Mode:             stringOption(context.EvaluatorOptions, "mode", ""),
		Scope:            context.Scope,
		SessionID:        context.SessionID,
		Tool:             context.Tool,
		ToolInputKeys:    context.ToolInputKeys,
		ToolResponseKeys: context.ToolResponseKeys,
		TranscriptPath:   context.TranscriptPath,
		ReturnCode:       context.ReturnCode,
		HasToolInput:     context.HasToolInput,
		HasToolResponse:  context.HasToolResponse,
		AdminApproved:    context.AdminApproved,
		Diagnostic:       context.Diagnostic,
		Diagnostics:      context.Diagnostics,
		Findings:         celFindings(context.Findings),
		ProtectedPaths: stringSliceOption(
			context.EvaluatorOptions,
			"protected_paths",
			nil,
		),
		ProtectedBranches: stringSliceOption(
			context.EvaluatorOptions,
			"protected_branches",
			nil,
		),
		ConfigCandidates: stringSliceOption(
			context.EvaluatorOptions,
			"config_candidates",
			nil,
		),
		CurrentBranch: celCurrentBranch(context),
		SourceRoots:   stringSliceOption(context.EvaluatorOptions, "source_roots", nil),
		PythonVersion: stringOption(context.EvaluatorOptions, "python_version", ""),
	})
}

func celCurrentBranch(context Context) string {
	if branch := strings.TrimSpace(context.CurrentBranch); branch != "" {
		return branch
	}
	if branch := stringOption(context.EvaluatorOptions, "current_branch", ""); branch != "" {
		return branch
	}
	if context.Cwd == "" {
		return ""
	}
	branch, ok := currentBranch(context.Cwd)
	if !ok {
		return ""
	}

	return branch
}

func celFindings(findings []Finding) []celexpr.FindingActivation {
	activations := make([]celexpr.FindingActivation, 0, len(findings))
	for _, finding := range findings {
		activations = append(activations, celexpr.FindingActivation{
			Tool:         finding.Tool,
			Code:         finding.Code,
			Message:      finding.Message,
			File:         finding.File,
			Severity:     finding.Severity,
			PolicyID:     finding.PolicyID,
			SkillID:      finding.SkillID,
			PrincipleIDs: append([]string(nil), finding.PrincipleIDs...),
			Column:       finding.Column,
			Line:         finding.Line,
		})
	}

	return activations
}
