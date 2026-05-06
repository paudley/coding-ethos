// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func defaultGeneratedConfigCheckCommand(configSourceRoot string) []string {
	return []string{
		"uv",
		"run",
		"--project",
		configSourceRoot,
		"python",
		filepath.Join(configSourceRoot, "main.py"),
		"--repo",
		".",
		"--check-tool-configs",
	}
}

func addPolicyIfEnabled(
	policies map[string]Policy,
	config map[string]any,
	_ map[string]Principle,
	id string,
	path []string,
	policy Policy,
) {
	if boolAt(config, append(path, "enabled")...) {
		policies[id] = policy
	}
}

func compileDispatch(policies map[string]Policy) Dispatch {
	hooks := compileHookDispatch(policies)

	return Dispatch{
		Hooks:  hooks,
		Linter: compileLinterDispatch(policies),
		Git:    compileGitDispatch(policies),
	}
}

func compileHookDispatch(
	policies map[string]Policy,
) map[string]map[string][]HookDispatchEntry {
	hooks := map[string]map[string][]HookDispatchEntry{}
	addGitHookBypassDispatch(hooks, policies)
	addBlockingBashDispatch(hooks, policies)
	addProtectedPathDispatch(hooks, policies)
	addProtectedBranchWriteDispatch(hooks, policies)
	addPythonWriteDispatch(hooks, policies)
	addCommitHeadDispatch(hooks, policies)
	addExpressionPoliciesToHookDispatch(hooks, policies)

	return hooks
}

func addGitHookBypassDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["git.hook_bypass"]; ok {
		ensureHookTool(hooks, "PreToolUse", "Bash")
		hooks["PreToolUse"]["Bash"] = append(
			hooks["PreToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID: "git.hook_bypass",
				Mode:     "block",
			},
		)
	}
}

func addBlockingBashDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	for _, policyID := range []string{
		"git.destructive_command",
		"git.merge_strategy_shortcut",
		"git.force_push_protected_branch",
		"git.checkout_protected_branch",
		"git.destructive_worktree",
		"git.protected_submodule_update",
		"git.change_dir_flag",
		"git.stash_blocked",
		"git.commitlint",
		"git.commit_attribution",
		"shell.malformed_command",
		"shell.dangerous_command",
		"shell.background_git",
		"shell.github_admin",
		"shell.forbidden_strings",
	} {
		if _, ok := policies[policyID]; ok {
			ensureHookTool(hooks, "PreToolUse", "Bash")
			hooks["PreToolUse"]["Bash"] = append(
				hooks["PreToolUse"]["Bash"],
				HookDispatchEntry{
					PolicyID: policyID,
					Mode:     "block",
				},
			)
		}
	}
}

func addProtectedBranchWriteDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["filesystem.protected_branch_write"]; !ok {
		return
	}

	for _, tool := range []string{"Bash", "Write", "Edit", "MultiEdit"} {
		ensureHookTool(hooks, "PreToolUse", tool)
		hooks["PreToolUse"][tool] = append(
			hooks["PreToolUse"][tool],
			HookDispatchEntry{
				PolicyID: "filesystem.protected_branch_write",
				Mode:     "block",
			},
		)
	}
}

func addProtectedPathDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["filesystem.protected_path"]; !ok {
		return
	}

	for _, tool := range []string{"Bash", "Write", "Edit", "MultiEdit"} {
		ensureHookTool(hooks, "PreToolUse", tool)
		hooks["PreToolUse"][tool] = append(
			hooks["PreToolUse"][tool],
			HookDispatchEntry{
				PolicyID: "filesystem.protected_path",
				Mode:     "block",
			},
		)
	}
}

func addPythonWriteDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	for _, policyID := range []string{
		"python.conditional_imports",
		"python.functional_idioms",
		"python.optional_returns",
		"python.catch_and_silence",
		"python.structured_logging",
		"python.direct_imports",
		"python.bare_except",
		"python.unexplained_type_ignore",
	} {
		if _, exists := policies[policyID]; exists {
			for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
				ensureHookTool(hooks, "PreToolUse", tool)
				hooks["PreToolUse"][tool] = append(
					hooks["PreToolUse"][tool],
					HookDispatchEntry{
						PolicyID:     policyID,
						Mode:         pythonWriteDispatchMode(policyID),
						PathPatterns: []string{"**/*.py"},
					},
				)
			}
		}
	}
}

func pythonWriteDispatchMode(policyID string) string {
	if policyID == "python.conditional_imports" {
		return "block"
	}

	return "advise"
}

func addCommitHeadDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["git.commit_head_advanced"]; ok {
		ensureHookTool(hooks, "PreToolUse", "Bash")
		hooks["PreToolUse"]["Bash"] = append(
			hooks["PreToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "git.commit_head_advanced",
				Mode:            "record",
				CommandPatterns: []string{"git commit"},
			},
		)
		ensureHookTool(hooks, "PostToolUse", "Bash")
		hooks["PostToolUse"]["Bash"] = append(
			hooks["PostToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "git.commit_head_advanced",
				Mode:            "block",
				CommandPatterns: []string{"git commit"},
			},
		)
	}
}

func addExpressionPoliciesToHookDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	for policyID, policyDef := range policies {
		for _, evaluator := range policyDef.Evaluators {
			if evaluator.Kind != "cel" || evaluator.Name != "cel.expression" {
				continue
			}

			for _, event := range stringSliceValueAllowEmpty(
				evaluator.Options["hook_events"],
				[]string{"PreToolUse"},
			) {
				for _, tool := range stringSliceValue(
					evaluator.Options["tools"],
					expressionHookTools(
						stringOptionFromMap(evaluator.Options, "scope", "command"),
					),
				) {
					ensureHookTool(hooks, event, tool)

					if !hookDispatchContains(hooks[event][tool], policyID) {
						hooks[event][tool] = append(
							hooks[event][tool],
							HookDispatchEntry{
								PolicyID:        policyID,
								Mode:            expressionDispatchMode(policyDef, evaluator),
								CommandPatterns: stringSliceValue(evaluator.Options["command_patterns"], nil),
								PathPatterns:    stringSliceValue(evaluator.Options["path_patterns"], nil),
							},
						)
					}
				}
			}
		}
	}
}

func expressionDispatchMode(policyDef Policy, evaluator Evaluator) string {
	mode := stringOptionFromMap(evaluator.Options, "mode", "")
	if mode != "" {
		return mode
	}

	return policyDef.DefaultSeverity
}

func expressionHookTools(scope string) []string {
	switch scope {
	case "path", "file", "files":
		return []string{"Bash", "Write", "Edit", "MultiEdit"}
	case "diagnostic", "finding", "lint":
		return []string{"Bash"}
	default:
		return []string{"Bash"}
	}
}

func hookDispatchContains(entries []HookDispatchEntry, policyID string) bool {
	for _, entry := range entries {
		if entry.PolicyID == policyID {
			return true
		}
	}

	return false
}

