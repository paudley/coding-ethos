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
	validModes = map[string]bool{
		"off":      true,
		"record":   true,
		"advise":   true,
		"prepare":  true,
		"annotate": true,
		"ask":      true,
		"block":    true,
	}
	validEvaluatorKinds = map[string]bool{
		"argv":      true,
		"shell":     true,
		"path":      true,
		"ast":       true,
		"text":      true,
		"config":    true,
		"git_state": true,
		"external":  true,
	}
)

func (bundle Bundle) Validate() error {
	var errs []error

	if bundle.Version <= 0 {
		errs = append(errs, errors.New("version must be greater than zero"))
	}
	if bundle.BundleID == "" {
		errs = append(errs, errors.New("bundle_id is required"))
	}
	if bundle.Sources.Ethos.Primary == "" {
		errs = append(errs, errors.New("sources.ethos.primary is required"))
	}
	if bundle.Sources.Enforcement.Primary == "" {
		errs = append(errs, errors.New("sources.enforcement.primary is required"))
	}

	for id, principle := range bundle.Principles {
		if principle.ID != id {
			errs = append(errs, fmt.Errorf("principle %q has mismatched id %q", id, principle.ID))
		}
		if principle.Title == "" {
			errs = append(errs, fmt.Errorf("principle %q title is required", id))
		}
	}

	for id, policy := range bundle.Policies {
		errs = append(errs, validatePolicy(id, policy, bundle.Principles)...)
	}

	errs = append(errs, validateHookDispatch(bundle.Dispatch.Hooks, bundle.Policies)...)
	errs = append(errs, validatePolicyIDLists("dispatch.linter", bundle.Dispatch.Linter, bundle.Policies)...)
	errs = append(errs, validateGitDispatch(bundle.Dispatch.Git, bundle.Policies)...)

	return errors.Join(errs...)
}

func validatePolicy(id string, policy Policy, principles map[string]Principle) []error {
	var errs []error
	if policy.ID != id {
		errs = append(errs, fmt.Errorf("policy %q has mismatched id %q", id, policy.ID))
	}
	if policy.Category == "" {
		errs = append(errs, fmt.Errorf("policy %q category is required", id))
	}
	if policy.Source.File == "" {
		errs = append(errs, fmt.Errorf("policy %q source.file is required", id))
	}
	if !validModes[policy.DefaultSeverity] {
		errs = append(errs, fmt.Errorf("policy %q has invalid default_severity %q", id, policy.DefaultSeverity))
	}
	if len(policy.SupportedModes) == 0 {
		errs = append(errs, fmt.Errorf("policy %q must define supported_modes", id))
	}
	for _, mode := range policy.SupportedModes {
		if !validModes[mode] {
			errs = append(errs, fmt.Errorf("policy %q has invalid supported mode %q", id, mode))
		}
	}
	if !slices.Contains(policy.SupportedModes, policy.DefaultSeverity) {
		errs = append(errs, fmt.Errorf("policy %q default_severity %q is not in supported_modes", id, policy.DefaultSeverity))
	}
	if policy.Message == "" {
		errs = append(errs, fmt.Errorf("policy %q message is required", id))
	}
	if !policy.DefenseLayers.Persuade && !policy.DefenseLayers.Record &&
		policy.DefenseLayers.Intercept == "" &&
		policy.DefenseLayers.Mediate == "" &&
		policy.DefenseLayers.Detect == "" &&
		policy.DefenseLayers.Enforce == "" &&
		policy.DefenseLayers.Verify == "" &&
		policy.DefenseLayers.Notify == "" {
		errs = append(errs, fmt.Errorf("policy %q must define at least one defense layer", id))
	}
	if len(policy.Evaluators) == 0 {
		errs = append(errs, fmt.Errorf("policy %q must define at least one evaluator", id))
	}
	for _, evaluator := range policy.Evaluators {
		if evaluator.Name == "" {
			errs = append(errs, fmt.Errorf("policy %q has evaluator without name", id))
		}
		if !validEvaluatorKinds[evaluator.Kind] {
			errs = append(errs, fmt.Errorf("policy %q has invalid evaluator kind %q", id, evaluator.Kind))
		}
	}
	for _, principleID := range policy.PrincipleIDs {
		if _, ok := principles[principleID]; !ok {
			errs = append(errs, fmt.Errorf("policy %q references unknown principle %q", id, principleID))
		}
	}
	return errs
}

func validateHookDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) []error {
	var errs []error
	for event, tools := range hooks {
		if event == "" {
			errs = append(errs, errors.New("dispatch.hooks contains empty event name"))
		}
		for tool, entries := range tools {
			if tool == "" {
				errs = append(errs, fmt.Errorf("dispatch.hooks.%s contains empty tool matcher", event))
			}
			for idx, entry := range entries {
				context := fmt.Sprintf("dispatch.hooks.%s.%s[%d]", event, tool, idx)
				errs = append(errs, validateDispatchEntry(context, entry, policies)...)
			}
		}
	}
	return errs
}

func validateDispatchEntry(context string, entry HookDispatchEntry, policies map[string]Policy) []error {
	var errs []error
	policy, ok := policies[entry.PolicyID]
	if !ok {
		return []error{fmt.Errorf("%s references unknown policy %q", context, entry.PolicyID)}
	}
	if !validModes[entry.Mode] {
		errs = append(errs, fmt.Errorf("%s has invalid mode %q", context, entry.Mode))
	}
	if !slices.Contains(policy.SupportedModes, entry.Mode) {
		errs = append(errs, fmt.Errorf("%s mode %q is not supported by policy %q", context, entry.Mode, entry.PolicyID))
	}
	return errs
}

func validatePolicyIDLists(
	context string,
	lists map[string][]string,
	policies map[string]Policy,
) []error {
	var errs []error
	for name, policyIDs := range lists {
		for _, policyID := range policyIDs {
			if _, ok := policies[policyID]; !ok {
				errs = append(errs, fmt.Errorf("%s.%s references unknown policy %q", context, name, policyID))
			}
		}
	}
	return errs
}

func validateGitDispatch(git map[string]GitOperationDispatch, policies map[string]Policy) []error {
	var errs []error
	for operation, dispatch := range git {
		for _, policyID := range dispatch.Pre {
			if _, ok := policies[policyID]; !ok {
				errs = append(errs, fmt.Errorf("dispatch.git.%s.pre references unknown policy %q", operation, policyID))
			}
		}
		for _, policyID := range dispatch.Post {
			if _, ok := policies[policyID]; !ok {
				errs = append(errs, fmt.Errorf("dispatch.git.%s.post references unknown policy %q", operation, policyID))
			}
		}
	}
	return errs
}

func FormatValidationError(err error) string {
	if err == nil {
		return ""
	}
	parts := strings.Split(err.Error(), "\n")
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
