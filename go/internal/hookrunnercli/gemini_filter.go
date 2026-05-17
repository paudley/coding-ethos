// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

func normalizeGeminiModalAllowlistPattern(pattern string) string {
	return normalizeGeminiPath(pattern)
}

func isModalGeminiViolation(violation geminiViolation) bool {
	text := strings.ToLower(
		fmt.Sprintf("%s %s", violation.EthosSection, violation.Message),
	)
	modalSection := containsAny(text, modalSectionMarkers())
	modalShape := containsAny(text, modalShapeMarkers())
	nonModalSection7 := strings.Contains(text, "section 7") &&
		!modalShape &&
		!strings.Contains(text, "sections 5+7+19") &&
		!strings.Contains(text, "if available")

	return modalSection && modalShape && !nonModalSection7
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}

	return false
}

func modalSectionMarkers() []string {
	return []string{
		"section 19",
		"one path for critical operations",
		"sections 5+7+19",
		"no optional internal state for capabilities",
		"section 7",
		"if available",
	}
}

func modalShapeMarkers() []string {
	return []string{
		"modal",
		"gates the",
		"gates ",
		"gating feature enablement",
		"conditionally disables",
		"conditional execution paths",
		"different execution paths",
		"based on a configuration field",
		"based on an input type",
		"via configuration",
		"enabled/disabled",
		"silently degrade",
		"silent degradation",
		"skipping the",
		"skip the",
		"full job",
	}
}

func geminiGlobMatches(pattern, candidate string) bool {
	replaced := regexp.QuoteMeta(pattern)
	replaced = strings.ReplaceAll(replaced, `\*\*`, "<<double-star>>")
	replaced = strings.ReplaceAll(replaced, `\*`, `[^/]*`)
	replaced = strings.ReplaceAll(replaced, `<<double-star>>`, `.*`)
	replaced = strings.ReplaceAll(replaced, `\?`, `[^/]`)
	matched, err := regexp.MatchString("^"+replaced+"$", candidate)

	return err == nil && matched
}

func isGeminiModalAllowlisted(filePath string, patterns []string) bool {
	normalized := normalizeGeminiPath(filePath)
	for _, pattern := range patterns {
		if geminiGlobMatches(pattern, normalized) {
			return true
		}
	}

	return false
}

func filterGeminiModalAllowlistedViolations(
	violations []geminiViolation,
	patterns []string,
) []geminiViolation {
	if len(patterns) == 0 {
		return violations
	}

	filtered := make([]geminiViolation, 0, len(violations))
	for _, violation := range violations {
		if isModalGeminiViolation(violation) &&
			violation.File != "" &&
			isGeminiModalAllowlisted(violation.File, patterns) {
			continue
		}

		filtered = append(filtered, violation)
	}

	return filtered
}

func parseGeminiChangedLines(diffOutput string) map[int]struct{} {
	changedLines := make(map[int]struct{})

	hunkPattern := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	for line := range strings.SplitSeq(diffOutput, "\n") {
		match := hunkPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		start, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}

		count := 1

		if match[2] != "" {
			parsedCount, err := strconv.Atoi(match[2])
			if err != nil {
				continue
			}

			count = parsedCount
		}

		for lineNumber := start; lineNumber < start+count; lineNumber++ {
			changedLines[lineNumber] = struct{}{}
		}
	}

	return changedLines
}

func changedLinesForGeminiFile(path, scope string) map[int]struct{} {
	var cmd *exec.Cmd

	switch scope {
	case "branch":
		cmd = safeexec.CommandContext(
			context.Background(),
			"git",
			"diff",
			"--no-ext-diff",
			"-U0",
			"origin/main...HEAD",
			"--",
			path,
		)
	default:
		cmd = safeexec.CommandContext(
			context.Background(),
			"git",
			"diff",
			"--no-ext-diff",
			"-U0",
			"--staged",
			path,
		)
	}

	output, err := cmd.Output()
	if err != nil {
		return map[int]struct{}{}
	}

	return parseGeminiChangedLines(string(output))
}

