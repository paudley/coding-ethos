// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

const (
	pythonConditionalImportsPolicy = "python.conditional_imports"
	pythonPyprojectIgnoresPolicy   = "python.pyproject_ignores"
	pythonUVExcludeNewerPolicy     = "python.uv_exclude_newer"
)

func addConfiguredPythonPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	for _, spec := range pythonPolicySpecs(config, principles) {
		addPolicyIfEnabled(
			policies,
			config,
			principles,
			spec.ID,
			spec.EnabledPath,
			spec.Policy,
		)
	}
}

type compiledPolicySpec struct {
	Policy      Policy
	ID          string
	EnabledPath []string
}

func pythonPolicySpecs(
	config map[string]any,
	principles map[string]Principle,
) []compiledPolicySpec {
	return []compiledPolicySpec{
		pythonPolicySpec(
			pythonConditionalImportsPolicy,
			[]string{"python", "conditional_imports"},
			principleRefs(principles, "no-conditional-imports"),
		),
		pythonPolicySpec(
			"python.functional_idioms",
			[]string{"python", "functional_idioms"},
			principleRefs(principles, "functional-idioms"),
		),
		pythonPolicySpec(
			"python.optional_returns",
			[]string{"python", "optional_returns"},
			principleRefs(principles, "no-optional-types-for-required-dependencies"),
		),
		pythonPolicySpec(
			"python.catch_and_silence",
			[]string{"python", "catch_and_silence"},
			principleRefs(
				principles,
				"fail-fast-fail-hard-overview",
				"exception-hierarchy-and-error-messages",
			),
		),
		pythonPolicySpec(
			"python.structured_logging",
			[]string{"python", "structured_logging"},
			principleRefs(principles, "radical-visibility"),
		),
		pythonPolicySpec(
			"python.direct_imports",
			[]string{"python", "direct_imports"},
			principleRefs(principles, "protocol-first-design"),
		),
		pythonPolicySpec(
			"python.bare_except",
			[]string{"python", "catch_and_silence"},
			principleRefs(principles, "exception-hierarchy-and-error-messages"),
		),
		pythonPolicySpec(
			"python.unexplained_type_ignore",
			[]string{"python", "comment_suppressions"},
			principleRefs(principles, "linting-as-code-quality-enforcement"),
		),
		pyprojectIgnoresPolicySpec(config, principles),
		uvExcludeNewerPolicySpec(config, principles),
		pytestGatePolicySpec(config, principles),
	}
}

func pythonPolicySpec(
	policyID string,
	enabledPath []string,
	principleIDs []string,
) compiledPolicySpec {
	policy := Policy{
		ID:              policyID,
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: policyID},
		PrincipleIDs:    principleIDs,
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         pythonPolicyMessage(policyID),
		Suggestion:      pythonPolicySuggestion(policyID),
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Languages:    []string{"python"},
			FilePatterns: []string{"**/*.py"},
		},
		Evaluators: []Evaluator{{Kind: "ast", Name: policyID}},
	}

	return compiledPolicySpec{ID: policyID, EnabledPath: enabledPath, Policy: policy}
}

func pytestGatePolicySpec(
	config map[string]any,
	principles map[string]Principle,
) compiledPolicySpec {
	command := stringSliceAt(
		config,
		[]string{"python", "pytest_gate", "test_command"},
		[]string{"pytest"},
	)

	policy := Policy{
		ID:              "pytest.gate",
		Category:        "pytest",
		Source:          SourceRef{File: "config.yaml", Path: "python.pytest_gate"},
		PrincipleIDs:    principleRefs(principles, "testing-as-specification"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "prepare", "annotate", "record"},
		Message:         "The configured pytest gate must pass before claiming readiness.",
		Suggestion:      "Run the configured pytest gate and address failures.",
		DefenseLayers:   PytestDefenseLayers(),
		AppliesTo: AppliesTo{
			Commands: []string{"pytest"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{
			Kind:    "external",
			Name:    "pytest.gate",
			Options: map[string]any{"command": command},
		}},
	}

	return compiledPolicySpec{
		ID:          "pytest.gate",
		EnabledPath: []string{"python", "pytest_gate"},
		Policy:      policy,
	}
}

func pythonPolicyMessage(policyID string) string {
	switch policyID {
	case pythonConditionalImportsPolicy:
		return sentence(
			"Required dependencies should fail immediately;",
			"conditional, nested, or dynamic imports create soft dependency paths.",
		)
	case "python.functional_idioms":
		return sentence(
			"Ad-hoc closures and assigned lambdas obscure reusable behavior;",
			"use Python's functional helpers when they make intent clearer.",
		)
	case "python.optional_returns":
		return sentence(
			"Required values should not be modeled as optional",
			"returns unless explicitly exempted.",
		)
	case "python.catch_and_silence":
		return sentence(
			"Silent exception handling hides failures and violates",
			"fail-fast behavior.",
		)
	case "python.structured_logging":
		return sentence(
			"Logging should preserve structured context instead of",
			"formatting it away.",
		)
	case "python.direct_imports":
		return sentence(
			"Direct imports from protected packages bypass the",
			"intended public interface.",
		)
	case "python.bare_except":
		return "Bare except clauses hide exception types and are forbidden."
	case pythonPyprojectIgnoresPolicy:
		return "pyproject.toml contains forbidden linter ignore configuration."
	case pythonUVExcludeNewerPolicy:
		return "uv dependency resolution must exclude newly uploaded packages."
	default:
		return "Unexplained type ignore suppressions are forbidden."
	}
}

func pythonPolicySuggestion(policyID string) string {
	switch policyID {
	case pythonConditionalImportsPolicy:
		return "Move required imports to module scope or repair the module " +
			"boundary that made the import conditional."
	case "python.functional_idioms":
		return "Use functools.partial, operator helpers, itertools utilities, " +
			"or a named helper instead of ad-hoc closure factories."
	case "python.optional_returns":
		return "Use a required return type or configure a narrow exemption."
	case "python.catch_and_silence":
		return "Handle the exception explicitly or let it fail with useful context."
	case "python.structured_logging":
		return "Use structured logging fields according to the repo policy."
	case "python.direct_imports":
		return "Import through the package public API or configure an exempt path."
	case "python.bare_except":
		return "Catch a precise exception type and handle it explicitly."
	case pythonPyprojectIgnoresPolicy:
		return "Move file-specific ignores into the target files with documented " +
			"justification."
	case pythonUVExcludeNewerPolicy:
		return "Set [tool.uv].exclude-newer to the configured review window."
	default:
		return "Remove the suppression or document the narrow technical reason."
	}
}
