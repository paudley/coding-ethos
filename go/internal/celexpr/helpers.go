// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"reflect"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func helperFunctions() []cel.EnvOption {
	listOfStrings := cel.ListType(cel.StringType)

	options := make([]cel.EnvOption, 0, helperFunctionCapacity)
	options = append(options, basicStringHelpers()...)
	options = append(options, pathHelpers(listOfStrings)...)
	options = append(options, commandHelpers(listOfStrings)...)
	options = append(options, repoHelpers(listOfStrings)...)
	options = append(options, listHelpers()...)
	options = append(options, symbolHelpers()...)
	options = append(options, sourceHelpers()...)

	return options
}

func symbolHelpers() []cel.EnvOption {
	symbolType := cel.ObjectType(reflect.TypeFor[ProposedSymbolChangeInput]().String())

	return []cel.EnvOption{
		cel.Function(
			"kind_is",
			cel.MemberOverload(
				"symbol_kind_is_string",
				[]*cel.Type{symbolType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					var symbol *ProposedSymbolChangeInput

					switch v := lhs.Value().(type) {
					case *ProposedSymbolChangeInput:
						symbol = v
					case ProposedSymbolChangeInput:
						symbol = &v
					default:
						return types.Bool(false)
					}

					return types.Bool(symbol.SymbolKind == stringFromValue(rhs))
				}),
			),
		),
		cel.Function(
			"name_matches",
			cel.MemberOverload(
				"symbol_name_matches_string",
				[]*cel.Type{symbolType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					var symbol *ProposedSymbolChangeInput

					switch v := lhs.Value().(type) {
					case *ProposedSymbolChangeInput:
						symbol = v
					case ProposedSymbolChangeInput:
						symbol = &v
					default:
						return types.Bool(false)
					}

					matched, err := doublestar.Match(
						stringFromValue(rhs),
						symbol.SymbolName,
					)

					return types.Bool(err == nil && matched)
				}),
			),
		),
	}
}

func sourceHelpers() []cel.EnvOption {
	sourceType := cel.ObjectType(reflect.TypeFor[SourceInput]().String())

	return []cel.EnvOption{
		cel.Function(
			"has_nearby_test",
			cel.MemberOverload(
				"source_has_nearby_test",
				[]*cel.Type{sourceType},
				cel.BoolType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					var source *SourceInput

					switch v := val.Value().(type) {
					case *SourceInput:
						source = v
					case SourceInput:
						source = &v
					default:
						return types.Bool(false)
					}

					return types.Bool(source.HasNearbyTest)
				}),
			),
		),
		cel.Function(
			"has_doc_chunk",
			cel.MemberOverload(
				"source_has_doc_chunk",
				[]*cel.Type{sourceType},
				cel.BoolType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					var source *SourceInput

					switch v := val.Value().(type) {
					case *SourceInput:
						source = v
					case SourceInput:
						source = &v
					default:
						return types.Bool(false)
					}

					return types.Bool(source.HasDocChunk)
				}),
			),
		),
	}
}

func basicStringHelpers() []cel.EnvOption {
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
			func(pattern, value string) bool {
				matched, err := doublestar.Match(pattern, value)

				return err == nil && matched
			},
		),
		stringHelper(
			"lint_code_matches",
			"lint_code_matches_string_string",
			lintCodeMatches,
		),
		cel.Function(
			"is_linter",
			cel.Overload(
				"is_linter_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					return types.Bool(toolcatalog.IsLinter(stringFromValue(value)))
				}),
			),
		),
		cel.Function(
			"advertising_filter",
			cel.Overload(
				"advertising_filter_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					return types.Bool(advertisingFilter(stringFromValue(value)))
				}),
			),
		),
		stringHelper(
			"self_promotion_branding",
			"self_promotion_branding_string_string",
			selfPromotionBranding,
		),
	}
}

