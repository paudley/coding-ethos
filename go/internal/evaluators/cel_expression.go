// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type (
	proposedSymbolChangeInputs = []celexpr.ProposedSymbolChangeInput
	proposedFileChangeInputs   = []celexpr.ProposedFileChangeInput
)

const (
	bashExtension                = ".bash"
	defaultGoHardLineLimit       = 2000
	defaultPythonHardLineLimit   = 1000
	defaultShellHardLineLimit    = 500
	defaultCoverageFloor         = 80.0
	defaultCoverageGoal          = 90.0
	coverageThresholdsOption     = "coverage_thresholds"
	goExtension                  = ".go"
	lineLimitThresholdsOption    = "line_limit_thresholds"
	metadataScopeBeforeRun       = "changed_file_scope_before_run"
	metadataUnsafeUnscopedRun    = "unsafe_unscoped_path_sensitive_run"
	pythonExtension              = ".py"
	pythonSuppressionWritePolicy = "python.suppression_in_write_method"
	shellExtension               = ".sh"
	scriptsPrefix                = "scripts/"
)

type lineLimitThresholds struct {
	goHard     int64
	pythonHard int64
	shellHard  int64
}

func EvaluateCELExpression(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	source := strings.TrimSpace(stringOption(context.EvaluatorOptions, "when", ""))
	if source == "" {
		return nil, apperror.Wrapf(
			apperror.StaticError("CEL expression policy %q missing when"),
			"CEL expression policy %q missing when",
			policyDef.ID,
		)
	}

	program, err := celexpr.Program(policyDef.ID, source)
	if err != nil {
		return nil, fmt.Errorf(
			"compile CEL expression for policy %s: %w",
			policyDef.ID,
			err,
		)
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
		applyMatchedDiagnostic(&diagnostic, context.Diagnostic)

		return diagnostic
	}

	if len(context.Files) == 1 {
		diagnostic.File = context.Files[0]
	}

	if policyDef.ID == filesystemLineLimitsPolicy {
		applyLineLimitFileDiagnostic(
			&diagnostic,
			activation,
			context.EvaluatorOptions,
		)

		return diagnostic
	}

	if policyDef.ID == hookChangedFileScopePolicy {
		applyHookCommandDiagnostic(&diagnostic, activation)

		return diagnostic
	}

	if policyDef.ID == similarCodeDetectedPolicy {
		applySimilarityDiagnostic(&diagnostic, activation)

		return diagnostic
	}

	if policyDef.ID == pythonSuppressionWritePolicy {
		applyPythonSuppressionDiagnostic(&diagnostic, activation)

		return diagnostic
	}

	applyGrowingSymbolDiagnostic(&diagnostic, activation)

	return diagnostic
}

func applyPythonSuppressionDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
) {
	fact, ok := firstPythonSuppressionInWriteMethod(activation)
	if !ok {
		return
	}

	diagnostic.File = fact.File
	diagnostic.Line = int(fact.Line)
	diagnostic.Column = int(fact.Column)
	diagnostic.Code = fact.SuppressionLabel
	diagnostic.Detail = fact.Text
	diagnostic.Metadata["ast_change_source"] = "source"
	diagnostic.Metadata["ast_language"] = fact.Language
	diagnostic.Metadata["ast_node_kind"] = fact.NodeKind
	diagnostic.Metadata["ast_symbol_kind"] = fact.SymbolKind
	diagnostic.Metadata["ast_symbol_path"] = fact.SymbolPath
	diagnostic.Metadata["enclosing_function"] = fact.EnclosingFunction
	diagnostic.Metadata["enclosing_symbol"] = fact.EnclosingSymbol
	diagnostic.Metadata["suppression_label"] = fact.SuppressionLabel
}

func firstPythonSuppressionInWriteMethod(
	activation map[string]any,
) (celexpr.PythonASTFactInput, bool) {
	facts, ok := activation["python_ast"].([]celexpr.PythonASTFactInput)
	if !ok {
		return celexpr.PythonASTFactInput{}, false
	}

	for _, fact := range facts {
		if fact.IsSuppression && pythonWriteFunctionName(fact.EnclosingFunction) {
			return fact, true
		}
	}

	return celexpr.PythonASTFactInput{}, false
}

func pythonWriteFunctionName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}

	prefixes := []string{
		"append", "commit", "create", "delete", "emit", "flush", "index",
		"ingest", "insert", "persist", "record", "remove", "replace",
		"save", "store", "sync", "update", "upsert", "write",
	}
	for _, prefix := range prefixes {
		if name == prefix || strings.HasPrefix(name, prefix+"_") {
			return true
		}
	}

	return false
}

func applyHookCommandDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
) {
	command, ok := firstUnsafeHookCommand(activation)
	if !ok {
		return
	}

	diagnostic.File = command.File
	diagnostic.Line = int(command.Line)
	diagnostic.Metadata["ast_language"] = "go"
	diagnostic.Metadata["ast_node_kind"] = "function_declaration"
	diagnostic.Metadata["ast_symbol_kind"] = "function"
	diagnostic.Metadata["ast_symbol_name"] = command.SymbolName
	diagnostic.Metadata["ast_symbol_path"] = command.SymbolPath
	diagnostic.Metadata[metadataScopeBeforeRun] = command.ChangedFileScopeBeforeRun
	diagnostic.Metadata["hook_command_calls"] = append([]string(nil), command.CallNames...)
	diagnostic.Metadata["runs_path_sensitive_check"] = command.RunsPathSensitiveCheck
	diagnostic.Metadata[metadataUnsafeUnscopedRun] = command.UnsafeUnscopedPathSensitiveRun
}

func firstUnsafeHookCommand(
	activation map[string]any,
) (celexpr.HookCommandInput, bool) {
	commands, ok := activation["hook_commands"].([]celexpr.HookCommandInput)
	if !ok {
		return celexpr.HookCommandInput{}, false
	}

	for _, command := range commands {
		if command.UnsafeUnscopedPathSensitiveRun {
			return command, true
		}
	}

	return celexpr.HookCommandInput{}, false
}

func applyGrowingSymbolDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
) {
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

		return
	}

	if symbol, ok := firstGrowingChangedSymbol(activation); ok {
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
}

func applyMatchedDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	matched *diagnostics.Diagnostic,
) {
	diagnostic.Tool = matched.Tool
	diagnostic.Code = matched.Code
	diagnostic.Detail = matched.Detail
	diagnostic.File = matched.File
	diagnostic.Line = matched.Line
	diagnostic.Column = matched.Column
	maps.Copy(diagnostic.Metadata, matched.Metadata)

	diagnostic.Metadata["matched_diagnostic_policy_id"] = matched.PolicyID
	diagnostic.Metadata["matched_diagnostic_severity"] = matched.Severity
}

func applyLineLimitFileDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
	options map[string]any,
) {
	thresholds := lineLimitThresholdsFromOptions(options)

	if file, ok := firstLineLimitProposedFile(activation, thresholds); ok {
		diagnostic.File = file.File
		diagnostic.Metadata["line_limit_change_source"] = "proposed"
		diagnostic.Metadata["current_line_count"] = file.CurrentLineCount
		diagnostic.Metadata["current_nonblank_line_count"] = file.CurrentNonBlankLineCount
		diagnostic.Metadata["proposed_line_count"] = file.ProposedLineCount
		diagnostic.Metadata["proposed_nonblank_line_count"] = file.ProposedNonBlankLineCount

		return
	}

	if file, ok := firstLineLimitChangedFile(activation, thresholds); ok {
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
	thresholds lineLimitThresholds,
) (celexpr.ProposedFileChangeInput, bool) {
	files, ok := activation["proposed_file_changes"].([]celexpr.ProposedFileChangeInput)
	if !ok {
		return celexpr.ProposedFileChangeInput{}, false
	}

	for _, file := range files {
		if proposedFileMatchesLineLimit(file, thresholds) {
			return file, true
		}
	}

	return celexpr.ProposedFileChangeInput{}, false
}

func proposedFileMatchesLineLimit(
	file celexpr.ProposedFileChangeInput,
	thresholds lineLimitThresholds,
) bool {
	return !file.IsBinary &&
		!file.IsTest &&
		file.LineCountGrows &&
		file.NonBlankLineCountGrows &&
		proposedFileExceedsLineLimit(file, thresholds)
}

func proposedFileExceedsLineLimit(
	file celexpr.ProposedFileChangeInput,
	thresholds lineLimitThresholds,
) bool {
	return fileExceedsLineLimit(
		file.File,
		file.Ext,
		file.ProposedLineCount,
		thresholds,
	)
}

func firstLineLimitChangedFile(
	activation map[string]any,
	thresholds lineLimitThresholds,
) (celexpr.FileChangeInput, bool) {
	proposedFiles, found := proposedFileChanges(activation)
	if found && len(proposedFiles) != 0 {
		return celexpr.FileChangeInput{}, false
	}

	files, ok := activation["file_changes"].([]celexpr.FileChangeInput)
	if !ok {
		return celexpr.FileChangeInput{}, false
	}

	for _, file := range files {
		if changedFileMatchesLineLimit(file, thresholds) {
			return file, true
		}
	}

	return celexpr.FileChangeInput{}, false
}

func changedFileMatchesLineLimit(
	file celexpr.FileChangeInput,
	thresholds lineLimitThresholds,
) bool {
	return !file.IsBinary &&
		!file.IsTest &&
		changedFileExceedsLineLimit(file, thresholds) &&
		(file.OriginalLineCount < 0 || file.LineCount > file.OriginalLineCount) &&
		(file.OriginalNonBlankLineCount < 0 || file.NonBlankLineCountGrows)
}

func changedFileExceedsLineLimit(
	file celexpr.FileChangeInput,
	thresholds lineLimitThresholds,
) bool {
	return fileExceedsLineLimit(file.File, file.Ext, file.LineCount, thresholds)
}

func fileExceedsLineLimit(
	file string,
	extension string,
	lineCount int64,
	thresholds lineLimitThresholds,
) bool {
	exceedsPythonLimit := extension == pythonExtension &&
		lineCount > thresholds.pythonHard
	exceedsGoLimit := extension == goExtension &&
		lineCount > thresholds.goHard
	exceedsShellLimit := isScriptLineLimitCandidate(file, extension) &&
		lineCount > thresholds.shellHard

	return exceedsPythonLimit || exceedsGoLimit || exceedsShellLimit
}

func isScriptLineLimitCandidate(file, extension string) bool {
	return extension == shellExtension ||
		extension == bashExtension ||
		(extension == "" && strings.HasPrefix(file, scriptsPrefix))
}

func lineLimitThresholdsFromOptions(options map[string]any) lineLimitThresholds {
	thresholds := lineLimitThresholds{
		goHard:     defaultGoHardLineLimit,
		pythonHard: defaultPythonHardLineLimit,
		shellHard:  defaultShellHardLineLimit,
	}

	raw, ok := options[lineLimitThresholdsOption].(map[string]any)
	if !ok {
		return thresholds
	}

	thresholds.goHard = int64(intValueFromMap(
		raw,
		"go_hard",
		int(thresholds.goHard),
	))
	thresholds.pythonHard = int64(intValueFromMap(
		raw,
		"python_hard",
		int(thresholds.pythonHard),
	))
	thresholds.shellHard = int64(intValueFromMap(
		raw,
		"shell_hard",
		int(thresholds.shellHard),
	))

	return thresholds
}

func intValueFromMap(values map[string]any, key string, defaultValue int) int {
	value, ok := values[key]
	if !ok {
		return defaultValue
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return defaultValue
	}
}

func celCoverageThresholds(
	options map[string]any,
) celexpr.CoverageThresholdsInput {
	raw := map[string]any(nil)
	if typed, found := options[coverageThresholdsOption].(map[string]any); found {
		raw = typed
	}

	return celexpr.CoverageThresholdsInput{
		Project:  coverageThresholdBand(raw, "project"),
		Package:  coverageThresholdBand(raw, "package"),
		File:     coverageThresholdBand(raw, "file"),
		Function: coverageThresholdBand(raw, "function"),
	}
}

func coverageThresholdBand(
	thresholds map[string]any,
	key string,
) celexpr.CoverageThresholdBandInput {
	raw := map[string]any(nil)
	if typed, found := thresholds[key].(map[string]any); found {
		raw = typed
	}

	floor := floatValueFromMap(raw, "floor", defaultCoverageFloor)
	goal := floatValueFromMap(raw, "goal", defaultCoverageGoal)

	return celexpr.CoverageThresholdBandInput{
		Floor:  floor,
		Goal:   goal,
		High:   floatValueFromMap(raw, "high", goal),
		Medium: floatValueFromMap(raw, "medium", floor),
		Low:    floatValueFromMap(raw, "low", 0),
	}
}

func floatValueFromMap(
	values map[string]any,
	key string,
	defaultValue float64,
) float64 {
	value, ok := values[key]
	if !ok {
		return defaultValue
	}

	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}

	return defaultValue
}

func firstGrowingProposedSymbol(
	activation map[string]any,
) (celexpr.ProposedSymbolChangeInput, bool) {
	symbols, ok := proposedSymbolChanges(activation)
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

func proposedFileChanges(
	activation map[string]any,
) ([]celexpr.ProposedFileChangeInput, bool) {
	files, ok := activation["proposed_file_changes"].(proposedFileChangeInputs)

	return files, ok
}

func proposedSymbolChanges(
	activation map[string]any,
) ([]celexpr.ProposedSymbolChangeInput, bool) {
	symbols, ok := activation["proposed_symbol_changes"].(proposedSymbolChangeInputs)

	return symbols, ok
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
		StrategicIntent:    context.StrategicIntent,
		ActiveTodo:         context.ActiveTodo,
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
		HookCommands:       celHookCommands(context, source),
		LineLimits:         celLineLimitThresholds(context.EvaluatorOptions),
		CoverageThresholds: celCoverageThresholds(context.EvaluatorOptions),
		PythonASTFacts:     celPythonASTFacts(context, source),
		SimilarityFacts:    celSimilarityFacts(context, source),
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

func celLineLimitThresholds(options map[string]any) celexpr.LineLimitInput {
	thresholds := lineLimitThresholdsFromOptions(options)

	return celexpr.LineLimitInput{
		GoHard:     thresholds.goHard,
		PythonHard: thresholds.pythonHard,
		ShellHard:  thresholds.shellHard,
	}
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
