// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type GeminiCLIOptions struct {
	CheckType string
	Files     []string
	DryRun    bool
	FullCheck bool
}

type GeminiBatchPlan struct {
	Files []string `json:"files"`
}

type GeminiCheckPlan struct {
	ThinkingBudget    *int              `json:"thinkingBudget,omitempty"`
	Name              string            `json:"name"`
	FileScope         string            `json:"fileScope"`
	Model             string            `json:"model"`
	ServiceTier       string            `json:"serviceTier"`
	SelectedFiles     []string          `json:"selectedFiles"`
	IncludedFiles     []string          `json:"includedFiles"`
	SkippedLargeFiles []string          `json:"skippedLargeFiles"`
	Batches           []GeminiBatchPlan `json:"batches"`
	BatchSize         int               `json:"batchSize"`
	MaxFileSizeKB     int               `json:"maxFileSizeKb"`
	CacheEnabled      bool              `json:"cacheEnabled"`
}

type GeminiExecutionPlan struct {
	Scope  string            `json:"scope"`
	Checks []GeminiCheckPlan `json:"checks"`
	DryRun bool              `json:"dryRun"`
}

type geminiPreparedBatch struct {
	Prompt         string
	CachedPrompt   string
	Content        string
	ExplicitAPIKey string
	Files          []string
}

type geminiPreparedCheck struct {
	Prompt  string
	Plan    GeminiCheckPlan
	Batches []geminiPreparedBatch
	Request geminiRequestSettings
}

type geminiRequest struct {
	GenerationConfig geminiGenerationConfig `json:"generationConfig,omitempty"`
	CachedContent    string                 `json:"cachedContent,omitempty"`
	ServiceTier      string                 `json:"serviceTier,omitempty"`
	Contents         []geminiContent        `json:"contents"`
	SafetySettings   []geminiSafetySetting  `json:"safetySettings,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerationConfig struct {
	ThinkingConfig   *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseMIMEType string                `json:"responseMimeType,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiGenerateResponse struct {
	PromptFeedback map[string]any `json:"promptFeedback"`
	Candidates     []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type geminiCachedContentCreateRequest struct {
	Model       string          `json:"model"`
	DisplayName string          `json:"displayName,omitempty"`
	TTL         string          `json:"ttl,omitempty"`
	Contents    []geminiContent `json:"contents,omitempty"`
}

type geminiCachedContentResponse struct {
	Name       string `json:"name"`
	ExpireTime string `json:"expireTime"`
}

type geminiAPIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

type geminiResult struct {
	Verdict    string            `json:"verdict"`
	Violations []geminiViolation `json:"violations"`
}

type geminiViolation struct {
	Severity     string `json:"severity"`
	File         string `json:"file"`
	Message      string `json:"message"`
	EthosSection string `json:"ethosSection"`
	Line         int    `json:"line"`
}

type geminiBatchOutcome struct {
	Error  string       `json:"error,omitempty"`
	Result geminiResult `json:"result"`
	Files  []string     `json:"files"`
}

type geminiFilteredViolations struct {
	InDiff      []geminiViolation `json:"inDiff"`
	PreExisting []geminiViolation `json:"preExisting"`
}

type geminiCheckOutcome struct {
	Filtered         geminiFilteredViolations `json:"filtered"`
	Batches          []geminiBatchOutcome     `json:"batches"`
	Plan             GeminiCheckPlan          `json:"plan"`
	BatchErrors      int                      `json:"batchErrors"`
	BatchesCompleted int                      `json:"batchesCompleted"`
}

type geminiReportSummary struct {
	Format   string                `json:"format"`
	Scope    string                `json:"scope"`
	Status   string                `json:"status"`
	Outcomes []geminiOutcomeReport `json:"outcomes"`
}

type geminiOutcomeReport struct {
	Name              string             `json:"name"`
	Status            string             `json:"status"`
	Model             string             `json:"model"`
	ServiceTier       string             `json:"service_tier"`
	BatchErrors       []geminiBatchError `json:"batch_errors,omitempty"`
	SkippedLargeFiles []string           `json:"skipped_large_files,omitempty"`
	InDiff            []geminiViolation  `json:"in_diff,omitempty"`
	PreExisting       []geminiViolation  `json:"pre_existing,omitempty"`
	IncludedFileCount int                `json:"included_file_count"`
	BatchCount        int                `json:"batch_count"`
}

type geminiBatchError struct {
	Error string   `json:"error"`
	Files []string `json:"files"`
	Batch int      `json:"batch"`
}

type geminiRuntimePaths struct {
	BundleRoot   string
	ConsumerRoot string
	CacheDir     string
}

type geminiRequestSettings struct {
	ThinkingBudget        *int
	CheckName             string
	Model                 string
	ServiceTier           string
	Cache                 geminiResponseCache
	MaxRetries            int
	InitialBackoffSeconds float64
	DisableSafetyFilters  bool
}

type geminiResponseCache struct {
	Dir        string
	TTL        time.Duration
	APITTL     time.Duration
	Enabled    bool
	APIEnabled bool
}

type geminiCacheEntry struct {
	CreatedAt string `json:"createdAt"`
	Text      string `json:"text"`
}

type geminiExplicitCacheEntry struct {
	Name       string `json:"name"`
	ExpireTime string `json:"expireTime"`
}

type geminiExplicitCacheSeed struct {
	Model   string
	Content string
	Cache   geminiResponseCache
}

func parseGeminiCLIOptions(args []string) (GeminiCLIOptions, error) {
	options := GeminiCLIOptions{}

	for argIndex := 0; argIndex < len(args); argIndex++ {
		arg := args[argIndex]
		switch {
		case arg == "--dry-run":
			options.DryRun = true
		case arg == "--full-check":
			options.FullCheck = true
		case arg == "--check-type":
			if argIndex+1 >= len(args) {
				return options, errCheckTypeValue
			}

			argIndex++
			options.CheckType = strings.TrimSpace(args[argIndex])
		case strings.HasPrefix(arg, "--check-type="):
			options.CheckType = strings.TrimSpace(
				strings.SplitN(arg, "=", splitNParts)[1],
			)
		case strings.HasPrefix(arg, "--"):
			return options, fmt.Errorf("%w: %s", errUnknownFlag, arg)
		default:
			options.Files = append(options.Files, arg)
		}
	}

	return options, nil
}

func checkNamesFromPromptPack(
	pack GeminiPromptPack,
	checkType string,
) ([]string, error) {
	names := make([]string, 0, len(pack.Checks))
	for name := range pack.Checks {
		names = append(names, name)
	}

	sort.Strings(names)

	if checkType == "" {
		return names, nil
	}

	if _, ok := pack.Checks[checkType]; !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownGeminiCheckType, checkType)
	}

	return []string{checkType}, nil
}

func normalizeGeminiPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "./")
}

func matchesGeminiSelector(path string, selector GeminiFileSelector) (bool, error) {
	normalized := normalizeGeminiPath(path)
	if excludedByGeminiSelector(normalized, selector) {
		return false, nil
	}

	ext := strings.ToLower(filepath.Ext(normalized))
	if matchesGeminiExtension(ext, selector) ||
		matchesGeminiScriptWithoutExtension(normalized, ext, selector) {
		return true, nil
	}

	return matchesGeminiShebang(path, selector)
}

func excludedByGeminiSelector(
	normalized string,
	selector GeminiFileSelector,
) bool {
	for _, pattern := range selector.ExcludeSubstrings {
		if pattern != "" && strings.Contains(normalized, pattern) {
			return true
		}
	}

	for _, pattern := range selector.ExcludePrefixes {
		if pattern != "" && strings.HasPrefix(normalized, pattern) {
			return true
		}
	}

	return false
}

func matchesGeminiExtension(ext string, selector GeminiFileSelector) bool {
	for _, candidate := range selector.IncludeExtensions {
		if ext == strings.ToLower(candidate) {
			return true
		}
	}

	return false
}

func matchesGeminiScriptWithoutExtension(
	normalized string,
	ext string,
	selector GeminiFileSelector,
) bool {
	return selector.AllowExtensionlessInScripts &&
		ext == "" &&
		(strings.Contains(normalized, "scripts/") ||
			strings.Contains(normalized, "scripts\\"))
}

