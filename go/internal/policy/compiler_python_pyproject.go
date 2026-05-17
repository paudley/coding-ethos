// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

func pyprojectIgnoresPolicySpec(
	config map[string]any,
	principles map[string]Principle,
) compiledPolicySpec {
	policyID := "python.pyproject_ignores"
	options := map[string]any{
		"allowed_ignore_patterns": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_ignore_patterns"},
			nil,
		),
		"allowed_exclude_patterns": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_exclude_patterns"},
			nil,
		),
		"allowed_mypy_missing_imports": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_mypy_missing_imports"},
			nil,
		),
	}

	policy := Policy{
		ID:       policyID,
		Category: "python",
		Source: SourceRef{
			File: "config.yaml",
			Path: "python.pyproject_ignores",
		},
		PrincipleIDs: principleRefs(
			principles,
			"linting-as-code-quality-enforcement",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         pythonPolicyMessage(policyID),
		Suggestion:      pythonPolicySuggestion(policyID),
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			FilePatterns: []string{"pyproject.toml", "**/pyproject.toml"},
		},
		Evaluators: []Evaluator{{
			Kind:    "toml",
			Name:    policyID,
			Options: options,
		}},
	}

	return compiledPolicySpec{
		ID:          policyID,
		EnabledPath: []string{"python", "pyproject_ignores"},
		Policy:      policy,
	}
}

func uvExcludeNewerPolicySpec(
	config map[string]any,
	principles map[string]Principle,
) compiledPolicySpec {
	policyID := "python.uv_exclude_newer"

	expectedValue := stringAt(config, "python", "uv_exclude_newer", "expected_value")
	if expectedValue == "" {
		expectedValue = "7 days"
	}

	options := map[string]any{
		"expected_value": expectedValue,
	}

	policy := Policy{
		ID:       policyID,
		Category: "python",
		Source: SourceRef{
			File: "config.yaml",
			Path: "python.uv_exclude_newer",
		},
		PrincipleIDs:    principleRefs(principles, "security-by-design"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         pythonPolicyMessage(policyID),
		Suggestion:      pythonPolicySuggestion(policyID),
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			FilePatterns: []string{"pyproject.toml", "**/pyproject.toml"},
		},
		Evaluators: []Evaluator{{
			Kind:    "toml",
			Name:    policyID,
			Options: options,
		}},
	}

	return compiledPolicySpec{
		ID:          policyID,
		EnabledPath: []string{"python", "uv_exclude_newer"},
		Policy:      policy,
	}
}
