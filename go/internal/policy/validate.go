// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

var (
	errInvalidBundleVersion     = errors.New("version must be greater than zero")
	errBundleIDRequired         = errors.New("bundle_id is required")
	errEthosPrimaryRequired     = errors.New("sources.ethos.primary is required")
	errEnforcementPrimaryNeeded = errors.New("sources.enforcement.primary is required")
	errValidationFailed         = errors.New("policy bundle validation failed")
	errEmptyHookEvent           = errors.New("dispatch.hooks contains empty event name")
)

const (
	validateBaseCapacity         = 6
	bundleFieldCapacity          = 4
	principleValidationFactor    = 2
	policyValidationCapacity     = 5
	policyIdentityCapacity       = 3
	modeValidationBaseCapacity   = 3
	policyBodyValidationCapacity = 2
	evaluatorValidationFactor    = 2
	dispatchEntryCapacity        = 2
	gitDispatchValidationFactor  = 2
)

func (bundle Bundle) Validate() error {
	errs := make([]error, 0, validateBaseCapacity+len(bundle.Policies))

	errs = append(errs, validateBundleFields(bundle)...)
	errs = append(errs, validatePrinciples(bundle.Principles)...)
	errs = append(errs, validateSkills(bundle.Skills, bundle.Principles)...)

	for policyID, policy := range bundle.Policies {
		errs = append(errs, validatePolicy(policyID, policy, bundle.Principles)...)
	}

	errs = append(errs, validateHookDispatch(bundle.Dispatch.Hooks, bundle.Policies)...)
	errs = append(
		errs,
		validatePolicyIDLists(
			"dispatch.linter",
			bundle.Dispatch.Linter,
			bundle.Policies,
		)...)
	errs = append(errs, validateGitDispatch(bundle.Dispatch.Git, bundle.Policies)...)

	return errors.Join(errs...)
}

func validateBundleFields(bundle Bundle) []error {
	errs := make([]error, 0, bundleFieldCapacity)

	if bundle.Version <= 0 {
		errs = append(errs, errInvalidBundleVersion)
	}

	if bundle.BundleID == "" {
		errs = append(errs, errBundleIDRequired)
	}

	if bundle.Sources.Ethos.Primary == "" {
		errs = append(errs, errEthosPrimaryRequired)
	}

	if bundle.Sources.Enforcement.Primary == "" {
		errs = append(errs, errEnforcementPrimaryNeeded)
	}

	return errs
}

func validatePrinciples(principles map[string]Principle) []error {
	errs := make([]error, 0, len(principles)*principleValidationFactor)

	for principleID, principle := range principles {
		if principle.ID != principleID {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: principle %q has mismatched id %q",
					errValidationFailed,
					principleID,
					principle.ID,
				),
			)
		}

		if principle.Title == "" {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: principle %q title is required",
					errValidationFailed,
					principleID,
				),
			)
		}
	}

	return errs
}

func validateSkills(
	skills map[string]Skill,
	principles map[string]Principle,
) []error {
	errs := []error{}

	for skillID, skill := range skills {
		if skill.ID != skillID {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: skill %q has mismatched id %q",
					errValidationFailed,
					skillID,
					skill.ID,
				),
			)
		}
		if skill.Title == "" || skill.Description == "" {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: skill %q title and description are required",
					errValidationFailed,
					skillID,
				),
			)
		}
		for _, principleID := range skill.PrincipleIDs {
			if _, ok := principles[principleID]; !ok {
				errs = append(
					errs,
					fmt.Errorf(
						"%w: skill %q references unknown principle %q",
						errValidationFailed,
						skillID,
						principleID,
					),
				)
			}
		}
	}

	return errs
}

func validatePolicy(
	policyID string,
	policy Policy,
	principles map[string]Principle,
) []error {
	errs := make([]error, 0, policyValidationCapacity)

	errs = append(errs, validatePolicyIdentity(policyID, policy)...)
	errs = append(errs, validatePolicyModes(policyID, policy)...)
	errs = append(errs, validatePolicyBody(policyID, policy)...)
	errs = append(errs, validatePolicyEvaluators(policyID, policy)...)
	errs = append(errs, validatePolicyPrinciples(policyID, policy, principles)...)

	return errs
}

func validatePolicyIdentity(policyID string, policy Policy) []error {
	errs := make([]error, 0, policyIdentityCapacity)

	if policy.ID != policyID {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q has mismatched id %q",
				errValidationFailed,
				policyID,
				policy.ID,
			),
		)
	}

	if policy.Category == "" {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q category is required",
				errValidationFailed,
				policyID,
			),
		)
	}

	if policy.Source.File == "" {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q source.file is required",
				errValidationFailed,
				policyID,
			),
		)
	}

	return errs
}

func validatePolicyModes(policyID string, policy Policy) []error {
	errs := make(
		[]error,
		0,
		modeValidationBaseCapacity+len(policy.SupportedModes),
	)

	if !validMode(policy.DefaultSeverity) {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q has invalid default_severity %q",
				errValidationFailed,
				policyID,
				policy.DefaultSeverity,
			),
		)
	}

	if len(policy.SupportedModes) == 0 {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q must define supported_modes",
				errValidationFailed,
				policyID,
			),
		)
	}

	for _, mode := range policy.SupportedModes {
		if !validMode(mode) {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: policy %q has invalid supported mode %q",
					errValidationFailed,
					policyID,
					mode,
				),
			)
		}
	}

	if !slices.Contains(policy.SupportedModes, policy.DefaultSeverity) {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q default_severity %q is not in supported_modes",
				errValidationFailed,
				policyID,
				policy.DefaultSeverity,
			),
		)
	}

	return errs
}

