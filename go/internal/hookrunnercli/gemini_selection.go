// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const shebangProbeBytes = 4096

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
	ThinkingBudget    *int              `json:"thinking_budget,omitempty"`
	Name              string            `json:"name"`
	FileScope         string            `json:"file_scope"`
	Model             string            `json:"model"`
	ServiceTier       string            `json:"service_tier"`
	SelectedFiles     []string          `json:"selected_files"`
	IncludedFiles     []string          `json:"included_files"`
	SkippedLargeFiles []string          `json:"skipped_large_files"`
	Batches           []GeminiBatchPlan `json:"batches"`
	BatchSize         int               `json:"batch_size"`
	MaxFileSizeKB     int               `json:"max_file_size_kb"`
	CacheEnabled      bool              `json:"cache_enabled"`
}

type GeminiExecutionPlan struct {
	Scope  string            `json:"scope"`
	Checks []GeminiCheckPlan `json:"checks"`
	DryRun bool              `json:"dry_run"`
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
	GenerationConfig geminiGenerationConfig
	CachedContent    string
	ServiceTier      string
	Contents         []geminiContent
	SafetySettings   []geminiSafetySetting
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerationConfig struct {
	ThinkingConfig   *geminiThinkingConfig
	ResponseMIMEType string
}

type geminiThinkingConfig struct {
	ThinkingBudget int
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiGenerateResponse struct {
	PromptFeedback map[string]any
	Candidates     []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content geminiCandidateContent `json:"content"`
}

type geminiCandidateContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiCachedContentCreateRequest struct {
	Model       string
	DisplayName string
	TTL         string
	Contents    []geminiContent
}

type geminiCachedContentResponse struct {
	Name       string
	ExpireTime string
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
	EthosSection string `json:"ethos_section"`
	Line         int    `json:"line"`
}

type geminiBatchOutcome struct {
	Error  string       `json:"error,omitempty"`
	Result geminiResult `json:"result"`
	Files  []string     `json:"files"`
}

type geminiFilteredViolations struct {
	InDiff      []geminiViolation `json:"in_diff"`
	PreExisting []geminiViolation `json:"pre_existing"`
}

type geminiCheckOutcome struct {
	Filtered         geminiFilteredViolations `json:"filtered"`
	Batches          []geminiBatchOutcome     `json:"batches"`
	Plan             GeminiCheckPlan          `json:"plan"`
	BatchErrors      int                      `json:"batch_errors"`
	BatchesCompleted int                      `json:"batches_completed"`
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
	CreatedAt string `json:"created_at"`
	Text      string `json:"text"`
}

type geminiExplicitCacheEntry struct {
	Name       string `json:"name"`
	ExpireTime string `json:"expire_time"`
}

type geminiExplicitCacheSeed struct {
	Model   string
	Content string
	Cache   geminiResponseCache
}

func (request geminiRequest) MarshalJSON() ([]byte, error) {
	fields := map[string]any{
		"contents": request.Contents,
	}
	if request.CachedContent != "" {
		fields["cachedContent"] = request.CachedContent
	}

	if request.ServiceTier != "" {
		fields["serviceTier"] = request.ServiceTier
	}

	if request.GenerationConfig.ResponseMIMEType != "" ||
		request.GenerationConfig.ThinkingConfig != nil {
		fields["generationConfig"] = request.GenerationConfig
	}

	if len(request.SafetySettings) > 0 {
		fields["safetySettings"] = request.SafetySettings
	}

	return marshalGeminiJSONFields("request", fields)
}

func (config geminiGenerationConfig) MarshalJSON() ([]byte, error) {
	fields := map[string]any{}
	if config.ThinkingConfig != nil {
		fields["thinkingConfig"] = config.ThinkingConfig
	}

	if config.ResponseMIMEType != "" {
		fields["responseMimeType"] = config.ResponseMIMEType
	}

	return marshalGeminiJSONFields("generation config", fields)
}

func (config geminiThinkingConfig) MarshalJSON() ([]byte, error) {
	return marshalGeminiJSONFields("thinking config", map[string]any{
		"thinkingBudget": config.ThinkingBudget,
	})
}

func (request geminiCachedContentCreateRequest) MarshalJSON() ([]byte, error) {
	fields := map[string]any{"model": request.Model}
	if request.DisplayName != "" {
		fields["displayName"] = request.DisplayName
	}

	if request.TTL != "" {
		fields["ttl"] = request.TTL
	}

	if len(request.Contents) > 0 {
		fields["contents"] = request.Contents
	}

	return marshalGeminiJSONFields("cached content request", fields)
}

func (response *geminiGenerateResponse) UnmarshalJSON(payload []byte) error {
	fields, err := decodeGeminiJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode Gemini response: %w", err)
	}

	err = decodeGeminiJSONFieldsInto(fields, []geminiJSONFieldTarget{
		{key: "promptFeedback", target: &response.PromptFeedback},
		{key: "candidates", target: &response.Candidates},
	})
	if err != nil {
		return err
	}

	return nil
}

func (response *geminiCachedContentResponse) UnmarshalJSON(payload []byte) error {
	fields, err := decodeGeminiJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode Gemini cached content response: %w", err)
	}

	return decodeGeminiJSONFieldsInto(fields, []geminiJSONFieldTarget{
		{key: "name", target: &response.Name},
		{key: "expireTime", target: &response.ExpireTime},
	})
}

func (violation *geminiViolation) UnmarshalJSON(payload []byte) error {
	fields, err := decodeGeminiJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode Gemini violation: %w", err)
	}

	err = decodeGeminiJSONFieldsInto(fields, []geminiJSONFieldTarget{
		{key: "severity", target: &violation.Severity},
		{key: "file", target: &violation.File},
		{key: "message", target: &violation.Message},
		{key: "ethosSection", target: &violation.EthosSection},
		{key: "ethos_section", target: &violation.EthosSection},
		{key: "line", target: &violation.Line},
	})
	if err != nil {
		return err
	}

	return nil
}

func marshalGeminiJSONFields(kind string, fields map[string]any) ([]byte, error) {
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal Gemini %s: %w", kind, err)
	}

	return payload, nil
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
			value, nextIndex, err := nextGeminiCLIArg(args, argIndex)
			if err != nil {
				return options, errCheckTypeValue
			}

			options.CheckType = strings.TrimSpace(value)
			argIndex = nextIndex
		case strings.HasPrefix(arg, "--check-type="):
			_, value, _ := strings.Cut(arg, "=")
			options.CheckType = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--"):
			return options, fmt.Errorf("%w: %s", errUnknownFlag, arg)
		default:
			options.Files = append(options.Files, arg)
		}
	}

	return options, nil
}

func nextGeminiCLIArg(args []string, argIndex int) (string, int, error) {
	nextIndex := argIndex + 1
	if nextIndex >= len(args) {
		return "", 0, errCheckTypeValue
	}

	return args[nextIndex], nextIndex, nil
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
	data, err := readRootedFilePrefix(path, shebangProbeBytes)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) {
		return false, nil
	}

	firstLineBytes, _, _ := bytes.Cut(data, []byte("\n"))

	firstLine := string(firstLineBytes)
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

	for item := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
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

	info, err := statRootedFile(path)
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
	size := batchSize(spec)

	for batchStart := 0; batchStart < len(formattedContents); batchStart += size {
		end := batchStart + spec.BatchSize
		end = min(end, len(formattedContents))

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

func batchSize(spec GeminiPromptCheckSpec) int {
	return spec.BatchSize
}