func compileLinterDispatch(policies map[string]Policy) map[string][]string {
	linter := map[string][]string{
		"files": existingPolicyIDs(
			policies,
			"syntax.file_syntax",
			"syntax.merge_conflict",
			"security.private_key",
			"filesystem.shebangs",
			"filesystem.large_files",
			"filesystem.line_limits",
			"repo.pii_scrubber",
			"repo.license_header",
			"shell.malformed_command",
			"shell.best_practices",
			"shell.forbidden_strings",
			"python.conditional_imports",
			"python.functional_idioms",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
			"python.pyproject_ignores",
			"python.uv_exclude_newer",
		),
		"staged": existingPolicyIDs(
			policies,
			"git.hook_bypass",
			"git.destructive_command",
			"git.merge_strategy_shortcut",
			"git.force_push_protected_branch",
			"git.checkout_protected_branch",
			"git.destructive_worktree",
			"git.protected_submodule_update",
			"git.change_dir_flag",
			"git.stash_blocked",
			"shell.malformed_command",
			"shell.dangerous_command",
			"shell.background_git",
			"shell.github_admin",
			"shell.forbidden_strings",
			"shell.best_practices",
			"git.commit_attribution",
			"git.staged_admin_files",
			"filesystem.protected_path",
			"filesystem.protected_branch_write",
			"repo.required_ignores",
			"syntax.file_syntax",
			"syntax.merge_conflict",
			"security.private_key",
			"filesystem.shebangs",
			"filesystem.large_files",
			"filesystem.line_limits",
			"repo.pii_scrubber",
			"repo.license_header",
			"python.conditional_imports",
			"python.functional_idioms",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
			"python.bare_except",
			"python.unexplained_type_ignore",
			"python.pyproject_ignores",
			"python.uv_exclude_newer",
		),
		"smoke": existingPolicyIDs(
			policies,
			"repo.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"full": existingPolicyIDs(
			policies,
			"repo.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"cutover": existingPolicyIDs(
			policies,
			"repo.required_ignores",
		),
		"commit-msg": existingPolicyIDs(
			policies,
			"git.commitlint",
			"git.commit_attribution",
		),
	}
	addExpressionPoliciesToLinterDispatch(linter, policies)

	return linter
}

func addExpressionPoliciesToLinterDispatch(
	linter map[string][]string,
	policies map[string]Policy,
) {
	for policyID, policyDef := range policies {
		for _, evaluator := range policyDef.Evaluators {
			if evaluator.Name != "cel.expression" {
				continue
			}

			for _, scope := range stringSliceValue(
				evaluator.Options["dispatch_scopes"],
				[]string{"files", "staged"},
			) {
				if !slices.Contains(linter[scope], policyID) {
					linter[scope] = append(linter[scope], policyID)
				}
			}
		}
	}
}

func compileGitDispatch(policies map[string]Policy) map[string]GitOperationDispatch {
	return map[string]GitOperationDispatch{
		"*": {
			Pre: existingPolicyIDs(policies, "git.change_dir_flag"),
		},
		"commit": {
			Pre: existingPolicyIDs(
				policies,
				"git.hook_bypass",
				"git.commitlint",
				"git.commit_attribution",
				"git.staged_admin_files",
			),
			Post: existingPolicyIDs(policies, "git.commit_head_advanced"),
		},
		"push": {
			Pre: existingPolicyIDs(
				policies,
				"git.hook_bypass",
				"git.force_push_protected_branch",
			),
		},
		"reset":    {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"clean":    {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"restore":  {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"switch":   {Pre: existingPolicyIDs(policies, "git.checkout_protected_branch")},
		"merge":    {Pre: existingPolicyIDs(policies, "git.merge_strategy_shortcut")},
		"worktree": {Pre: existingPolicyIDs(policies, "git.destructive_worktree")},
		"submodule": {
			Pre: existingPolicyIDs(policies, "git.protected_submodule_update"),
		},
		"stash": {Pre: existingPolicyIDs(policies, "git.stash_blocked")},
		"checkout": {
			Pre: existingPolicyIDs(
				policies,
				"git.destructive_command",
				"git.checkout_protected_branch",
			),
		},
	}
}

func ensureHookTool(
	hooks map[string]map[string][]HookDispatchEntry,
	event string,
	tool string,
) {
	if _, ok := hooks[event]; !ok {
		hooks[event] = map[string][]HookDispatchEntry{}
	}

	if _, ok := hooks[event][tool]; !ok {
		hooks[event][tool] = []HookDispatchEntry{}
	}
}

func existingPolicyIDs(policies map[string]Policy, ids ...string) []string {
	existing := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := policies[id]; ok {
			existing = append(existing, id)
		}
	}

	return existing
}

func principleRefs(principles map[string]Principle, ids ...string) []string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := principles[id]; ok {
			refs = append(refs, id)
		}
	}

	return refs
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	for key, overlayValue := range overlay {
		baseMap, baseOK := base[key].(map[string]any)

		overlayMap, overlayOK := overlayValue.(map[string]any)
		if baseOK && overlayOK {
			base[key] = mergeMaps(baseMap, overlayMap)

			continue
		}

		base[key] = overlayValue
	}

	return base
}