func collectGeminiChangedLines(
	files []string,
	scope string,
) map[string]map[int]struct{} {
	changed := make(map[string]map[int]struct{}, len(files))
	for _, file := range files {
		normalized := normalizeGeminiPath(file)
		changed[normalized] = changedLinesForGeminiFile(file, scope)
	}

	return changed
}

func isGeminiAddedOrUntracked(ctx context.Context, path string) bool {
	output, err := safeexec.CommandContext(
		ctx,
		"git",
		"status",
		"--porcelain",
		path,
	).Output()
	if err != nil {
		return false
	}

	status := string(output)

	return strings.HasPrefix(status, "A ") || strings.HasPrefix(status, "?? ")
}

func filterGeminiViolationsByDiff(
	ctx context.Context,
	violations []geminiViolation,
	changedLinesByFile map[string]map[int]struct{},
) geminiFilteredViolations {
	filtered := geminiFilteredViolations{
		InDiff:      make([]geminiViolation, 0),
		PreExisting: make([]geminiViolation, 0),
	}

	for _, violation := range violations {
		if violation.Line == 0 {
			filtered.InDiff = append(filtered.InDiff, violation)

			continue
		}

		changedLines := changedLinesByFile[normalizeGeminiPath(violation.File)]
		if len(changedLines) == 0 {
			appendGeminiViolationWithoutChangedLines(ctx, &filtered, violation)

			continue
		}

		if _, ok := changedLines[violation.Line]; ok {
			filtered.InDiff = append(filtered.InDiff, violation)
		} else {
			filtered.PreExisting = append(filtered.PreExisting, violation)
		}
	}

	return filtered
}

func appendGeminiViolationWithoutChangedLines(
	ctx context.Context,
	filtered *geminiFilteredViolations,
	violation geminiViolation,
) {
	if violation.File == "" {
		filtered.InDiff = append(filtered.InDiff, violation)

		return
	}

	_, err := os.Stat(violation.File)
	if err != nil {
		filtered.InDiff = append(filtered.InDiff, violation)

		return
	}

	if isGeminiAddedOrUntracked(ctx, violation.File) {
		filtered.InDiff = append(filtered.InDiff, violation)
	} else {
		filtered.PreExisting = append(filtered.PreExisting, violation)
	}
}

func (filtered geminiFilteredViolations) hasBlockingCriticals() bool {
	for _, violation := range filtered.InDiff {
		if violation.Severity == severityCritical {
			return true
		}
	}

	return false
}

func (filtered geminiFilteredViolations) hasAnyInDiff() bool {
	return len(filtered.InDiff) > 0
}

type geminiBatchJob struct {
	Batch      geminiPreparedBatch
	Request    geminiRequestSettings
	CheckIndex int
	BatchIndex int
}

type geminiBatchJobResult struct {
	Outcome    geminiBatchOutcome
	CheckIndex int
	BatchIndex int
}

func buildGeminiExplicitCacheBindings(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	prepared []geminiPreparedCheck,
) map[string]string {
	usageCounts := map[string]int{}
	seeds := map[string]geminiExplicitCacheSeed{}

	for _, check := range prepared {
		if !check.Request.Cache.APIEnabled {
			continue
		}

		for _, batch := range check.Batches {
			if batch.ExplicitAPIKey == "" || strings.TrimSpace(batch.Content) == "" {
				continue
			}

			usageCounts[batch.ExplicitAPIKey]++
			if _, ok := seeds[batch.ExplicitAPIKey]; !ok {
				seeds[batch.ExplicitAPIKey] = geminiExplicitCacheSeed{
					Model:   check.Request.Model,
					Content: batch.Content,
					Cache:   check.Request.Cache,
				}
			}
		}
	}

	bindings := make(map[string]string)

	for key, count := range usageCounts {
		if count < minCollectionItems {
			continue
		}

		cacheName, cacheCreated := ensureGeminiExplicitCache(
			ctx,
			client,
			apiKey,
			seeds[key],
			key,
		)
		if cacheCreated {
			bindings[key] = cacheName
		}
	}

	return bindings
}

