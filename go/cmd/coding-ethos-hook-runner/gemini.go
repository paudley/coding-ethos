// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func runGeminiCheck(_ Config, args []string) int {
	options, err := parseGeminiCLIOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	settings, runtimePaths, err := loadGeminiSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled && !options.DryRun {
		return 0
	}

	prepared, scope, err := buildGeminiPreparedChecks(
		options,
		settings,
		runtimePaths,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	plan := buildGeminiExecutionPlan(prepared, scope, options.DryRun)
	if options.DryRun {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")

		err := encoder.Encode(plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: write Gemini dry-run plan: %v\n", err)

			return 1
		}

		return 0
	}

	if countGeminiBatches(prepared) == 0 {
		return 0
	}

	apiKey := geminiAPIKey()
	if apiKey == "" {
		fmt.Fprintln(
			os.Stderr,
			"FATAL: GEMINI_API_KEY not set. AI code review is required. "+
				"Add GEMINI_API_KEY to your environment.",
		)

		return 1
	}

	changedLinesByFile := collectGeminiChangedLines(
		geminiPreparedFiles(prepared),
		scope,
	)

	outcomes := executeGeminiChecks(
		context.Background(),
		settings,
		apiKey,
		prepared,
		changedLinesByFile,
	)
	if report := formatGeminiReport(
		scope,
		outcomes,
		selectedHookOutputFormat(),
	); report != "" {
		writeText(os.Stdout, report)
	}

	return geminiOutcomeExitCode(outcomes)
}

func buildGeminiPreparedChecks(
	options GeminiCLIOptions,
	settings GeminiSettings,
	runtimePaths geminiRuntimePaths,
) ([]geminiPreparedCheck, string, error) {
	pack, err := loadGeminiPromptPack(runtimePaths.BundleRoot)
	if err != nil {
		return nil, "", err
	}

	files, scope, err := candidateFilesForGemini(options, pack)
	if err != nil {
		return nil, "", err
	}

	prepared, err := prepareGeminiChecks(
		pack,
		files,
		options.CheckType,
		settings,
		runtimePaths.CacheDir,
	)
	if err != nil {
		return nil, "", err
	}

	return prepared, scope, nil
}

func countGeminiBatches(prepared []geminiPreparedCheck) int {
	totalBatches := 0
	for _, check := range prepared {
		totalBatches += len(check.Batches)
	}

	return totalBatches
}

func geminiPreparedFiles(prepared []geminiPreparedCheck) []string {
	totalFiles := 0
	for _, check := range prepared {
		totalFiles += len(check.Plan.IncludedFiles)
	}

	files := make([]string, 0, totalFiles)
	for _, check := range prepared {
		files = append(files, check.Plan.IncludedFiles...)
	}

	return files
}

func geminiOutcomeExitCode(outcomes []geminiCheckOutcome) int {
	hasErrors, hasCriticals, hasAnyInDiff := summarizeGeminiOutcomes(outcomes)
	switch {
	case hasCriticals:
		fmt.Fprint(
			os.Stderr,
			"\nXX Commit blocked: CRITICAL Gemini violations were found in "+
				"the checked files.\n\n",
		)

		return 1
	case hasErrors:
		fmt.Fprint(
			os.Stderr,
			"\nXX Commit blocked: Gemini API errors prevented code verification.\n\n",
		)

		return 1
	case hasAnyInDiff:
		fmt.Fprint(
			os.Stderr,
			"\nW  Gemini reported non-blocking issues in the checked files.\n\n",
		)
	}

	return 0
}
