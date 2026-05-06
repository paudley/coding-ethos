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

	activation := celActivation(context, source)

	output, _, err := program.Eval(activation)
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
		activation,
	)}

	return []policy.Decision{decision}, nil
}

func celDiagnostic(
	context Context,
	policyDef policy.Policy,
	decisionMode string,
	source string,
	activation map[string]any,
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
		diagnostic.Detail = context.Diagnostic.Detail
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

	if policyDef.ID == "filesystem.line_limits" {
		applyLineLimitFileDiagnostic(&diagnostic, activation)

		return diagnostic
	}

	if symbol, ok := firstGrowingProposedSymbol(activation); ok {
		diagnostic.File = symbol.File
		diagnostic.Line = int(symbol.ProposedStartLine)
		diagnostic.Metadata["ast_action"] = symbol.Action
		diagnostic.Metadata["ast_change_source"] = "proposed"
		diagnostic.Metadata["ast_language"] = symbol.Language
		diagnostic.Metadata["ast_line_delta"] = symbol.LineDelta
		diagnostic.Metadata["ast_nonblank_line_delta"] = symbol.NonBlankLineDelta
		diagnostic.Metadata["ast_node_kind"] = symbol.NodeKind
		diagnostic.Metadata["ast_symbol_kind"] = symbol.SymbolKind
		diagnostic.Metadata["ast_symbol_name"] = symbol.SymbolName
		diagnostic.Metadata["ast_symbol_path"] = symbol.SymbolPath
		diagnostic.Metadata["current_line_count"] = symbol.CurrentLineCount
		diagnostic.Metadata["current_nonblank_line_count"] = symbol.CurrentNonBlankLineCount
		diagnostic.Metadata["proposed_line_count"] = symbol.ProposedLineCount
		diagnostic.Metadata["proposed_nonblank_line_count"] = symbol.ProposedNonBlankLineCount
	} else if symbol, ok := firstGrowingChangedSymbol(activation); ok {
		diagnostic.File = symbol.File
		diagnostic.Line = int(symbol.CurrentStartLine)
		diagnostic.Metadata["ast_action"] = symbol.Action
		diagnostic.Metadata["ast_change_source"] = "staged"
		diagnostic.Metadata["ast_language"] = symbol.Language
		diagnostic.Metadata["ast_line_delta"] = symbol.LineDelta
		diagnostic.Metadata["ast_nonblank_line_delta"] = symbol.NonBlankLineDelta
		diagnostic.Metadata["ast_node_kind"] = symbol.NodeKind
		diagnostic.Metadata["ast_symbol_kind"] = symbol.SymbolKind
		diagnostic.Metadata["ast_symbol_name"] = symbol.SymbolName
		diagnostic.Metadata["ast_symbol_path"] = symbol.SymbolPath
		diagnostic.Metadata["current_line_count"] = symbol.CurrentLineCount
		diagnostic.Metadata["current_nonblank_line_count"] = symbol.CurrentNonBlankLineCount
		diagnostic.Metadata["original_line_count"] = symbol.OriginalLineCount
		diagnostic.Metadata["original_nonblank_line_count"] = symbol.OriginalNonBlankLineCount
	}

	return diagnostic
}

func applyLineLimitFileDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
) {
	if file, ok := firstLineLimitProposedFile(activation); ok {
		diagnostic.File = file.File
		diagnostic.Metadata["line_limit_change_source"] = "proposed"
		diagnostic.Metadata["current_line_count"] = file.CurrentLineCount
		diagnostic.Metadata["current_nonblank_line_count"] = file.CurrentNonBlankLineCount
		diagnostic.Metadata["proposed_line_count"] = file.ProposedLineCount
		diagnostic.Metadata["proposed_nonblank_line_count"] = file.ProposedNonBlankLineCount

		return
	}

	if file, ok := firstLineLimitChangedFile(activation); ok {
		diagnostic.File = file.File
		diagnostic.Metadata["line_limit_change_source"] = "staged"
		diagnostic.Metadata["current_line_count"] = file.LineCount
		diagnostic.Metadata["current_nonblank_line_count"] = file.NonBlankLineCount
		diagnostic.Metadata["original_line_count"] = file.OriginalLineCount
		diagnostic.Metadata["original_nonblank_line_count"] = file.OriginalNonBlankLineCount
	}
}

func firstLineLimitProposedFile(
	activation map[string]any,
) (celexpr.ProposedFileChangeInput, bool) {
	files, ok := activation["proposed_file_changes"].([]celexpr.ProposedFileChangeInput)
	if !ok {
		return celexpr.ProposedFileChangeInput{}, false
	}

	for _, file := range files {
		if proposedFileMatchesLineLimit(file) {
			return file, true
		}
	}

	return celexpr.ProposedFileChangeInput{}, false
}