func normalizedGeminiModalAllowlistPatterns(
	settings GeminiSettings,
) []string {
	patterns := make([]string, 0, len(settings.ModalAllowlistFiles))
	for _, pattern := range settings.ModalAllowlistFiles {
		normalized := normalizeGeminiModalAllowlistPattern(pattern)
		if normalized != "" {
			patterns = append(patterns, normalized)
		}
	}

	return patterns
}

func initializeGeminiOutcomesAndJobs(
	prepared []geminiPreparedCheck,
) ([]geminiCheckOutcome, []geminiBatchJob) {
	outcomes := make([]geminiCheckOutcome, 0, len(prepared))

	jobs := make([]geminiBatchJob, 0)
	for checkIndex, check := range prepared {
		outcome := geminiCheckOutcome{
			Plan:    check.Plan,
			Batches: make([]geminiBatchOutcome, len(check.Batches)),
			Filtered: geminiFilteredViolations{
				InDiff:      []geminiViolation{},
				PreExisting: []geminiViolation{},
			},
		}
		for batchIndex, batch := range check.Batches {
			outcome.Batches[batchIndex] = geminiBatchOutcome{
				Files: append([]string{}, batch.Files...),
			}
			jobs = append(jobs, geminiBatchJob{
				CheckIndex: checkIndex,
				BatchIndex: batchIndex,
				Request:    check.Request,
				Batch:      batch,
			})
		}

		outcomes = append(outcomes, outcome)
	}

	return outcomes, jobs
}

func maxGeminiConcurrency(settings GeminiSettings) int {
	if settings.MaxConcurrentAPICalls <= 0 {
		return 1
	}

	return settings.MaxConcurrentAPICalls
}

func executeGeminiBatchJob(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	job geminiBatchJob,
	explicitCacheBindings map[string]string,
	patterns []string,
) geminiBatchOutcome {
	batchOutcome := geminiBatchOutcome{
		Files: append([]string{}, job.Batch.Files...),
	}
	prompt, responseDependency, cachedContent := geminiBatchRequestInputs(
		job,
		explicitCacheBindings,
	)

	responseText, err := generateGeminiText(
		ctx,
		client,
		job.Request,
		apiKey,
		prompt,
		responseDependency,
		cachedContent,
	)
	if err != nil {
		batchOutcome.Error = err.Error()

		return batchOutcome
	}

	result, err := parseGeminiResult(responseText)
	if err != nil {
		batchOutcome.Error = err.Error()

		return batchOutcome
	}

	result.Violations = filterGeminiModalAllowlistedViolations(
		result.Violations,
		patterns,
	)
	batchOutcome.Result = result

	return batchOutcome
}

func geminiBatchRequestInputs(
	job geminiBatchJob,
	explicitCacheBindings map[string]string,
) (string, string, string) {
	prompt := job.Batch.Prompt
	responseDependency := ""
	cachedContent := ""

	if cacheName, ok := explicitCacheBindings[job.Batch.ExplicitAPIKey]; ok {
		prompt = job.Batch.CachedPrompt
		responseDependency = job.Batch.ExplicitAPIKey
		cachedContent = cacheName
	}

	return prompt, responseDependency, cachedContent
}

func collectGeminiBatchResults(
	outcomes []geminiCheckOutcome,
	results <-chan geminiBatchJobResult,
) {
	for result := range results {
		outcome := &outcomes[result.CheckIndex]

		outcome.Batches[result.BatchIndex] = result.Outcome
		if result.Outcome.Error != "" {
			outcome.BatchErrors++

			continue
		}

		outcome.BatchesCompleted++
	}
}

func finalizeGeminiOutcomes(
	ctx context.Context,
	outcomes []geminiCheckOutcome,
	changedLinesByFile map[string]map[int]struct{},
) {
	for outcomeIndex := range outcomes {
		outcomes[outcomeIndex].Filtered = filterGeminiViolationsByDiff(
			ctx,
			collectGeminiViolations(outcomes[outcomeIndex].Batches),
			changedLinesByFile,
		)
	}
}
