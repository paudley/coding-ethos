// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"reflect"
	"strings"

	"github.com/bmatcuk/doublestar"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func helperFunctions() []cel.EnvOption {
	listOfStrings := cel.ListType(cel.StringType)

	return []cel.EnvOption{
		stringHelper(
			"has_prefix",
			"has_prefix_string_string",
			strings.HasPrefix,
		),
		stringHelper(
			"has_suffix",
			"has_suffix_string_string",
			strings.HasSuffix,
		),
		stringHelper(
			"glob_match",
			"glob_match_string_string",
			func(pattern string, value string) bool {
				matched, err := doublestar.Match(pattern, value)

				return err == nil && matched
			},
		),
		cel.Function(
			"is_test_path",
			cel.Overload(
				"is_test_path_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					return types.Bool(isTestPath(string(value.(types.String))))
				}),
			),
		),
		cel.Function(
			"is_generated_path",
			cel.Overload(
				"is_generated_path_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					return types.Bool(isGeneratedPath(string(value.(types.String))))
				}),
			),
		),
		cel.Function(
			"is_protected_path",
			cel.Overload(
				"is_protected_path_string_list_string",
				[]*cel.Type{cel.StringType, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(file ref.Val, paths ref.Val) ref.Val {
					return types.Bool(isProtectedPath(
						string(file.(types.String)),
						stringsFromValue(paths),
					))
				}),
			),
		),
		cel.Function(
			"in_source_root",
			cel.Overload(
				"in_source_root_string_list_string",
				[]*cel.Type{cel.StringType, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(file ref.Val, roots ref.Val) ref.Val {
					return types.Bool(inSourceRoot(
						string(file.(types.String)),
						stringsFromValue(roots),
					))
				}),
			),
		),
		stringHelper(
			"lint_code_matches",
			"lint_code_matches_string_string",
			lintCodeMatches,
		),
		stringHelper(
			"command_invokes",
			"command_invokes_string_string",
			commandInvokes,
		),
		cel.Function(
			"argv_invokes",
			cel.Overload(
				"argv_invokes_list_string",
				[]*cel.Type{listOfStrings, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(func(argv ref.Val, tool ref.Val) ref.Val {
					return types.Bool(argvInvokes(
						stringsFromValue(argv),
						string(tool.(types.String)),
					))
				}),
			),
		),
		cel.Function(
			"argv_command_is",
			cel.Overload(
				"argv_command_is_list_string",
				[]*cel.Type{listOfStrings, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(func(argv ref.Val, tool ref.Val) ref.Val {
					return types.Bool(argvCommandIs(
						stringsFromValue(argv),
						string(tool.(types.String)),
					))
				}),
			),
		),
		stringHelper(
			"has_inline_env",
			"has_inline_env_string_string",
			commandHasInlineEnv,
		),
		cel.Function(
			"repo_config_present",
			cel.Overload(
				"repo_config_present_list_list",
				[]*cel.Type{listOfStrings, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(files ref.Val, candidates ref.Val) ref.Val {
					return types.Bool(len(presentRepoConfigs(
						stringsFromValue(files),
						stringsFromValue(candidates),
					)) > 0)
				}),
			),
		),
		cel.Function(
			"is_protected_branch",
			cel.Overload(
				"is_protected_branch_string_list_string",
				[]*cel.Type{cel.StringType, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(branch ref.Val, branches ref.Val) ref.Val {
					return types.Bool(isProtectedBranch(
						string(branch.(types.String)),
						stringsFromValue(branches),
					))
				}),
			),
		),
		listStringHelper(
			"list_contains",
			"list_contains_list_string",
			func(value string, needle string) bool { return value == needle },
		),
		listStringHelper(
			"any_glob_match",
			"any_glob_match_list_string",
			func(pattern string, value string) bool {
				matched, err := doublestar.Match(pattern, value)

				return err == nil && matched
			},
		),
		listStringHelper(
			"any_has_prefix",
			"any_has_prefix_list_string",
			strings.HasPrefix,
		),
		listStringHelper(
			"any_has_suffix",
			"any_has_suffix_list_string",
			strings.HasSuffix,
		),
	}
}

func stringHelper(
	name string,
	overload string,
	matches func(string, string) bool,
) cel.EnvOption {
	return cel.Function(
		name,
		cel.Overload(
			overload,
			[]*cel.Type{cel.StringType, cel.StringType},
			cel.BoolType,
			cel.BinaryBinding(func(lhs ref.Val, rhs ref.Val) ref.Val {
				return types.Bool(matches(
					string(lhs.(types.String)),
					string(rhs.(types.String)),
				))
			}),
		),
	)
}

func listStringHelper(
	name string,
	overload string,
	matches func(string, string) bool,
) cel.EnvOption {
	return cel.Function(
		name,
		cel.Overload(
			overload,
			[]*cel.Type{cel.ListType(cel.StringType), cel.StringType},
			cel.BoolType,
			cel.BinaryBinding(func(values ref.Val, needle ref.Val) ref.Val {
				for _, value := range stringsFromValue(values) {
					if matches(value, string(needle.(types.String))) {
						return types.True
					}
				}

				return types.False
			}),
		),
	)
}

func stringsFromValue(value ref.Val) []string {
	converted, err := value.ConvertToNative(reflect.TypeOf([]string{}))
	if err != nil {
		return nil
	}

	items, ok := converted.([]string)
	if !ok {
		return nil
	}

	return items
}

func lintCodeMatches(code string, pattern string) bool {
	code = strings.TrimSpace(code)
	pattern = strings.TrimSpace(pattern)
	if code == "" || pattern == "" {
		return false
	}
	if code == pattern {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(code, strings.TrimSuffix(pattern, "*"))
	}

	matched, err := doublestar.Match(pattern, code)

	return err == nil && matched
}

func commandInvokes(command string, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	for _, field := range strings.Fields(command) {
		if commandTokenMatchesTool(field, tool) {
			return true
		}
	}

	return false
}

func argvInvokes(argv []string, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	for _, arg := range argv {
		if commandTokenMatchesTool(arg, tool) {
			return true
		}
	}

	return false
}

func argvCommandIs(argv []string, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	stripped := stripLeadingAssignments(argv)

	return len(stripped) > 0 && commandTokenMatchesTool(stripped[0], tool)
}

func stripLeadingAssignments(argv []string) []string {
	for len(argv) > 0 && isShellAssignment(argv[0]) {
		argv = argv[1:]
	}

	return argv
}

func isShellAssignment(arg string) bool {
	name, value, ok := strings.Cut(arg, "=")
	if !ok || name == "" || value == "" {
		return false
	}
	for _, char := range name {
		if char != '_' && (char < 'A' || char > 'Z') &&
			(char < 'a' || char > 'z') &&
			(char < '0' || char > '9') {
			return false
		}
	}

	return true
}

func commandTokenMatchesTool(token string, tool string) bool {
	token = strings.Trim(token, `"'`)
	if token == "" {
		return false
	}

	return token == tool || strings.HasSuffix(token, "/"+tool)
}
