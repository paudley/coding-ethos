// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	decisionBlock  = "block"
	decisionRecord = "record"
	severityError  = "error"
	statusFail     = "fail"
	statusResolved = "resolved"
)

const (
	ScopeFiles   = "files"
	ScopeChanged = "changed"
	ScopeStaged  = "staged"
	ScopeSmoke   = "smoke"
	ScopeFull    = "full"
	ScopeCutover = "cutover"
	ScopeCommit  = "commit-msg"
)

var (
	errUnknownScopePolicy = apperror.StaticError("lint scope references unknown policy")
	errUnsupportedScope   = apperror.StaticError("unsupported lint scope")
	errMissingEvaluator   = apperror.StaticError(
		"lint policy has no registered evaluator",
	)
)

type Options struct {
	Command       string
	Cwd           string
	Scope         string
	Files         []string
	Argv          []string
	AdminApproved bool
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(
	bundle policy.Bundle,
	options Options,
	registry evaluators.Registry,
) (Result, error) {
	scope := options.Scope
	if scope == "" {
		scope = ScopeFiles
	}

	policyIDs, err := policyIDsForScope(bundle, scope)
	if err != nil {
		return Result{}, err
	}

	decisions := make([]policy.Decision, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf(
				"%w: %q references %q",
				errUnknownScopePolicy,
				scope,
				policyID,
			)
		}

		evaluated, err := evaluatePolicy(policyDef, scope, options, registry)
		if err != nil {
			return Result{}, fmt.Errorf("evaluate policy %q: %w", policyID, err)
		}

		decisions = append(decisions, evaluated...)
	}

	decisions = enrichDecisionDiagnostics(decisions, bundle.EvidenceMaps)

	result := Result{
		Scope:       scope,
		Files:       append([]string(nil), options.Files...),
		Status:      resultStatus(decisions),
		Decisions:   decisions,
		Diagnostics: diagnosticsFromDecisions(decisions),
		Findings:    findingsFromDecisions(decisions, options.Files),
	}

	return EnrichResultWithSkills(result, bundle.Skills), nil
}

func enrichDecisionDiagnostics(
	decisions []policy.Decision,
	evidenceMaps []diagnostics.EvidenceMap,
) []policy.Decision {
	if len(decisions) == 0 || len(evidenceMaps) == 0 {
		return decisions
	}

	enriched := make([]policy.Decision, 0, len(decisions))
	for _, decision := range decisions {
		decision.Diagnostics = diagnostics.Enrich(decision.Diagnostics, evidenceMaps)
		enriched = append(enriched, decision)
	}

	return enriched
}

func evaluatePolicy(
	policyDef policy.Policy,
	scope string,
	options Options,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	context := evaluators.Context{
		AdminApproved: options.AdminApproved,
		Scope:         scope,
		EventName:     "lint",
		Provider:      "lint",
		Files:         append([]string(nil), options.Files...),
		Argv:          append([]string(nil), options.Argv...),
		Command:       options.Command,
		Cwd:           options.Cwd,
	}

	if scope == ScopeCommit {
		context.Content = commitMessageContent(options.Cwd, options.Files)
	}

	switch scope {
	case ScopeChanged:
		context.ChangedFiles = append([]string(nil), options.Files...)
	case ScopeStaged:
		context.StagedFiles = append([]string(nil), options.Files...)
	}

	var registered bool

	for _, evaluatorSpec := range policyDef.Evaluators {
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			continue
		}

		context.EvaluatorOptions = evaluatorSpec.Options

		registered = true

		decisions, err := evaluator.Evaluate(policyDef, context)
		if err != nil {
			return nil, fmt.Errorf("evaluate policy %q: %w", policyDef.ID, err)
		}

		if len(decisions) > 0 {
			return attachPolicySource(decisions, policyDef), nil
		}
	}

	if len(policyDef.Evaluators) > 0 && !registered {
		return nil, fmt.Errorf("%w: %q", errMissingEvaluator, policyDef.ID)
	}

	return []policy.Decision{recordDecision(policyDef, scope, options)}, nil
}

func commitMessageContent(cwd string, files []string) string {
	messages := make([]string, 0, len(files))
	for _, file := range files {
		path := file
		if !filepath.IsAbs(path) && cwd != "" {
			path = filepath.Join(cwd, path)
		}

		content, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(content)) != "" {
			messages = append(messages, string(content))
		}
	}

	return strings.Join(messages, "\n")
}

func attachPolicySource(
	decisions []policy.Decision,
	policyDef policy.Policy,
) []policy.Decision {
	source := policySource(policyDef)
	if source == "" {
		return decisions
	}

	withSource := make([]policy.Decision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Evidence == nil {
			decision.Evidence = map[string]any{}
		}

		if _, exists := decision.Evidence["policy_source"]; !exists {
			decision.Evidence["policy_source"] = source
		}

		withSource = append(withSource, decision)
	}

	return withSource
}

func policySource(policyDef policy.Policy) string {
	if policyDef.Source.Path != "" {
		return policyDef.Source.File + ":" + policyDef.Source.Path
	}

	return policyDef.Source.File
}