func pathHelpers(listOfStrings *cel.Type) []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(
			"is_test_path",
			cel.Overload(
				"is_test_path_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					return types.Bool(isTestPath(stringFromValue(value)))
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
					return types.Bool(isGeneratedPath(stringFromValue(value)))
				}),
			),
		),
		cel.Function(
			"is_protected_path",
			cel.Overload(
				"is_protected_path_string_list_string",
				[]*cel.Type{cel.StringType, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(file, paths ref.Val) ref.Val {
					return types.Bool(isProtectedPath(
						stringFromValue(file),
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
				cel.BinaryBinding(func(file, roots ref.Val) ref.Val {
					return types.Bool(inSourceRoot(
						stringFromValue(file),
						stringsFromValue(roots),
					))
				}),
			),
		),
	}
}

func commandHelpers(listOfStrings *cel.Type) []cel.EnvOption {
	return []cel.EnvOption{
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
				cel.BinaryBinding(func(argv, tool ref.Val) ref.Val {
					return types.Bool(argvInvokes(
						stringsFromValue(argv),
						stringFromValue(tool),
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
				cel.BinaryBinding(func(argv, tool ref.Val) ref.Val {
					return types.Bool(argvCommandIs(
						stringsFromValue(argv),
						stringFromValue(tool),
					))
				}),
			),
		),
		cel.Function(
			"sed_writes_files",
			cel.Overload(
				"sed_writes_files_list_string",
				[]*cel.Type{listOfStrings},
				cel.BoolType,
				cel.UnaryBinding(func(argv ref.Val) ref.Val {
					return types.Bool(sedWritesFiles(stringsFromValue(argv)))
				}),
			),
		),
		stringHelper(
			"has_inline_env",
			"has_inline_env_string_string",
			commandHasInlineEnv,
		),
	}
}

func repoHelpers(listOfStrings *cel.Type) []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(
			"repo_config_present",
			cel.Overload(
				"repo_config_present_list_list",
				[]*cel.Type{listOfStrings, listOfStrings},
				cel.BoolType,
				cel.BinaryBinding(func(files, candidates ref.Val) ref.Val {
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
				cel.BinaryBinding(func(branch, branches ref.Val) ref.Val {
					return types.Bool(isProtectedBranch(
						stringFromValue(branch),
						stringsFromValue(branches),
					))
				}),
			),
		),
	}
}

func listHelpers() []cel.EnvOption {
	return []cel.EnvOption{
		listStringHelper(
			"list_contains",
			"list_contains_list_string",
			func(value, needle string) bool { return value == needle },
		),
		listStringHelper(
			"any_glob_match",
			"any_glob_match_list_string",
			func(pattern, value string) bool {
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
		listStringHelper(
			"any_contains",
			"any_contains_list_string",
			func(value, needle string) bool {
				return strings.Contains(strings.ToLower(needle), strings.ToLower(value))
			},
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
			cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
				return types.Bool(matches(
					stringFromValue(lhs),
					stringFromValue(rhs),
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
			cel.BinaryBinding(func(values, needle ref.Val) ref.Val {
				for _, value := range stringsFromValue(values) {
					if matches(value, stringFromValue(needle)) {
						return types.True
					}
				}

				return types.False
			}),
		),
	)
}

func stringsFromValue(value ref.Val) []string {
	converted, err := value.ConvertToNative(reflect.TypeFor[[]string]())
	if err != nil {
		return nil
	}

	items, ok := converted.([]string)
	if !ok {
		return nil
	}

	return items
}

func stringFromValue(value ref.Val) string {
	converted, err := value.ConvertToNative(reflect.TypeFor[string]())
	if err != nil {
		return ""
	}

	text, ok := converted.(string)
	if !ok {
		return ""
	}

	return text
}

func lintCodeMatches(code, pattern string) bool {
	code = strings.TrimSpace(code)

	pattern = strings.TrimSpace(pattern)
	if code == "" || pattern == "" {
		return false
	}

	if code == pattern {
		return true
	}

	if before, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(code, before)
	}

	matched, err := doublestar.Match(pattern, code)

	return err == nil && matched
}

func selfPromotionBranding(text, provider string) bool {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return false
	}

	normalizedText := normalizedAdvertisingText(text)
	for _, pattern := range selfPromotionPatterns(normalizedProvider) {
		if strings.Contains(normalizedText, pattern) {
			return true
		}
	}

	return false
}

func advertisingFilter(text string) bool {
	return selfPromotionBranding(text, "codex") ||
		selfPromotionBranding(text, "claude") ||
		selfPromotionBranding(text, "gemini")
}

func normalizedAdvertisingText(text string) string {
	lower := strings.ToLower(text)
	for _, path := range []string{
		"claude.md",
		".codex/",
		".codex\\",
		".claude/",
		".claude\\",
		".gemini/",
		".gemini\\",
	} {
		lower = strings.ReplaceAll(lower, path, "")
	}

	return lower
}

func selfPromotionPatterns(provider string) []string {
	switch provider {
	case "codex":
		return []string{
			"[codex]",
			"codex/",
			"generated with codex",
			"generated by codex",
			"generated using codex",
			"co-authored-by: codex",
			"coauthored-by: codex",
		}
	case "claude":
		return []string{
			"[claude]",
			"claude/",
			"generated with claude",
			"generated by claude",
			"generated using claude",
			"co-authored-by: claude",
			"coauthored-by: claude",
		}
	case "gemini":
		return []string{
			"[gemini]",
			"gemini/",
			"generated with gemini",
			"generated by gemini",
			"generated using gemini",
			"co-authored-by: gemini",
			"coauthored-by: gemini",
		}
	default:
		return nil
	}
}

func commandInvokes(command, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}

	for field := range strings.FieldsSeq(command) {
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

func sedWritesFiles(argv []string) bool {
	stripped := stripLeadingAssignments(argv)
	if !argvCommandIs(stripped, "sed") {
		return false
	}

	return sedHasInPlaceOption(stripped[1:]) || sedHasWriteCommand(stripped[1:])
}

func sedHasInPlaceOption(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}

		if arg == "-i" || arg == "--in-place" ||
			strings.HasPrefix(arg, "-i") ||
			strings.HasPrefix(arg, "--in-place=") {
			return true
		}

		if strings.HasPrefix(arg, "--") {
			continue
		}

		if strings.HasPrefix(arg, "-") && strings.Contains(arg[1:], "i") {
			return true
		}

		if !strings.HasPrefix(arg, "-") {
			return false
		}
	}

	return false
}

func sedHasWriteCommand(args []string) bool {
	scripts := sedScriptArgs(args)

	return slices.ContainsFunc(scripts, sedScriptWritesFile)
}

func sedScriptArgs(args []string) []string {
	collector := sedScriptArgCollector{}

	for _, arg := range args {
		if collector.consume(arg) {
			break
		}
	}

	return collector.scripts
}

type sedScriptArgCollector struct {
	scripts           []string
	expectScript      bool
	skipNext          bool
	scriptFromOperand bool
}

func (collector *sedScriptArgCollector) consume(arg string) bool {
	if collector.skipNext {
		collector.skipNext = false

		return false
	}

	if collector.expectScript {
		collector.scripts = append(collector.scripts, arg)
		collector.expectScript = false

		return false
	}

	if arg == "--" {
		collector.scriptFromOperand = true

		return false
	}

	return collector.consumeOptionOrOperand(arg)
}

func (collector *sedScriptArgCollector) consumeOptionOrOperand(arg string) bool {
	if script, ok := strings.CutPrefix(arg, "--expression="); ok {
		collector.scripts = append(collector.scripts, script)

		return false
	}

	if arg == "-e" || arg == "--expression" {
		collector.expectScript = true

		return false
	}

	if arg == "-f" || arg == "--file" {
		collector.skipNext = true

		return false
	}

	if strings.HasPrefix(arg, "-e") && len(arg) > len("-e") {
		collector.scripts = append(collector.scripts, strings.TrimPrefix(arg, "-e"))

		return false
	}

	if strings.HasPrefix(arg, "-") && !collector.scriptFromOperand {
		return false
	}

	collector.scripts = append(collector.scripts, arg)

	return true
}

func sedScriptWritesFile(script string) bool {
	return slices.ContainsFunc(
		strings.FieldsFunc(script, sedScriptSeparator),
		sedCommandSegmentWritesFile,
	)
}

func sedScriptSeparator(char rune) bool {
	return char == ';' || char == '\n' || char == '{' || char == '}'
}

func sedCommandSegmentWritesFile(segment string) bool {
	fields := strings.Fields(strings.TrimSpace(segment))
	for index, field := range fields {
		if field == "w" || strings.HasSuffix(field, "w") {
			return index+1 < len(fields)
		}
	}

	return false
}

func stripLeadingAssignments(argv []string) []string {
	for len(argv) > 0 && isShellAssignment(argv[0]) {
		argv = argv[1:]
	}

	return argv
}

func isShellAssignment(arg string) bool {
	name, _, ok := strings.Cut(arg, "=")
	if !ok || name == "" {
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

func commandTokenMatchesTool(token, tool string) bool {
	token = strings.Trim(token, `"'`)
	if token == "" {
		return false
	}

	return token == tool || strings.HasSuffix(token, "/"+tool)
}