func proposedFileMatchesLineLimit(file celexpr.ProposedFileChangeInput) bool {
	return !file.IsBinary &&
		!file.IsTest &&
		file.LineCountGrows &&
		file.NonBlankLineCountGrows &&
		fileExceedsLineLimit(file.Ext, file.File, file.ProposedLineCount)
}

func firstLineLimitChangedFile(
	activation map[string]any,
) (celexpr.FileChangeInput, bool) {
	proposedFiles, ok := activation["proposed_file_changes"].([]celexpr.ProposedFileChangeInput)
	if ok && len(proposedFiles) != 0 {
		return celexpr.FileChangeInput{}, false
	}

	files, ok := activation["file_changes"].([]celexpr.FileChangeInput)
	if !ok {
		return celexpr.FileChangeInput{}, false
	}

	for _, file := range files {
		if changedFileMatchesLineLimit(file) {
			return file, true
		}
	}

	return celexpr.FileChangeInput{}, false
}

func changedFileMatchesLineLimit(file celexpr.FileChangeInput) bool {
	return !file.IsBinary &&
		!file.IsTest &&
		fileExceedsLineLimit(file.Ext, file.File, file.LineCount) &&
		(file.OriginalLineCount < 0 || file.LineCount > file.OriginalLineCount) &&
		(file.OriginalNonBlankLineCount < 0 || file.NonBlankLineCountGrows)
}

func fileExceedsLineLimit(ext, file string, lineCount int64) bool {
	switch {
	case ext == ".py":
		return lineCount > 1000
	case ext == ".go":
		return lineCount > 2000
	case ext == ".sh" || ext == ".bash" || strings.HasPrefix(file, "scripts/"):
		return lineCount > 500
	default:
		return false
	}
}

func firstGrowingProposedSymbol(
	activation map[string]any,
) (celexpr.ProposedSymbolChangeInput, bool) {
	symbols, ok := activation["proposed_symbol_changes"].([]celexpr.ProposedSymbolChangeInput)
	if !ok {
		return celexpr.ProposedSymbolChangeInput{}, false
	}

	for _, symbol := range symbols {
		if symbol.LineCountGrows {
			return symbol, true
		}
	}

	return celexpr.ProposedSymbolChangeInput{}, false
}

func firstGrowingChangedSymbol(
	activation map[string]any,
) (celexpr.ChangedSymbolInput, bool) {
	symbols, ok := activation["changed_symbols"].([]celexpr.ChangedSymbolInput)
	if !ok {
		return celexpr.ChangedSymbolInput{}, false
	}

	for _, symbol := range symbols {
		if symbol.LineCountGrows {
			return symbol, true
		}
	}

	return celexpr.ChangedSymbolInput{}, false
}

func policySource(policyDef policy.Policy) string {
	if policyDef.Source.Path != "" {
		return policyDef.Source.File + ":" + policyDef.Source.Path
	}

	return policyDef.Source.File
}

func celActivation(context Context, source string) map[string]any {
	return celexpr.Activation(celexpr.ActivationInput{
		Argv:               context.Argv,
		Command:            context.Command,
		Content:            context.Content,
		OldContent:         context.OldContent,
		Cwd:                context.Cwd,
		EventName:          context.EventName,
		EventMatcher:       context.EventMatcher,
		EventSource:        context.EventSource,
		Files:              context.Files,
		ChangedFiles:       context.ChangedFiles,
		StagedFiles:        context.StagedFiles,
		Provider:           context.Provider,
		Mode:               stringOption(context.EvaluatorOptions, "mode", ""),
		Scope:              context.Scope,
		SessionID:          context.SessionID,
		Tool:               context.Tool,
		ToolInputKeys:      context.ToolInputKeys,
		ToolResponseKeys:   context.ToolResponseKeys,
		TranscriptPath:     context.TranscriptPath,
		ReturnCode:         context.ReturnCode,
		HasToolInput:       context.HasToolInput,
		HasToolResponse:    context.HasToolResponse,
		AdminApproved:      context.AdminApproved,
		ReadOnlyInspection: context.ReadOnlyInspection,
		Diagnostic:         context.Diagnostic,
		Diagnostics:        context.Diagnostics,
		Findings:           celFindings(context.Findings),
		PythonASTFacts:     celPythonASTFacts(context, source),
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
		RequiredIgnores: stringSliceOption(
			context.EvaluatorOptions,
			"required_ignore_paths",
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

	if branch := stringOption(
		context.EvaluatorOptions,
		"current_branch",
		"",
	); branch != "" {
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