func boolAt(values map[string]any, path ...string) bool {
	value, exists := valueAt(values, path...)
	if !exists {
		return false
	}

	return boolValue(value)
}

func boolValue(value any) bool {
	boolValue, isBool := value.(bool)

	return isBool && boolValue
}

func boolOptionFromMap(
	values map[string]any,
	key string,
	defaultValue bool,
) (bool, error) {
	value, exists := values[key]
	if !exists {
		return defaultValue, nil
	}

	boolValue, isBool := value.(bool)
	if !isBool {
		return false, fmt.Errorf("%s must be a boolean", key)
	}

	return boolValue, nil
}

func enabledAt(values map[string]any, path []string) bool {
	value, exists := valueAt(values, append(path, "enabled")...)
	if !exists {
		return true
	}

	boolValue, isBool := value.(bool)

	return !isBool || boolValue
}

func stringSliceAt(
	values map[string]any,
	path []string,
	defaults []string,
) []string {
	value, exists := valueAt(values, path...)
	if !exists {
		return append([]string(nil), defaults...)
	}

	items := stringSlice(value)
	if len(items) == 0 {
		return append([]string(nil), defaults...)
	}

	return items
}

func intAt(values map[string]any, path []string, defaultValue int) int {
	value, exists := valueAt(values, path...)
	if !exists {
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

func stringAt(values map[string]any, path ...string) string {
	value, exists := valueAt(values, path...)
	if !exists {
		return ""
	}

	stringValue, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(stringValue)
}

func stringOptionFromMap(values map[string]any, key, defaultValue string) string {
	value, exists := values[key]
	if !exists {
		return defaultValue
	}

	text := strings.TrimSpace(stringValue(value))
	if text == "" || text == "<nil>" {
		return defaultValue
	}

	return text
}

func stringSliceValue(value any, defaults []string) []string {
	items := stringSlice(value)
	if len(items) == 0 {
		return append([]string(nil), defaults...)
	}

	return items
}

func stringSliceValueAllowEmpty(value any, defaults []string) []string {
	if value == nil {
		return append([]string(nil), defaults...)
	}

	return stringSlice(value)
}

func policyConfigEnabled(values map[string]any, policyID string) bool {
	path := append(strings.Split(policyID, "."), "enabled")

	value, exists := valueAt(values, path...)
	if !exists {
		return true
	}

	boolValue, isBool := value.(bool)

	return !isBool || boolValue
}

func valueAt(values map[string]any, path ...string) (any, bool) {
	current := any(values)
	for _, part := range path {
		currentMap, isMap := current.(map[string]any)
		if !isMap {
			return nil, false
		}

		var exists bool

		current, exists = currentMap[part]
		if !exists {
			return nil, false
		}
	}

	return current, true
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}

	return fmt.Sprint(value)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}

	return 0
}

func stringSlice(value any) []string {
	if items, ok := value.([]string); ok {
		return append([]string(nil), items...)
	}

	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}

	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		items = append(items, stringValue(raw))
	}

	return items
}

func stringMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	items := map[string]string{}
	for key, value := range raw {
		items[key] = stringValue(value)
	}

	return items
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}

	_, err := os.Stat(path)

	return err == nil
}

func defaultBundleID(primary, config string, hashes map[string]string) string {
	parts := make([]string, 0, defaultBundleBaseParts+len(hashes))
	parts = append(parts, primary, config)

	for path, hash := range hashes {
		parts = append(parts, path+"="+hash)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))

	return "policy-" + hex.EncodeToString(sum[:8])
}