func validatePolicyBody(policyID string, policy Policy) []error {
	errs := make([]error, 0, policyBodyValidationCapacity)

	if policy.Message == "" {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q message is required",
				errValidationFailed,
				policyID,
			),
		)
	}

	if !hasDefenseLayer(policy.DefenseLayers) {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q must define at least one defense layer",
				errValidationFailed,
				policyID,
			),
		)
	}

	return errs
}

func validatePolicyEvaluators(policyID string, policy Policy) []error {
	errs := make([]error, 0, 1+len(policy.Evaluators)*evaluatorValidationFactor)

	if len(policy.Evaluators) == 0 {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: policy %q must define at least one evaluator",
				errValidationFailed,
				policyID,
			),
		)
	}

	for _, evaluator := range policy.Evaluators {
		if evaluator.Name == "" {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: policy %q has evaluator without name",
					errValidationFailed,
					policyID,
				),
			)
		}

		if !validEvaluatorKind(evaluator.Kind) {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: policy %q has invalid evaluator kind %q",
					errValidationFailed,
					policyID,
					evaluator.Kind,
				),
			)
		}
	}

	return errs
}

func validatePolicyPrinciples(
	policyID string,
	policy Policy,
	principles map[string]Principle,
) []error {
	errs := make([]error, 0, len(policy.PrincipleIDs))

	for _, principleID := range policy.PrincipleIDs {
		if _, ok := principles[principleID]; !ok {
			errs = append(
				errs,
				fmt.Errorf(
					"%w: policy %q references unknown principle %q",
					errValidationFailed,
					policyID,
					principleID,
				),
			)
		}
	}

	return errs
}

func hasDefenseLayer(layers DefenseLayers) bool {
	return layers.Persuade ||
		layers.Record ||
		layers.Intercept != "" ||
		layers.Mediate != "" ||
		layers.Detect != "" ||
		layers.Enforce != "" ||
		layers.Verify != "" ||
		layers.Notify != ""
}

func validateHookDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) []error {
	errs := make([]error, 0, len(hooks))

	for event, tools := range hooks {
		if event == "" {
			errs = append(errs, errEmptyHookEvent)
		}

		for tool, entries := range tools {
			if tool == "" {
				errs = append(
					errs,
					fmt.Errorf(
						"%w: dispatch.hooks.%s contains empty tool matcher",
						errValidationFailed,
						event,
					),
				)
			}

			for idx, entry := range entries {
				context := fmt.Sprintf("dispatch.hooks.%s.%s[%d]", event, tool, idx)
				errs = append(errs, validateDispatchEntry(context, entry, policies)...)
			}
		}
	}

	return errs
}

func validateDispatchEntry(
	context string,
	entry HookDispatchEntry,
	policies map[string]Policy,
) []error {
	errs := make([]error, 0, dispatchEntryCapacity)

	policy, ok := policies[entry.PolicyID]
	if !ok {
		return []error{
			fmt.Errorf(
				"%w: %s references unknown policy %q",
				errValidationFailed,
				context,
				entry.PolicyID,
			),
		}
	}

	if !validMode(entry.Mode) {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: %s has invalid mode %q",
				errValidationFailed,
				context,
				entry.Mode,
			),
		)
	}

	if !slices.Contains(policy.SupportedModes, entry.Mode) {
		errs = append(
			errs,
			fmt.Errorf(
				"%w: %s mode %q is not supported by policy %q",
				errValidationFailed,
				context,
				entry.Mode,
				entry.PolicyID,
			),
		)
	}

	return errs
}

func validatePolicyIDLists(
	context string,
	lists map[string][]string,
	policies map[string]Policy,
) []error {
	errs := make([]error, 0, len(lists))

	for name, policyIDs := range lists {
		for _, policyID := range policyIDs {
			if _, ok := policies[policyID]; !ok {
				errs = append(
					errs,
					fmt.Errorf(
						"%w: %s.%s references unknown policy %q",
						errValidationFailed,
						context,
						name,
						policyID,
					),
				)
			}
		}
	}

	return errs
}

func validateGitDispatch(
	git map[string]GitOperationDispatch,
	policies map[string]Policy,
) []error {
	errs := make([]error, 0, len(git)*gitDispatchValidationFactor)

	for operation, dispatch := range git {
		for _, policyID := range dispatch.Pre {
			if _, ok := policies[policyID]; !ok {
				errs = append(
					errs,
					fmt.Errorf(
						"%w: dispatch.git.%s.pre references unknown policy %q",
						errValidationFailed,
						operation,
						policyID,
					),
				)
			}
		}

		for _, policyID := range dispatch.Post {
			if _, ok := policies[policyID]; !ok {
				errs = append(
					errs,
					fmt.Errorf(
						"%w: dispatch.git.%s.post references unknown policy %q",
						errValidationFailed,
						operation,
						policyID,
					),
				)
			}
		}
	}

	return errs
}

func validMode(mode string) bool {
	switch mode {
	case "off", "record", "advise", "prepare", "annotate", "ask", "block":
		return true
	default:
		return false
	}
}

func validEvaluatorKind(kind string) bool {
	switch kind {
	case "argv", "shell", "path", "ast", "text", "toml", "config", "git_state", "external", "cel":
		return true
	default:
		return false
	}
}

func FormatValidationError(err error) string {
	if err == nil {
		return ""
	}

	parts := strings.Split(err.Error(), "\n")
	sort.Strings(parts)

	return strings.Join(parts, "\n")
}
