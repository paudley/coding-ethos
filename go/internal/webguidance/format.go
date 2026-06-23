// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

import (
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/feedback"
)

// FormatTOON renders a compact agent-facing Modern Web Guidance response.
func FormatTOON(response Response) string {
	const baseScalars = 10

	lines := make(
		[]string,
		0,
		baseScalars+len(response.Results)+len(response.Guides)+len(response.Warnings),
	)
	lines = append(lines,
		"kind: "+feedback.Cell(response.Kind),
		"operation: "+feedback.Cell(response.Operation),
		"advisory: "+strconv.FormatBool(response.Advisory),
		"cache_status: "+feedback.Cell(response.Cache.Status),
		"cache_path: "+feedback.Cell(response.Cache.Path),
		"package: "+feedback.Cell(response.Provenance.PackageName),
		"version: "+feedback.Cell(response.Provenance.ResolvedVersion),
		"dist_tag: "+feedback.Cell(response.Provenance.DistTag),
		"fetched_at: "+feedback.Cell(response.Provenance.FetchTimeUTC),
		"content_hash: "+feedback.Cell(response.Provenance.ContentHash),
	)

	if response.Query != "" {
		lines = append(lines, "query: "+feedback.Cell(response.Query))
	}

	if response.BrowserPolicy != "" {
		lines = append(lines, "browser_policy: "+feedback.Cell(response.BrowserPolicy))
	}

	if response.Provenance.SourceURL != "" {
		lines = append(lines, "source_url: "+feedback.Cell(response.Provenance.SourceURL))
	}

	if len(response.Results) > 0 {
		lines = append(lines, formatSummaryTable(response.Results)...)
	}

	if len(response.Guides) > 0 {
		lines = append(lines, formatGuideTable(response.Guides)...)
	}

	if len(response.Warnings) > 0 {
		lines = append(lines, formatWarnings(response.Warnings)...)
	}

	return strings.Join(lines, "\n")
}

// FormatHuman renders a short operator-facing Modern Web Guidance response.
func FormatHuman(response Response) string {
	lines := make(
		[]string,
		0,
		2+len(response.Results)+len(response.Guides)+len(response.Warnings),
	)
	lines = append(lines,
		"modern web guidance",
		fmt.Sprintf(
			"operation=%s advisory=%t cache=%s version=%s",
			response.Operation,
			response.Advisory,
			response.Cache.Status,
			response.Provenance.ResolvedVersion,
		),
	)

	for _, result := range response.Results {
		lines = append(lines, fmt.Sprintf("- %s: %s", result.ID, result.Description))
	}

	for _, guide := range response.Guides {
		lines = append(lines, fmt.Sprintf("- %s\n%s", guide.ID, guide.Content))
	}

	for _, warning := range response.Warnings {
		lines = append(lines, "warning: "+warning)
	}

	return strings.Join(lines, "\n")
}

func formatSummaryTable(results []GuideSummary) []string {
	lines := make([]string, 0, 1+len(results))
	lines = append(lines,
		fmt.Sprintf(
			"results[%d]{id,category,similarity,tokens,description}:",
			len(results),
		),
	)

	for _, result := range results {
		lines = append(lines, "  "+strings.Join([]string{
			feedback.Cell(result.ID),
			feedback.Cell(result.Category),
			feedback.Cell(formatFloat(result.Similarity)),
			feedback.Cell(formatInt(result.TokenCount)),
			feedback.Cell(result.Description),
		}, ","))
	}

	return lines
}

func formatGuideTable(guides []GuideContent) []string {
	lines := make([]string, 0, 1+len(guides))
	lines = append(
		lines,
		fmt.Sprintf("guides[%d]{id,content_hash,sections,content}:", len(guides)),
	)

	for _, guide := range guides {
		lines = append(lines, "  "+strings.Join([]string{
			feedback.Cell(guide.ID),
			feedback.Cell(guide.ContentHash),
			formatInt(len(guide.Sections)),
			feedback.Cell(guide.Content),
		}, ","))
	}

	return lines
}

func formatWarnings(warnings []string) []string {
	lines := make([]string, 0, 1+len(warnings))
	lines = append(lines, fmt.Sprintf("warnings[%d]{message}:", len(warnings)))

	for _, warning := range warnings {
		lines = append(lines, "  "+feedback.Cell(warning))
	}

	return lines
}

func formatFloat(value float64) string {
	if value == 0 {
		return ""
	}

	return fmt.Sprintf("%.4f", value)
}

func formatInt(value int) string {
	if value == 0 {
		return ""
	}

	return strconv.Itoa(value)
}
