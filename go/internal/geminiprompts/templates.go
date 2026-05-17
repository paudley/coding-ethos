// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package geminiprompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const repoGroundingMarker = "{{ render_repo_grounding(" +
	"repo_overview, repo_commands, repo_paths, repo_notes, " +
	"gemini_notes, enforcement_notes) }}"

type templateFile struct {
	CheckName string
	FileName  string
}

func templateFiles() []templateFile {
	return []templateFile{
		{CheckName: "code_ethos", FileName: "code_ethos.j2"},
		{CheckName: "shell_review", FileName: "shell_review.j2"},
		{CheckName: "shell_ethos", FileName: "shell_ethos.j2"},
		{CheckName: "shell_documentation", FileName: "shell_documentation.j2"},
		{CheckName: "shellcheck_suppression", FileName: "shellcheck_suppression.j2"},
		{CheckName: "shell_placeholder", FileName: "shell_placeholder.j2"},
	}
}

func renderPrompts(ethosRoot string, context promptContext) (map[string]string, error) {
	files := templateFiles()
	prompts := make(map[string]string, len(files))

	for _, file := range files {
		content, err := os.ReadFile(
			filepath.Join(ethosRoot, "pre-commit", "prompts", "checks", file.FileName),
		)
		if err != nil {
			return nil, fmt.Errorf("read prompt template %s: %w", file.FileName, err)
		}

		rendered := renderTemplate(string(content), context)
		prompts[file.CheckName] = strings.TrimRight(rendered, "\n") + "\n"
	}

	return prompts, nil
}

func renderTemplate(template string, context promptContext) string {
	rendered := removeJinjaImport(template)
	rendered = strings.ReplaceAll(rendered, "{{ project_name }}", context.ProjectName)
	rendered = strings.ReplaceAll(
		rendered,
		"{{ project_context }}",
		context.ProjectContext,
	)
	rendered = strings.ReplaceAll(
		rendered,
		"{{ code_content_placeholder }}",
		"{code_content}",
	)
	rendered = replaceMacro(
		rendered,
		repoGroundingMarker,
		renderRepoGrounding(context),
	)
	rendered = replaceMacro(
		rendered,
		"{{ render_principles(principles) }}",
		renderPrinciples(context.Principles),
	)

	return rendered
}

func removeJinjaImport(template string) string {
	lines := strings.Split(template, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "{% from ") {
		return strings.Join(lines[1:], "\n")
	}

	return template
}

func replaceMacro(template, marker, value string) string {
	return strings.ReplaceAll(template, marker, strings.TrimRight(value, "\n"))
}

func renderRepoGrounding(context promptContext) string {
	var builder strings.Builder
	builder.WriteString("## Repo Grounding\n")

	if context.RepoOverview != "" {
		builder.WriteString("- Repo overview: ")
		builder.WriteString(context.RepoOverview)
		builder.WriteString("\n")
	}

	for _, command := range context.RepoCommands {
		builder.WriteString("- Command `")
		builder.WriteString(command.Name)
		builder.WriteString("`: ")
		builder.WriteString(strings.Join(command.Examples, " | "))
		builder.WriteString("\n")
	}

	for _, repoPath := range context.RepoPaths {
		builder.WriteString("- Path `")
		builder.WriteString(repoPath.Name)
		builder.WriteString("`: ")
		builder.WriteString(repoPath.Path)
		builder.WriteString("\n")
	}

	for _, note := range context.RepoNotes {
		builder.WriteString("- Repo note: ")
		builder.WriteString(note)
		builder.WriteString("\n")
	}

	for _, note := range context.GeminiNotes {
		builder.WriteString("- Gemini note: ")
		builder.WriteString(note)
		builder.WriteString("\n")
	}

	for _, note := range context.EnforcementNotes {
		builder.WriteString("- Enforcement: ")
		builder.WriteString(note)
		builder.WriteString("\n")
	}

	return builder.String()
}

func renderPrinciples(principles []principlePayload) string {
	var builder strings.Builder
	builder.WriteString("## ETHOS Grounding\n")

	for _, item := range principles {
		builder.WriteString("- ")
		fmt.Fprintf(&builder, "%02d", item.Order)
		builder.WriteString(". ")
		builder.WriteString(item.Title)
		builder.WriteString(": ")
		builder.WriteString(item.Directive)
		builder.WriteString("\n")

		if len(item.QuickRef) > 0 {
			builder.WriteString("  Quick ref: ")
			builder.WriteString(strings.Join(item.QuickRef, "; "))
			builder.WriteString("\n")
		}

		if item.AgentHint != "" {
			builder.WriteString("  Gemini hint: ")
			builder.WriteString(item.AgentHint)
			builder.WriteString("\n")
		}
	}

	return builder.String()
}
