// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"path"
	"reflect"
	"strings"

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
				matched, err := path.Match(pattern, value)

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
		listStringHelper(
			"list_contains",
			"list_contains_list_string",
			func(value string, needle string) bool { return value == needle },
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
