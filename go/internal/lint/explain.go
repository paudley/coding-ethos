// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

type ExplainResult struct {
	Scope    string         `json:"scope"`
	Checks   []ExplainCheck `json:"checks"`
	Selected int            `json:"selected"`
}

type ExplainCheck struct {
	CheckID      string   `json:"check_id"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason"`
	Severity     string   `json:"severity,omitempty"`
	Evaluators   []string `json:"evaluators,omitempty"`
	EthosIDs     []string `json:"ethos_ids,omitempty"`
	FilePatterns []string `json:"file_patterns,omitempty"`
	Languages    []string `json:"languages,omitempty"`
}

func Explain(bundle policy.Bundle, scope string) (ExplainResult, error) {
	if scope == "" {
		scope = ScopeFiles
	}

	policyIDs, err := policyIDsForScope(bundle, scope)
	if err != nil {
		return ExplainResult{}, err
	}

	result := ExplainResult{
		Scope:    scope,
		Checks:   make([]ExplainCheck, 0, len(policyIDs)),
		Selected: len(policyIDs),
	}

	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return ExplainResult{}, fmt.Errorf(
				"%w: %q references %q",
				errUnknownScopePolicy,
				scope,
				policyID,
			)
		}

		result.Checks = append(result.Checks, ExplainCheck{
			CheckID:      policyID,
			Status:       "selected",
			Reason:       "selected by lint scope dispatch",
			Severity:     policyDef.DefaultSeverity,
			Evaluators:   evaluatorNames(policyDef.Evaluators),
			EthosIDs:     append([]string(nil), policyDef.PrincipleIDs...),
			FilePatterns: append([]string(nil), policyDef.AppliesTo.FilePatterns...),
			Languages:    append([]string(nil), policyDef.AppliesTo.Languages...),
		})
	}

	sort.Slice(result.Checks, func(left int, right int) bool {
		return result.Checks[left].CheckID < result.Checks[right].CheckID
	})

	return result, nil
}

func EncodeExplainResult(
	writer io.Writer,
	result ExplainResult,
	jsonOutput bool,
) error {
	if jsonOutput {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")

		return encoder.Encode(result)
	}

	_, err := fmt.Fprintln(writer, FormatExplainResultHuman(result))

	return err
}

func FormatExplainResultHuman(result ExplainResult) string {
	lines := []string{
		"lint scope: " + result.Scope,
		fmt.Sprintf("selected checks: %d", result.Selected),
	}
	for _, check := range result.Checks {
		detail := []string{check.Status}
		if check.Severity != "" {
			detail = append(detail, check.Severity)
		}
		if len(check.Evaluators) > 0 {
			detail = append(detail, "evaluators="+strings.Join(check.Evaluators, "+"))
		}

		lines = append(
			lines,
			fmt.Sprintf("- %s: %s", check.CheckID, strings.Join(detail, ", ")),
			"  reason: "+check.Reason,
		)
	}

	return strings.Join(lines, "\n")
}

func evaluatorNames(evaluators []policy.Evaluator) []string {
	names := make([]string, 0, len(evaluators))
	for _, evaluator := range evaluators {
		if evaluator.Name == "" {
			continue
		}
		names = append(names, evaluator.Name)
	}

	return names
}