func recordDecision(
	policyDef policy.Policy,
	scope string,
	options Options,
) policy.Decision {
	decision := policy.NewDecision(decisionRecord, policyDef)
	decision.Severity = decisionRecord

	decision.Evidence = map[string]any{
		"policy_source": policySource(policyDef),
		"scope":         scope,
	}
	if len(options.Files) > 0 {
		decision.Evidence["files"] = append([]string(nil), options.Files...)
	}

	if len(options.Argv) > 0 {
		decision.Evidence["argv"] = append([]string(nil), options.Argv...)
	}

	if options.Command != "" {
		decision.Evidence["command"] = options.Command
	}

	return decision
}

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == decisionBlock || decision.Severity == decisionBlock {
			return "blocked"
		}
	}

	return statusResolved
}

func diagnosticsFromDecisions(decisions []policy.Decision) []diagnostics.Diagnostic {
	diagnosticItems := []diagnostics.Diagnostic{}
	for _, decision := range decisions {
		diagnosticItems = append(diagnosticItems, decision.EvidenceDiagnostics()...)
	}

	return diagnosticItems
}

func findingsFromDecisions(
	decisions []policy.Decision,
	files []string,
) []Finding {
	findings := []Finding{}

	for _, decision := range decisions {
		if len(decision.Diagnostics) == 0 {
			findings = append(findings, findingFromDecision(decision, files))

			continue
		}

		for _, diagnostic := range decision.Diagnostics {
			findings = append(
				findings,
				findingFromDiagnostic(decision, diagnostic, files),
			)
		}
	}

	return findings
}

func findingFromDecision(
	decision policy.Decision,
	files []string,
) Finding {
	return Finding{
		CheckID:      decision.PolicyID,
		PolicyID:     decision.PolicyID,
		PolicySource: policySourceFromDecision(decision),
		Status:       statusFromDecision(decision),
		Severity:     decision.Severity,
		SkillID:      decision.EvidenceSkillID(),
		Message:      decision.Message,
		Advice:       decision.Suggestion,
		EthosIDs:     append([]string(nil), decision.PrincipleIDs...),
		Files:        filesFromDecision(decision, files),
		RawOutcome:   rawOutcomeFromDecision(decision),
		Blocking:     decisionBlocks(decision),
	}
}

func findingFromDiagnostic(
	decision policy.Decision,
	diagnostic diagnostics.Diagnostic,
	files []string,
) Finding {
	return Finding{
		CheckID:      firstNonEmpty(diagnostic.PolicyID, decision.PolicyID),
		PolicyID:     firstNonEmpty(diagnostic.PolicyID, decision.PolicyID),
		PolicySource: policySourceFromDecision(decision),
		SourceTool: firstNonEmpty(
			diagnostic.Tool,
			decision.EvidenceTool(),
		),
		Status:   statusFromDecision(decision),
		Severity: firstNonEmpty(diagnostic.Severity, decision.Severity),
		Code:     diagnostic.Code,
		File:     diagnostic.File,
		Line:     diagnostic.Line,
		Column:   diagnostic.Column,
		SkillID: firstNonEmpty(
			diagnostic.SkillID,
			decision.EvidenceSkillID(),
		),
		Message: diagnostic.Message,
		Advice:  firstNonEmpty(diagnostic.Advice, decision.Suggestion),
		EthosIDs: firstNonEmptySlice(
			diagnostic.PrincipleIDs,
			decision.PrincipleIDs,
		),
		Files:      filesFromDecision(decision, files),
		RawOutcome: rawOutcomeFromDecision(decision),
		Blocking:   decisionBlocks(decision),
	}
}

func statusFromDecision(decision policy.Decision) string {
	if decisionBlocks(decision) {
		return "fail"
	}

	if decision.Decision == decisionRecord || decision.Severity == decisionRecord {
		return "pass"
	}

	return decision.Decision
}

func decisionBlocks(decision policy.Decision) bool {
	return decision.Decision == decisionBlock || decision.Severity == decisionBlock
}

func filesFromDecision(decision policy.Decision, fallback []string) []string {
	if files := decision.EvidenceFiles(); len(files) > 0 {
		return files
	}

	return append([]string(nil), fallback...)
}

func rawOutcomeFromDecision(decision policy.Decision) map[string]any {
	if len(decision.Evidence) == 0 {
		return nil
	}

	raw := make(map[string]any, len(decision.Evidence))
	for key, value := range decision.Evidence {
		switch key {
		case "stdout", "stderr":
			if text, ok := value.(string); ok && len(text) > 500 {
				raw[key] = text[:500]

				continue
			}
		}

		raw[key] = value
	}

	return raw
}

func policySourceFromDecision(decision policy.Decision) string {
	return firstNonEmpty(
		decision.EvidenceString("policy_source"),
		decision.EvidenceCommand(),
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func firstNonEmptySlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return append([]string(nil), value...)
		}
	}

	return nil
}

func policyIDsForScope(bundle policy.Bundle, scope string) ([]string, error) {
	if scope == ScopeChanged {
		scope = ScopeFiles
	}

	allowedScopes := []string{
		ScopeFiles,
		ScopeStaged,
		ScopeSmoke,
		ScopeFull,
		ScopeCutover,
		ScopeCommit,
	}
	if !slices.Contains(allowedScopes, scope) {
		return nil, fmt.Errorf(
			"unsupported lint scope %q: %w",
			scope,
			errUnsupportedScope,
		)
	}

	policyIDs, ok := bundle.Dispatch.Linter[scope]
	if !ok {
		return nil, nil
	}

	return append([]string(nil), policyIDs...), nil
}