func matchesGeminiShebang(
	path string,
	selector GeminiFileSelector,
) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) {
		return false, nil
	}

	firstLine, _, _ := strings.Cut(string(data), "\n")
	if !strings.HasPrefix(firstLine, "#!") {
		return false, nil
	}

	return shebangMatchesGeminiSelector(firstLine, selector), nil
}

func shebangMatchesGeminiSelector(
	firstLine string,
	selector GeminiFileSelector,
) bool {
	for _, marker := range selector.ShebangMarkers {
		if marker != "" &&
			strings.Contains(strings.ToLower(firstLine), strings.ToLower(marker)) {
			return true
		}
	}

	return false
}

func unionGeminiFileFilter(
	paths []string,
	checks map[string]GeminiPromptCheckSpec,
	names []string,
) ([]string, error) {
	filtered := make([]string, 0, len(paths))
	for _, raw := range existingFiles(paths) {
		include := false

		for _, name := range names {
			spec := checks[name]

			matches, err := matchesGeminiSelector(raw, spec.Selector)
			if err != nil {
				return nil, err
			}

			if matches {
				include = true

				break
			}
		}

		if include {
			filtered = append(filtered, raw)
		}
	}

	return filtered, nil
}

func changedFilesForGeminiFullCheck() ([]string, error) {
	cmd := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--name-only",
		"origin/main...HEAD",
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	var files []string

	for _, item := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		item = strings.TrimSpace(item)
		if item != "" {
			files = append(files, item)
		}
	}

	return files, nil
}

func candidateFilesForGemini(
	options GeminiCLIOptions,
	pack GeminiPromptPack,
) ([]string, string, error) {
	checkNames, err := checkNamesFromPromptPack(pack, options.CheckType)
	if err != nil {
		return nil, "", err
	}

	var candidates []string

	scope := geminiScopeStaged
	if options.FullCheck {
		scope = geminiScopeBranch

		candidates, err = changedFilesForGeminiFullCheck()
		if err != nil {
			return nil, "", err
		}
	} else {
		candidates = options.Files
	}

	files, err := unionGeminiFileFilter(candidates, pack.Checks, checkNames)
	if err != nil {
		return nil, "", err
	}

	return files, scope, nil
}

func buildGeminiExecutionPlan(
	prepared []geminiPreparedCheck,
	scope string,
	dryRun bool,
) GeminiExecutionPlan {
	checks := make([]GeminiCheckPlan, 0, len(prepared))
	for _, item := range prepared {
		checks = append(checks, item.Plan)
	}

	return GeminiExecutionPlan{
		Scope:  scope,
		DryRun: dryRun,
		Checks: checks,
	}
}

func prepareGeminiChecks(
	pack GeminiPromptPack,
	files []string,
	checkType string,
	settings GeminiSettings,
	cacheDir string,
) ([]geminiPreparedCheck, error) {
	checkNames, err := checkNamesFromPromptPack(pack, checkType)
	if err != nil {
		return nil, err
	}

	prepared := make([]geminiPreparedCheck, 0, len(checkNames))
	for _, name := range checkNames {
		check, prepareErr := prepareSingleGeminiCheck(
			pack,
			name,
			files,
			settings,
			cacheDir,
		)
		if prepareErr != nil {
			return nil, prepareErr
		}

		prepared = append(prepared, check)
	}

	return prepared, nil
}

func prepareSingleGeminiCheck(
	pack GeminiPromptPack,
	name string,
	files []string,
	settings GeminiSettings,
	cacheDir string,
) (geminiPreparedCheck, error) {
	requestSettings := resolveGeminiRequestSettings(settings, name, cacheDir)
	spec := defaultGeminiPromptSpec(pack.Checks[name])
	promptTemplate := pack.Prompts[name]

	selected, included, skippedLarge, formattedContents, err := collectGeminiCheckFiles(
		files,
		spec,
	)
	if err != nil {
		return geminiPreparedCheck{}, err
	}

	batches, batchPlans := buildGeminiCheckBatches(
		included,
		formattedContents,
		spec,
		promptTemplate,
		requestSettings,
	)

	return geminiPreparedCheck{
		Plan: GeminiCheckPlan{
			Name:              name,
			FileScope:         spec.FileScope,
			Model:             requestSettings.Model,
			ServiceTier:       requestSettings.ServiceTier,
			ThinkingBudget:    requestSettings.ThinkingBudget,
			CacheEnabled:      requestSettings.Cache.Enabled,
			SelectedFiles:     selected,
			IncludedFiles:     included,
			SkippedLargeFiles: skippedLarge,
			BatchSize:         spec.BatchSize,
			MaxFileSizeKB:     spec.MaxFileSizeKB,
			Batches:           batchPlans,
		},
		Prompt:  promptTemplate,
		Request: requestSettings,
		Batches: batches,
	}, nil
}

