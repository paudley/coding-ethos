// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,funlen,gocyclo,lll,noinlineerr,wsl_v5 // Artifact cleanup remains visible.
package tokeneconomy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errArtifact = errors.New("token-economy artifact error")

// ArtifactSet identifies the verified report files created by one write.
type ArtifactSet struct {
	JSONPath       string `json:"json_path"`
	MarkdownPath   string `json:"markdown_path"`
	ReceiptPath    string `json:"receipt_path"`
	JSONSHA256     string `json:"json_sha256"`
	MarkdownSHA256 string `json:"markdown_sha256"`
}

// WriteReportArtifacts writes create-new JSON, Markdown, and checksum files.
func WriteReportArtifacts(report Report, outputPrefix string) (ArtifactSet, error) {
	prefix, err := filepath.Abs(strings.TrimSpace(outputPrefix))
	if err != nil {
		return ArtifactSet{}, fmt.Errorf("resolve token-economy output prefix: %w", err)
	}
	if strings.TrimSpace(outputPrefix) == "" {
		return ArtifactSet{}, fmt.Errorf("%w: output prefix is required", errArtifact)
	}

	artifacts := ArtifactSet{
		JSONPath:     prefix + ".json",
		MarkdownPath: prefix + ".md",
		ReceiptPath:  prefix + ".sha256",
	}
	for _, path := range []string{
		artifacts.JSONPath,
		artifacts.MarkdownPath,
		artifacts.ReceiptPath,
	} {
		if _, statErr := os.Stat(path); statErr == nil {
			return ArtifactSet{}, fmt.Errorf("%w: path already exists: %s", errArtifact, path)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return ArtifactSet{}, fmt.Errorf(
				"inspect token-economy artifact %s: %w",
				path,
				statErr,
			)
		}
	}

	err = os.MkdirAll(filepath.Dir(prefix), storeDirMode)
	if err != nil {
		return ArtifactSet{}, fmt.Errorf("create token-economy artifact directory: %w", err)
	}

	jsonPayload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ArtifactSet{}, fmt.Errorf("encode token-economy JSON report: %w", err)
	}
	jsonPayload = append(jsonPayload, '\n')
	markdownPayload := []byte(formatReportMarkdown(report))

	created := []string{}
	cleanup := func() {
		for _, path := range created {
			_ = os.Remove(path)
		}
	}

	err = writeExclusiveArtifact(artifacts.JSONPath, jsonPayload)
	if err != nil {
		return ArtifactSet{}, err
	}
	created = append(created, artifacts.JSONPath)

	err = writeExclusiveArtifact(artifacts.MarkdownPath, markdownPayload)
	if err != nil {
		cleanup()

		return ArtifactSet{}, err
	}
	created = append(created, artifacts.MarkdownPath)

	artifacts.JSONSHA256, err = ledgerFileSHA256(artifacts.JSONPath)
	if err != nil {
		cleanup()

		return ArtifactSet{}, err
	}
	artifacts.MarkdownSHA256, err = ledgerFileSHA256(artifacts.MarkdownPath)
	if err != nil {
		cleanup()

		return ArtifactSet{}, err
	}

	receipt := artifacts.JSONSHA256 + "  " + filepath.Base(artifacts.JSONPath) + "\n" +
		artifacts.MarkdownSHA256 + "  " + filepath.Base(artifacts.MarkdownPath) + "\n"
	err = writeExclusiveArtifact(artifacts.ReceiptPath, []byte(receipt))
	if err != nil {
		cleanup()

		return ArtifactSet{}, err
	}

	return artifacts, nil
}

func writeExclusiveArtifact(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
	if err != nil {
		return fmt.Errorf("create token-economy artifact %s: %w", path, err)
	}

	_, writeErr := file.Write(payload)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write token-economy artifact %s: %w", path, err)
	}

	return nil
}

