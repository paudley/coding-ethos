// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package geminiprompts

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

func enforcementNotes(config map[string]any) []string {
	python := configdata.MapValue(config["python"])
	style := configdata.MapValue(config["style"])
	gemini := configdata.MapValue(config["gemini"])

	notes := []string{}
	if lineLength := strings.TrimSpace(
		fmt.Sprint(style["line_length"]),
	); lineLength != "" &&
		lineLength != "<nil>" {
		notes = append(notes, "Shared line length is "+lineLength+" characters.")
	}

	if version := configdata.StringAt(style, "python_version"); version != "" {
		notes = append(notes, "Target Python version is "+version+".")
	}

	if values := configdata.StringList(python["source_paths"]); len(values) > 0 {
		notes = append(notes, "Primary source paths: "+strings.Join(values, ", ")+".")
	}

	if values := configdata.StringList(python["test_paths"]); len(values) > 0 {
		notes = append(notes, "Primary test paths: "+strings.Join(values, ", ")+".")
	}

	if values := configdata.StringList(python["stub_paths"]); len(values) > 0 {
		notes = append(notes, "Stub paths: "+strings.Join(values, ", ")+".")
	}

	for _, note := range []string{
		directImportNote(python),
		utilCentralizationNote(python),
		sqlCentralizationNote(python),
		planCompletionNote(python),
		pytestGateNote(python),
		geminiAllowlistNote(gemini),
	} {
		if note != "" {
			notes = append(notes, note)
		}
	}

	return notes
}

func directImportNote(python map[string]any) string {
	directImports := configdata.MapValue(python["direct_imports"])
	if !boolValue(directImports["enabled"]) {
		return ""
	}

	packages := configdata.StringList(directImports["packages"])
	if len(packages) == 0 {
		return ""
	}

	return "Direct internal imports are restricted for packages: " + strings.Join(
		packages,
		", ",
	) + "."
}

func utilCentralizationNote(python map[string]any) string {
	util := configdata.MapValue(python["util_centralization"])
	if !boolValue(util["enabled"]) {
		return ""
	}

	banned := []string{}

	for _, item := range configdata.ListValue(util["banned_modules"]) {
		if itemMap := configdata.MapValue(item); len(itemMap) > 0 {
			module := configdata.StringAt(itemMap, "module")

			alternative := configdata.StringAt(itemMap, "alternative")
			if module != "" && alternative != "" {
				banned = append(banned, module+" -> "+alternative)
			} else if module != "" {
				banned = append(banned, module)
			}

			continue
		}

		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			banned = append(banned, text)
		}
	}

	if len(banned) == 0 {
		return ""
	}

	return "Utility centralization is enabled; banned direct imports: " + strings.Join(
		banned,
		"; ",
	) + "."
}

func sqlCentralizationNote(python map[string]any) string {
	sql := configdata.MapValue(python["sql_centralization"])
	if !boolValue(sql["enabled"]) {
		return ""
	}

	bits := []string{}
	if module := configdata.StringAt(sql, "module_name"); module != "" {
		bits = append(bits, "module "+module)
	}

	if paths := configdata.StringList(sql["central_paths"]); len(paths) > 0 {
		bits = append(bits, "paths "+strings.Join(paths, ", "))
	}

	if len(bits) == 0 {
		return ""
	}

	return "SQL centralization is enabled; keep raw query strings in " + strings.Join(
		bits,
		" and ",
	) + "."
}

func planCompletionNote(python map[string]any) string {
	plan := configdata.MapValue(python["plan_completion"])
	if !boolValue(plan["enabled"]) {
		return ""
	}

	details := []string{}
	if roots := configdata.StringList(plan["root_markers"]); len(roots) > 0 {
		details = append(details, "plan roots "+strings.Join(roots, ", "))
	}

	if metadata := configdata.StringAt(plan, "metadata_filename"); metadata != "" {
		details = append(details, "metadata file "+metadata)
	}

	if len(details) == 0 {
		return ""
	}

	return "Plan workflow enforcement is enabled; " + strings.Join(details, ", ") + "."
}

func pytestGateNote(python map[string]any) string {
	pytest := configdata.MapValue(python["pytest_gate"])
	if !boolValue(pytest["enabled"]) {
		return ""
	}

	command := configdata.StringList(pytest["test_command"])
	if len(command) == 0 {
		return ""
	}

	return "Pytest gate command: " + strings.Join(command, " ") + "."
}

func geminiAllowlistNote(gemini map[string]any) string {
	files := configdata.StringList(gemini["modal_allowlist_files"])
	if len(files) == 0 {
		return ""
	}

	return "Gemini modal-path allowlist: " + strings.Join(files, ", ") + "."
}

func boolValue(value any) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}

	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
}

func stringInConfig(config map[string]any, section, key string) string {
	return configdata.StringAt(configdata.MapValue(config[section]), key)
}