func defaultGeminiPromptSpec(spec GeminiPromptCheckSpec) GeminiPromptCheckSpec {
	if spec.BatchSize <= 0 {
		spec.BatchSize = 1
	}

	if spec.MaxFileSizeKB <= 0 {
		spec.MaxFileSizeKB = 100
	}

	return spec
}

func collectGeminiCheckFiles(
	files []string,
	spec GeminiPromptCheckSpec,
) ([]string, []string, []string, []string, error) {
	selected := make([]string, 0, len(files))
	included := make([]string, 0, len(files))
	skippedLarge := make([]string, 0)
	formattedContents := make([]string, 0, len(files))

	for _, path := range files {
		fileStatus, err := geminiCheckFileStatus(path, spec)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		if !fileStatus.selected {
			continue
		}

		selected = append(selected, path)
		if fileStatus.skippedLarge {
			skippedLarge = append(skippedLarge, path)

			continue
		}

		if fileStatus.binary {
			continue
		}

		included = append(included, path)
		formattedContents = append(formattedContents, fileStatus.formattedContent)
	}

	return selected, included, skippedLarge, formattedContents, nil
}

type geminiCheckFileSelection struct {
	formattedContent string
	selected         bool
	skippedLarge     bool
	binary           bool
}

func geminiCheckFileStatus(
	path string,
	spec GeminiPromptCheckSpec,
) (geminiCheckFileSelection, error) {
	matches, err := matchesGeminiSelector(path, spec.Selector)
	if err != nil {
		return geminiCheckFileSelection{}, err
	}

	if !matches {
		return geminiCheckFileSelection{}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return geminiCheckFileSelection{}, fmt.Errorf("stat %s: %w", path, err)
	}

	if info.Size() > int64(spec.MaxFileSizeKB*kibibyte) {
		return geminiCheckFileSelection{selected: true, skippedLarge: true}, nil
	}

	text, binary, err := readText(path)
	if err != nil {
		return geminiCheckFileSelection{}, err
	}

	if binary {
		return geminiCheckFileSelection{selected: true, binary: true}, nil
	}

	return geminiCheckFileSelection{
		selected:         true,
		formattedContent: fmt.Sprintf("--- %s ---\n%s\n", path, text),
	}, nil
}

func buildGeminiCheckBatches(
	included []string,
	formattedContents []string,
	spec GeminiPromptCheckSpec,
	promptTemplate string,
	requestSettings geminiRequestSettings,
) ([]geminiPreparedBatch, []GeminiBatchPlan) {
	batchPlans := make([]GeminiBatchPlan, 0)
	batches := make([]geminiPreparedBatch, 0)

	for batchStart := 0; batchStart < len(formattedContents); batchStart += spec.BatchSize {
		end := batchStart + spec.BatchSize
		if end > len(formattedContents) {
			end = len(formattedContents)
		}

		batchFiles := append([]string{}, included[batchStart:end]...)
		batchContent := strings.Join(formattedContents[batchStart:end], "\n")
		batchPrompt := geminiPromptWithInlineContent(promptTemplate, batchContent)
		batches = append(batches, geminiPreparedBatch{
			Files:        batchFiles,
			Prompt:       batchPrompt,
			CachedPrompt: geminiPromptForExplicitCachedContent(promptTemplate),
			Content:      batchContent,
			ExplicitAPIKey: geminiExplicitContentKey(
				requestSettings.Model,
				batchContent,
			),
		})
		batchPlans = append(batchPlans, GeminiBatchPlan{Files: batchFiles})
	}

	return batches, batchPlans
}