func formatReportMarkdown(report Report) string {
	var output strings.Builder

	output.WriteString("# Coding Ethos Token Economy Report\n\n")
	writeMarkdownField(&output, "Cohort", report.Cohort)
	writeMarkdownField(&output, "Generated", report.GeneratedAtUTC)
	writeMarkdownField(&output, "Conclusion", string(report.Conclusion))
	writeMarkdownField(&output, "Causal", strconv.FormatBool(report.Causal))

	if report.Historical != nil {
		output.WriteString("\n## Observational context reduction\n\n")
		writeMarkdownField(&output, "Window start (inclusive)", report.Historical.FromUTC)
		writeMarkdownField(&output, "Window end (exclusive)", report.Historical.ToUTC)
		writeMarkdownField(
			&output,
			"Raw estimated tokens",
			strconv.FormatInt(report.Historical.RawContextTokens, 10),
		)
		writeMarkdownField(
			&output,
			"Delivered estimated tokens",
			strconv.FormatInt(report.Historical.DeliveredContextTokens, 10),
		)
		writeMarkdownField(
			&output,
			"Avoided estimated tokens",
			strconv.FormatInt(report.Historical.AvoidedContextTokens, 10),
		)
		writeMarkdownField(
			&output,
			"Gross reduction",
			fmt.Sprintf("%.2f%%", report.Historical.GrossReductionPercent),
		)

		output.WriteString("\n### Verified sources\n\n")
		for _, source := range report.Historical.Sources {
			output.WriteString("- ")
			output.WriteString(markdownCodeSpan(source.Path))
			output.WriteString(": ")
			output.WriteString(markdownCodeSpan(source.SHA256After))
			output.WriteString(" (unchanged)\n")
		}
	}

	if len(report.Comparisons) > 0 {
		output.WriteString("\n## Arm comparisons\n\n")
		output.WriteString(
			"| Treatment | Control | Tasks | Savings | Adjusted interval | Confidence | Quality |\n",
		)
		output.WriteString("|---|---|---:|---:|---:|---:|---|\n")
		for _, comparison := range report.Comparisons {
			writeComparisonMarkdownRow(&output, comparison)
		}
	}

	output.WriteString("\n## Coverage\n\n")
	writeMarkdownField(&output, "Tasks", strconv.Itoa(report.Coverage.TaskCount))
	writeMarkdownField(
		&output,
		"Complete three-arm tasks",
		strconv.Itoa(report.Coverage.CompleteTaskCount),
	)
	writeMarkdownField(
		&output,
		"Complete task-replicate blocks",
		strconv.Itoa(report.Coverage.CompleteBlockCount),
	)
	writeMarkdownField(
		&output,
		"Partial task-replicate blocks",
		strconv.Itoa(report.Coverage.PartialBlockCount),
	)
	writeMarkdownField(&output, "Runs", strconv.Itoa(report.Coverage.RunCount))
	writeMarkdownField(
		&output,
		"Accepted runs",
		strconv.Itoa(report.Coverage.AcceptedRunCount),
	)
	for _, reason := range report.Coverage.Reasons {
		output.WriteString("- ")
		output.WriteString(strings.ReplaceAll(reason, "\n", " "))
		output.WriteByte('\n')
	}

	return output.String()
}

func markdownCodeSpan(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
	longestRun := 0
	currentRun := 0
	for _, character := range value {
		if character != '`' {
			currentRun = 0

			continue
		}

		currentRun++
		longestRun = max(longestRun, currentRun)
	}

	delimiter := strings.Repeat("`", longestRun+1)
	padding := ""
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") ||
		strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		padding = " "
	}

	return delimiter + padding + value + padding + delimiter
}

func writeMarkdownField(output *strings.Builder, label, value string) {
	output.WriteString("- ")
	output.WriteString(label)
	output.WriteString(": ")
	output.WriteString(strings.ReplaceAll(value, "\n", " "))
	output.WriteByte('\n')
}

func writeComparisonMarkdownRow(output *strings.Builder, comparison Comparison) {
	output.WriteString("| ")
	output.WriteString(string(comparison.TreatmentArm))
	output.WriteString(" | ")
	output.WriteString(string(comparison.ControlArm))
	output.WriteString(" | ")
	output.WriteString(strconv.Itoa(comparison.TaskCount))
	output.WriteString(" | ")
	output.WriteString(strconv.FormatFloat(comparison.SavingsPercent, 'f', 2, 64))
	output.WriteString("% | ")
	output.WriteString(
		strconv.FormatFloat(comparison.SavingsPercentInterval.Lower, 'f', 2, 64),
	)
	output.WriteString("% to ")
	output.WriteString(
		strconv.FormatFloat(comparison.SavingsPercentInterval.Upper, 'f', 2, 64),
	)
	output.WriteString("% | ")
	output.WriteString(strconv.FormatFloat(comparison.ConfidenceLevelPercent, 'f', 2, 64))
	output.WriteString("% | ")
	output.WriteString(qualityLabel(comparison))
	output.WriteString(" |\n")
}

func qualityLabel(comparison Comparison) string {
	if comparison.QualityNonInferior {
		return "non-inferior"
	}

	return "not established"
}
