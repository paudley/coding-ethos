// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle,gosec,funlen,lll,embeddedstructfieldcheck,modernize // External schemas and bundled hook orchestration.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
	"go.yaml.in/yaml/v3"
)

const (
	configEnv         = "CODE_ETHOS_PRECOMMIT_CONFIG"
	precommitRootEnv  = "CODE_ETHOS_PRECOMMIT_ROOT"
	consumerRootEnv   = "CODE_ETHOS_CONSUMER_ROOT"
	privateKeyPattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	textChunkSize     = 8192
	severityCritical  = "CRITICAL"
	severityInfo      = "INFO"
	severityWarning   = "WARNING"
)

type Config struct {
	QuietFilter       QuietFilterConfig
	CommitAttribution struct{ BlockedNames []string }
	Shell             struct{ RequireCommonForPrefixes []string }
	Text              struct {
		ForbiddenStrings         []string
		LargeFileExcludePrefixes []string
		LargeFileSuffixes        []string
		MaxLargeFileKB           int
	}
	CommitLint struct {
		AllowedTypes    []string
		IgnoredPrefixes []string
		MaxHeaderLength int
	}
	LineLimits struct {
		PythonHard int
		PythonWarn int
		ShellHard  int
		ShellWarn  int
	}
}

type GeminiSettings struct {
	ServiceTierOverrides    map[string]string
	ThinkingBudgetOverrides map[string]int
	ModelOverrides          map[string]string
	ThinkingBudget          *int
	ServiceTier             string
	Model                   string
	ModalAllowlistFiles     []string
	Cache                   GeminiCacheSettings
	MaxRetries              int
	TimeoutSeconds          int
	InitialBackoffSeconds   float64
	MaxConcurrentAPICalls   int
	Enabled                 bool
	DisableSafetyFilters    bool
}

type GeminiCacheSettings struct {
	Dirname       string
	TTLSeconds    int
	APITTLSeconds int
	Enabled       bool
	APIEnabled    bool
}

type QuietFilterConfig struct {
	ANSIRegex        string
	FailedRegex      string
	PassedRegex      string
	PreexistingRegex string
	SeparatorRegex   string
	SkippedRegex     string
	StatusRegex      string
	MetadataPrefixes []string
	SuppressExact    []string
	SuppressPrefixes []string
	SuppressRegexes  []string
	BannerWidth      int
}

type GeminiPromptCheckSpec struct {
	FileScope     string             `json:"fileScope"`
	Selector      GeminiFileSelector `json:"selector"`
	BatchSize     int                `json:"batchSize"`
	MaxFileSizeKB int                `json:"maxFileSizeKb"`
}

type GeminiFileSelector struct {
	IncludeExtensions           []string `json:"includeExtensions"`
	ExcludeSubstrings           []string `json:"excludeSubstrings"`
	ExcludePrefixes             []string `json:"excludePrefixes"`
	ShebangMarkers              []string `json:"shebangMarkers"`
	AllowExtensionlessInScripts bool     `json:"allowExtensionlessInScripts"`
}

type GeminiPromptPack struct {
	Checks  map[string]GeminiPromptCheckSpec `json:"checks"`
	Prompts map[string]string                `json:"prompts"`
	Version int                              `json:"version"`
}

type manifestValidationSettings struct {
	RequiredListSections map[string]manifestValidationListSpec
	CandidatePaths       []string
	RequiredStringFields []string
	Enabled              bool
}

type manifestValidationListSpec struct {
	RequiredStringFields []string
	OptionalStringFields []string
	Required             bool
}

type planCompletionSettings struct {
	MetadataFilename      string
	RootMarkers           []string
	CompletedStatusValues []string
	Enabled               bool
}

type commentSuppressionSettings struct {
	Patterns []commentSuppressionPattern
	Enabled  bool
}

type commentSuppressionPattern struct {
	Regex string
	Label string
}

type compiledCommentSuppressionPattern struct {
	Regex *regexp.Regexp
	Label string
}

type commentSuppressionViolation struct {
	File    string
	Label   string
	Comment string
	Line    int
}

type moduleDocsSettings struct {
	SourceDocsPath     string
	CheckFilenames     []string
	ExcludedDirs       []string
	BannedDocFilenames []string
	Enabled            bool
}

type moduleDocsViolations struct {
	MissingDocstring []string
	MissingMarkdown  []string
	MissingRefs      []moduleDocsMissingRefs
	MissingIndex     []string
	PathPrefixed     []moduleDocsPathRefs
	NonexistentRefs  []moduleDocsBadRefs
	BannedFilenames  []string
}

type moduleDocsMissingRefs struct {
	PythonFile string
	Markdown   []string
}

type moduleDocsPathRefs struct {
	PythonFile string
	Refs       []string
}

type moduleDocsBadRefs struct {
	PythonFile string
	Refs       []string
}

var (
	moduleDocsSeeAlsoPattern = regexp.MustCompile(`(?im)^See Also:\s*$`)
	moduleDocsEntryPattern   = regexp.MustCompile(
		`(?m)^\s+([A-Za-z0-9_-]+\.md)\s*[:|-]`,
	)
	moduleDocsPathPattern = regexp.MustCompile(
		`(?m)^\s+([A-Za-z0-9_/-]+/[A-Za-z0-9_-]+\.md)\s*[:|-]`,
	)
)

type CommandFunc func(Config, []string) int

func main() {
	if len(os.Args) < minCollectionItems {
		usage()
		os.Exit(1)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	command, ok := defaultHookCommandRegistry().Commands[os.Args[1]]
	if !ok {
		usage()
		os.Exit(1)
	}

	os.Exit(command(cfg, os.Args[2:]))
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook <command> [files...]")
	fmt.Fprintln(
		os.Stderr,
		"  gemini-check supports --dry-run, --full-check, and --check-type <name>",
	)
	fmt.Fprintln(
		os.Stderr,
		"  config-get <dot.path> [default] prints merged config values",
	)
}

func normalizeConfigKey(value string) string {
	replacer := strings.NewReplacer("_", "", "-", "", ".", "")

	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func decodeYAMLValue(value any, target any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		MatchName: func(mapKey string, fieldName string) bool {
			return normalizeConfigKey(mapKey) == normalizeConfigKey(fieldName)
		},
		Result:           target,
		WeaklyTypedInput: true,
	})
	if err != nil {
		return fmt.Errorf("build config decoder: %w", err)
	}

	err = decoder.Decode(value)
	if err != nil {
		return fmt.Errorf("decode config value: %w", err)
	}

	return nil
}

func decodeConfigBlock(value any, label string, target any) error {
	err := decodeYAMLValue(value, target)
	if err != nil {
		return fmt.Errorf("parse %s config: %w", label, err)
	}

	return nil
}

func decodeOptionalConfigSection(
	rootConfig map[string]any,
	path string,
	label string,
	target any,
) (bool, error) {
	value, ok := rootConfigValue(rootConfig, path)
	if !ok {
		return false, nil
	}

	err := decodeConfigBlock(value, label, target)
	if err != nil {
		return false, err
	}

	return true, nil
}

func writeLine(writer io.Writer, text string) {
	_, _ = fmt.Fprintln(writer, text)
}

func writef(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}

func writeText(writer io.Writer, text string) {
	if text == "" {
		return
	}

	_, err := io.WriteString(writer, text)
	if err != nil {
		return
	}

	if !strings.HasSuffix(text, "\n") {
		_, _ = fmt.Fprintln(writer)
	}
}

func loadConfig() (Config, error) {
	var cfg Config

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return cfg, err
	}

	goConfig, ok := rootConfig["go"]
	if !ok {
		return cfg, nil
	}

	err = decodeConfigBlock(goConfig, "go", &cfg)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func loadGeminiSettings() (GeminiSettings, geminiRuntimePaths, error) {
	var (
		settings GeminiSettings
		paths    geminiRuntimePaths
	)

	bundleRoot, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, paths, err
	}

	paths.BundleRoot = bundleRoot
	paths.ConsumerRoot = consumerRoot(filepath.Dir(bundleRoot))

	geminiConfig, ok := rootConfig["gemini"]
	if !ok {
		paths.CacheDir = filepath.Join(
			consumerRuntimeCacheDir(paths.ConsumerRoot),
			"gemini-cache",
		)

		return settings, paths, nil
	}

	err = decodeYAMLValue(geminiConfig, &settings)
	if err != nil {
		return settings, paths, fmt.Errorf("parse gemini config: %w", err)
	}

	if settings.Model == "" {
		settings.Model = geminiDefaultModel
	}

	err = applyGeminiDefaults(&settings)
	if err != nil {
		return settings, paths, err
	}

	paths.CacheDir = filepath.Join(
		consumerRuntimeCacheDir(paths.ConsumerRoot),
		settings.Cache.Dirname,
	)

	return settings, paths, nil
}

func applyGeminiDefaults(settings *GeminiSettings) error {
	serviceTier, err := normalizeGeminiServiceTier(settings.ServiceTier)
	if err != nil {
		return fmt.Errorf("gemini.service_tier: %w", err)
	}

	settings.ServiceTier = serviceTier
	if settings.MaxRetries == 0 {
		settings.MaxRetries = 3
	}

	if settings.TimeoutSeconds == 0 {
		settings.TimeoutSeconds = 300
	}

	if settings.InitialBackoffSeconds == 0 {
		settings.InitialBackoffSeconds = 1
	}

	if settings.MaxConcurrentAPICalls <= 0 {
		settings.MaxConcurrentAPICalls = 1
	}

	if settings.Cache.TTLSeconds <= 0 {
		settings.Cache.TTLSeconds = int((7 * 24 * time.Hour).Seconds())
	}

	if settings.Cache.APITTLSeconds <= 0 {
		settings.Cache.APITTLSeconds = int(time.Hour.Seconds())
	}

	if strings.TrimSpace(settings.Cache.Dirname) == "" {
		settings.Cache.Dirname = "gemini-cache"
	}

	return normalizeGeminiServiceTierOverrides(settings)
}

func normalizeGeminiServiceTierOverrides(settings *GeminiSettings) error {
	for checkName, tier := range settings.ServiceTierOverrides {
		normalized, err := normalizeGeminiServiceTier(tier)
		if err != nil {
			return fmt.Errorf(
				"gemini.service_tier_overrides.%s: %w",
				checkName,
				err,
			)
		}

		settings.ServiceTierOverrides[checkName] = normalized
	}

	return nil
}

func loadMergedRootConfig() (string, map[string]any, error) {
	bundleRoot, err := findBundleRoot()
	if err != nil {
		return "", nil, err
	}

	rootConfig, err := loadYAMLMap(
		filepath.Join(filepath.Dir(bundleRoot), "config.yaml"),
	)
	if err != nil {
		return "", nil, err
	}

	if overridePath := strings.TrimSpace(os.Getenv(configEnv)); overridePath != "" {
		overrideConfig, err := loadYAMLMap(overridePath)
		if err != nil {
			return "", nil, err
		}

		return bundleRoot, deepMerge(rootConfig, overrideConfig), nil
	}

	consumer := consumerRoot(filepath.Dir(bundleRoot))
	for _, candidate := range overrideCandidates(consumer, rootConfig) {
		overrideConfig, err := loadYAMLMap(candidate)
		if err == nil {
			return bundleRoot, deepMerge(rootConfig, overrideConfig), nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
	}

	return bundleRoot, rootConfig, nil
}

func gitOutput(args ...string) string {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Env = externalToolEnv(nil)

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func repoRoot() string {
	if root := strings.TrimSpace(os.Getenv(consumerRootEnv)); root != "" {
		return root
	}

	if root := gitOutput("rev-parse", "--show-toplevel"); root != "" {
		return root
	}

	return "."
}

func consumerRoot(ethosRoot string) string {
	if root := strings.TrimSpace(os.Getenv(consumerRootEnv)); root != "" {
		if explicitConsumerRootApplies(root, ethosRoot) {
			return root
		}
	}

	if root := gitOutput(
		"-C",
		ethosRoot,
		"rev-parse",
		"--show-superproject-working-tree",
	); root != "" {
		return root
	}

	if root := gitOutput("-C", ethosRoot, "rev-parse", "--show-toplevel"); root != "" {
		return root
	}

	return ethosRoot
}

func explicitConsumerRootApplies(root string, ethosRoot string) bool {
	absRoot, rootErr := filepath.Abs(root)

	absEthosRoot, ethosErr := filepath.Abs(ethosRoot)
	if rootErr != nil || ethosErr != nil {
		return root == ethosRoot
	}

	rel, err := filepath.Rel(absRoot, absEthosRoot)
	if err != nil {
		return false
	}

	return rel == "." || !strings.HasPrefix(rel, "..")
}

func consumerRuntimeCacheDir(root string) string {
	return filepath.Join(root, ".code-ethos", "cache")
}

func isBundleRoot(path string) bool {
	info, err := os.Stat(filepath.Join(path, "hooks", "managed-toolchain.tsv"))
	if err != nil || info.IsDir() {
		return false
	}

	hookRuntime, err := os.Stat(filepath.Join(path, "hooks", "go-hooks"))

	return err == nil && hookRuntime.IsDir()
}

func findBundleRoot() (string, error) {
	if envRoot := strings.TrimSpace(os.Getenv(precommitRootEnv)); envRoot != "" {
		if isBundleRoot(envRoot) {
			return envRoot, nil
		}
	}

	root := repoRoot()
	for _, candidate := range []string{"code-ethos/pre-commit", "pre-commit"} {
		resolved := filepath.Join(root, candidate)
		if isBundleRoot(resolved) {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("%w: %s", errBundleRootNotFound, root)
}

func overrideCandidates(root string, rootConfig map[string]any) []string {
	names := []string{
		"repo_config.yaml",
		"repo_config.yml",
		"code-ethos.repo.yaml",
		"code-ethos.repo.yml",
		"coding-ethos.repo.yaml",
		"coding-ethos.repo.yml",
		"code-ethos.pre-commit.yaml",
		"code-ethos.pre-commit.yml",
		"coding-ethos.pre-commit.yaml",
		"coding-ethos.pre-commit.yml",
	}

	if bundle, ok := rootConfig["bundle"].(map[string]any); ok {
		if raw, ok := bundle["consumer_override_candidates"].([]any); ok {
			names = names[:0]

			for _, item := range raw {
				name := strings.TrimSpace(fmt.Sprint(item))
				if name != "" {
					names = append(names, name)
				}
			}
		}
	}

	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(root, name))
	}

	return paths
}

func loadYAMLMap(path string) (map[string]any, error) {
	var cfg map[string]any

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if cfg == nil {
		cfg = map[string]any{}
	}

	return cfg, nil
}

func deepMerge(base map[string]any, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base))
	for key, value := range base {
		merged[key] = value
	}

	for key, value := range override {
		baseMap, baseOK := merged[key].(map[string]any)

		overrideMap, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			merged[key] = deepMerge(baseMap, overrideMap)

			continue
		}

		merged[key] = value
	}

	return merged
}

func rootConfigValue(root map[string]any, path string) (any, bool) {
	current := any(root)

	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}

		nextMap, isMap := current.(map[string]any)
		if !isMap {
			return nil, false
		}

		next, found := nextMap[part]
		if !found {
			return nil, false
		}

		current = next
	}

	return current, true
}

func formatRootConfigValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool, int, int64, float64:
		return fmt.Sprint(typed), nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", fmt.Errorf("marshal config value: %w", err)
		}

		return string(data), nil
	}
}

func configGet(_ Config, args []string) int {
	if len(args) < 1 || strings.TrimSpace(args[0]) == "" {
		writeLine(os.Stderr, "Usage: coding-ethos-hook config-get <dot.path> [default]")

		return 1
	}

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	value, ok := rootConfigValue(rootConfig, args[0])
	if !ok {
		if len(args) >= minCollectionItems {
			writeLine(os.Stdout, args[1])

			return 0
		}

		writef(os.Stderr, "FATAL: config path not found: %s\n", args[0])

		return 1
	}

	formatted, err := formatRootConfigValue(value)
	if err != nil {
		writef(os.Stderr, "FATAL: format config value %s: %v\n", args[0], err)

		return 1
	}

	writeLine(os.Stdout, formatted)

	return 0
}

func loadGeminiPromptPack(bundleRoot string) (GeminiPromptPack, error) {
	var pack GeminiPromptPack

	ethosRoot := filepath.Dir(bundleRoot)
	consumer := consumerRoot(ethosRoot)

	candidates := []string{
		filepath.Join(consumer, ".code-ethos", "gemini", "prompt-pack.json"),
		filepath.Join(ethosRoot, ".code-ethos", "gemini", "prompt-pack.json"),
	}
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return pack, fmt.Errorf("read %s: %w", candidate, err)
		}

		err = json.Unmarshal(data, &pack)
		if err != nil {
			return pack, fmt.Errorf("parse %s: %w", candidate, err)
		}

		if len(pack.Prompts) == 0 {
			return pack, fmt.Errorf(
				"%w: %s",
				errGeminiPackMissingPrompts,
				candidate,
			)
		}

		if len(pack.Checks) == 0 {
			return pack, fmt.Errorf(
				"%w: %s",
				errGeminiPackMissingChecks,
				candidate,
			)
		}

		return pack, nil
	}

	return pack, fmt.Errorf("%w: %s", errGeminiPackNotFound, bundleRoot)
}

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

	for batchStart := 0; batchStart < len(formattedContents); batchStart +=
		spec.BatchSize {
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

func geminiAPIKey() string {
	return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
}

func normalizeGeminiServiceTier(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == "unspecified" {
		return geminiServiceTierNormal, nil
	}

	switch normalized {
	case geminiServiceTierNormal, "flex", "priority":
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: %q", errGeminiServiceTier, value)
	}
}

func resolveGeminiRequestSettings(
	settings GeminiSettings,
	checkName string,
	cacheDir string,
) geminiRequestSettings {
	model := strings.TrimSpace(settings.Model)
	if override := strings.TrimSpace(settings.ModelOverrides[checkName]); override != "" {
		model = override
	}

	if model == "" {
		model = geminiDefaultModel
	}

	serviceTier := settings.ServiceTier
	if override, ok := settings.ServiceTierOverrides[checkName]; ok {
		serviceTier = override
	}

	if serviceTier == "" {
		serviceTier = geminiServiceTierNormal
	}

	var thinkingBudget *int

	if settings.ThinkingBudget != nil {
		value := *settings.ThinkingBudget
		thinkingBudget = &value
	}

	if override, ok := settings.ThinkingBudgetOverrides[checkName]; ok {
		value := override
		thinkingBudget = &value
	}

	return geminiRequestSettings{
		CheckName:             checkName,
		Model:                 model,
		ServiceTier:           serviceTier,
		ThinkingBudget:        thinkingBudget,
		MaxRetries:            settings.MaxRetries,
		InitialBackoffSeconds: settings.InitialBackoffSeconds,
		DisableSafetyFilters:  settings.DisableSafetyFilters,
		Cache: geminiResponseCache{
			Enabled:    settings.Cache.Enabled,
			Dir:        cacheDir,
			TTL:        time.Duration(settings.Cache.TTLSeconds) * time.Second,
			APIEnabled: settings.Cache.APIEnabled,
			APITTL:     time.Duration(settings.Cache.APITTLSeconds) * time.Second,
		},
	}
}

func geminiModelPath(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = geminiDefaultModel
	}

	if !strings.HasPrefix(model, "models/") {
		return "models/" + model
	}

	return model
}

func isRetryableGeminiStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusRequestTimeout ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout ||
		code >= 500
}

func geminiAPIErrorMessage(body []byte, status string) string {
	var apiError geminiAPIErrorResponse

	err := json.Unmarshal(body, &apiError)
	if err == nil {
		switch {
		case apiError.Error.Message != "" && apiError.Error.Status != "":
			return fmt.Sprintf("%s (%s)", apiError.Error.Message, apiError.Error.Status)
		case apiError.Error.Message != "":
			return apiError.Error.Message
		}
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return status
	}

	return text
}

func geminiSafetySettings(disabled bool) []geminiSafetySetting {
	if !disabled {
		return nil
	}

	return []geminiSafetySetting{
		{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_CIVIC_INTEGRITY", Threshold: "OFF"},
	}
}

func geminiPromptWithInlineContent(template string, content string) string {
	if strings.Contains(template, "{code_content}") {
		return strings.Replace(template, "{code_content}", content, 1)
	}

	if strings.TrimSpace(content) == "" {
		return template
	}

	return strings.TrimSpace(template) + "\n\n" + content
}

func geminiPromptForExplicitCachedContent(template string) string {
	replacement := strings.TrimSpace(
		"The source corpus to review is provided as cached content attached " +
			"to this request. " +
			"Analyze that cached file corpus directly; do not ask for it again.",
	)
	if strings.Contains(template, "{code_content}") {
		return strings.Replace(template, "{code_content}", replacement, 1)
	}

	return strings.TrimSpace(template) + "\n\n" + replacement
}

func geminiExplicitContentKey(model string, content string) string {
	sum := sha256.Sum256([]byte(geminiModelPath(model) + "\x00" + content))

	return hex.EncodeToString(sum[:])
}

func geminiCachePath(cache geminiResponseCache, key string) string {
	return filepath.Join(cache.Dir, key+".json")
}

func geminiExplicitCachePath(cache geminiResponseCache, key string) string {
	return filepath.Join(cache.Dir, "explicit-api", key+".json")
}

func readGeminiCache(cache geminiResponseCache, key string) (string, bool, error) {
	if !cache.Enabled {
		return "", false, nil
	}

	path := geminiCachePath(cache, key)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	var entry geminiCacheEntry

	err = json.Unmarshal(data, &entry)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", path, err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	if err != nil {
		return "", false, fmt.Errorf("parse %s timestamp: %w", path, err)
	}

	if cache.TTL > 0 && time.Since(createdAt) > cache.TTL {
		_ = os.Remove(path)

		return "", false, nil
	}

	return entry.Text, true, nil
}

func writeGeminiCache(cache geminiResponseCache, key string, text string) error {
	if !cache.Enabled {
		return nil
	}

	err := os.MkdirAll(cache.Dir, defaultDirPerm)
	if err != nil {
		return fmt.Errorf("create cache dir %s: %w", cache.Dir, err)
	}

	entry := geminiCacheEntry{
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Text:      text,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}

	path := geminiCachePath(cache, key)
	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())

	err = os.WriteFile(tempPath, data, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("write cache temp file %s: %w", tempPath, err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("rename cache file %s: %w", path, err)
	}

	return nil
}

func readGeminiExplicitCache(
	cache geminiResponseCache,
	key string,
) (string, bool, error) {
	if !cache.APIEnabled {
		return "", false, nil
	}

	path := geminiExplicitCachePath(cache, key)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	var entry geminiExplicitCacheEntry

	err = json.Unmarshal(data, &entry)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", path, err)
	}

	if strings.TrimSpace(entry.Name) == "" {
		return "", false, nil
	}

	expireTime, err := time.Parse(time.RFC3339Nano, entry.ExpireTime)
	if err != nil {
		return "", false, fmt.Errorf("parse %s timestamp: %w", path, err)
	}

	if time.Now().UTC().After(expireTime) {
		_ = os.Remove(path)

		return "", false, nil
	}

	return entry.Name, true, nil
}

func writeGeminiExplicitCache(
	cache geminiResponseCache,
	key string,
	name string,
	expireTime time.Time,
) error {
	if !cache.APIEnabled {
		return nil
	}

	path := geminiExplicitCachePath(cache, key)

	err := os.MkdirAll(filepath.Dir(path), defaultDirPerm)
	if err != nil {
		return fmt.Errorf("create cache dir %s: %w", filepath.Dir(path), err)
	}

	entry := geminiExplicitCacheEntry{
		Name:       name,
		ExpireTime: expireTime.UTC().Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode explicit cache entry: %w", err)
	}

	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())

	err = os.WriteFile(tempPath, data, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("write explicit cache temp file %s: %w", tempPath, err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("rename explicit cache file %s: %w", path, err)
	}

	return nil
}

func geminiDurationLiteral(duration time.Duration) string {
	if duration <= 0 {
		duration = time.Hour
	}

	return fmt.Sprintf("%.0fs", duration.Seconds())
}

func createGeminiExplicitCache(
	client *http.Client,
	apiKey string,
	model string,
	content string,
	ttl time.Duration,
	displayName string,
) (geminiCachedContentResponse, error) {
	var created geminiCachedContentResponse

	payload, err := json.Marshal(geminiCachedContentCreateRequest{
		Model:       geminiModelPath(model),
		DisplayName: displayName,
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: content}},
			},
		},
		TTL: geminiDurationLiteral(ttl),
	})
	if err != nil {
		return created, fmt.Errorf(
			"encode Gemini cachedContents.create request: %w",
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://generativelanguage.googleapis.com/v1beta/cachedContents",
		bytes.NewReader(payload),
	)
	if err != nil {
		return created, fmt.Errorf(
			"build Gemini cachedContents.create request: %w",
			err,
		)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goog-Api-Key", apiKey)

	response, err := client.Do(request)
	if err != nil {
		return created, fmt.Errorf("gemini cachedContents.create failed: %w", err)
	}

	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()

	if readErr != nil {
		return created, fmt.Errorf(
			"read Gemini cachedContents.create response: %w",
			readErr,
		)
	}

	if closeErr != nil {
		return created, fmt.Errorf(
			"close Gemini cachedContents.create response: %w",
			closeErr,
		)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return created, fmt.Errorf(
			"%w: %s: %s",
			errGeminiAPIResponse,
			response.Status,
			geminiAPIErrorMessage(body, response.Status),
		)
	}

	err = json.Unmarshal(body, &created)
	if err != nil {
		return created, fmt.Errorf(
			"parse Gemini cachedContents.create response: %w",
			err,
		)
	}

	if strings.TrimSpace(created.Name) == "" {
		return created, errGeminiCreateNoName
	}

	return created, nil
}

func ensureGeminiExplicitCache(
	client *http.Client,
	apiKey string,
	seed geminiExplicitCacheSeed,
	key string,
) (string, bool) {
	if !seed.Cache.APIEnabled || strings.TrimSpace(seed.Content) == "" {
		return "", false
	}

	cachedName, ok, err := readGeminiExplicitCache(seed.Cache, key)
	if err == nil && ok {
		return cachedName, true
	}

	created, err := createGeminiExplicitCache(
		client,
		apiKey,
		seed.Model,
		seed.Content,
		seed.Cache.APITTL,
		"coding-ethos-"+key[:12],
	)
	if err != nil {
		return "", false
	}

	expireTime := time.Now().UTC().Add(seed.Cache.APITTL)

	parsedExpireTime, err := time.Parse(time.RFC3339Nano, created.ExpireTime)
	if err == nil {
		expireTime = parsedExpireTime
	}

	err = writeGeminiExplicitCache(seed.Cache, key, created.Name, expireTime)
	if err != nil {
		writef(
			os.Stderr,
			"WARN: failed to persist Gemini explicit cache entry: %v\n",
			err,
		)
	}

	return created.Name, true
}

func generateGeminiText(
	client *http.Client,
	settings geminiRequestSettings,
	apiKey string,
	prompt string,
	responseDependency string,
	cachedContent string,
) (string, error) {
	cacheKey := geminiCacheKey(settings, prompt, responseDependency)

	cachedText, ok, err := readGeminiCache(settings.Cache, cacheKey)
	if err == nil && ok {
		return cachedText, nil
	}

	requestPayload := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: prompt}},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMIMEType: "application/json",
		},
		CachedContent:  cachedContent,
		ServiceTier:    settings.ServiceTier,
		SafetySettings: geminiSafetySettings(settings.DisableSafetyFilters),
	}
	if settings.ThinkingBudget != nil {
		requestPayload.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{
			ThinkingBudget: *settings.ThinkingBudget,
		}
	}

	payload, err := json.Marshal(requestPayload)
	if err != nil {
		return "", fmt.Errorf("encode Gemini request: %w", err)
	}

	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/%s:generateContent",
		geminiModelPath(settings.Model),
	)

	backoff := time.Duration(settings.InitialBackoffSeconds * float64(time.Second))
	if backoff <= 0 {
		backoff = time.Second
	}

	var lastErr error

	for attempt := 0; attempt <= settings.MaxRetries; attempt++ {
		text, retryable, requestErr := generateGeminiTextAttempt(
			client,
			apiKey,
			endpoint,
			payload,
			settings,
			cacheKey,
		)
		if requestErr == nil {
			return text, nil
		}

		lastErr = requestErr
		if !retryable {
			return "", lastErr
		}

		if attempt >= settings.MaxRetries {
			break
		}

		time.Sleep(backoff)
		backoff *= 2
	}

	return "", fmt.Errorf(
		"gemini request failed after %d attempts: %w",
		settings.MaxRetries+1,
		lastErr,
	)
}

func generateGeminiTextAttempt(
	client *http.Client,
	apiKey string,
	endpoint string,
	payload []byte,
	settings geminiRequestSettings,
	cacheKey string,
) (string, bool, error) {
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", false, fmt.Errorf("build Gemini request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goog-Api-Key", apiKey)

	response, err := client.Do(request)
	if err != nil {
		return "", true, fmt.Errorf("gemini request failed: %w", err)
	}

	defer func() {
		_ = response.Body.Close()
	}()

	return parseGeminiTextResponse(response, settings, cacheKey)
}

func parseGeminiTextResponse(
	response *http.Response,
	settings geminiRequestSettings,
	cacheKey string,
) (string, bool, error) {
	body, err := readGeminiResponseBody(response)
	if err != nil {
		return "", true, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		requestErr := fmt.Errorf(
			"%w: %s: %s",
			errGeminiAPIResponse,
			response.Status,
			geminiAPIErrorMessage(body, response.Status),
		)

		return "", isRetryableGeminiStatus(response.StatusCode), requestErr
	}

	text, err := decodeGeminiResponseText(body)
	if err != nil {
		return "", false, err
	}

	cacheErr := writeGeminiCache(settings.Cache, cacheKey, text)
	if cacheErr != nil {
		writef(
			os.Stderr,
			"WARN: failed to persist Gemini response cache: %v\n",
			cacheErr,
		)
	}

	return text, false, nil
}

func readGeminiResponseBody(response *http.Response) ([]byte, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read Gemini response: %w", err)
	}

	return body, nil
}

func decodeGeminiResponseText(body []byte) (string, error) {
	var parsed geminiGenerateResponse

	err := json.Unmarshal(body, &parsed)
	if err != nil {
		return "", fmt.Errorf("parse Gemini API response: %w", err)
	}

	text := extractGeminiText(parsed)
	if text == "" {
		return "", errGeminiAPINoText
	}

	return text, nil
}

func extractGeminiText(response geminiGenerateResponse) string {
	for _, candidate := range response.Candidates {
		parts := make([]string, 0, len(candidate.Content.Parts))
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, part.Text)
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}

	return ""
}

func stripGeminiCodeFence(text string) string {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimSuffix(cleaned, "```")

	return strings.TrimSpace(cleaned)
}

func parseGeminiResult(responseText string) (geminiResult, error) {
	var result geminiResult

	cleaned := stripGeminiCodeFence(responseText)

	if strings.HasPrefix(cleaned, "[") {
		var violations []geminiViolation

		err := json.Unmarshal([]byte(cleaned), &violations)
		if err != nil {
			return result, fmt.Errorf("parse Gemini JSON response: %w", err)
		}

		result.Violations = violations
	} else {
		err := json.Unmarshal([]byte(cleaned), &result)
		if err != nil {
			return result, fmt.Errorf("parse Gemini JSON response: %w", err)
		}
	}

	if result.Verdict == "" {
		result.Verdict = passVerdict
	}

	for index := range result.Violations {
		result.Violations[index].Severity = strings.ToUpper(
			strings.TrimSpace(result.Violations[index].Severity),
		)
		if result.Violations[index].Severity == "" {
			result.Violations[index].Severity = severityInfo
		}

		result.Violations[index].File = normalizeGeminiPath(
			result.Violations[index].File,
		)
		result.Violations[index].Message = strings.TrimSpace(
			result.Violations[index].Message,
		)

		result.Violations[index].EthosSection = strings.TrimSpace(
			result.Violations[index].EthosSection,
		)
		if result.Violations[index].Line < 0 {
			result.Violations[index].Line = 0
		}
	}

	return result, nil
}

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

func geminiGlobMatches(pattern string, candidate string) bool {
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
	for _, line := range strings.Split(diffOutput, "\n") {
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

func changedLinesForGeminiFile(path string, scope string) map[int]struct{} {
	var cmd *exec.Cmd

	switch scope {
	case "branch":
		cmd = exec.CommandContext(
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
		cmd = exec.CommandContext(
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

func isGeminiAddedOrUntracked(path string) bool {
	output, err := exec.CommandContext(
		context.Background(),
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
			appendGeminiViolationWithoutChangedLines(&filtered, violation)

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

	if isGeminiAddedOrUntracked(violation.File) {
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

		if cacheName, ok := ensureGeminiExplicitCache(client, apiKey, seeds[key], key); ok {
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
	outcomes []geminiCheckOutcome,
	changedLinesByFile map[string]map[int]struct{},
) {
	for outcomeIndex := range outcomes {
		outcomes[outcomeIndex].Filtered = filterGeminiViolationsByDiff(
			collectGeminiViolations(outcomes[outcomeIndex].Batches),
			changedLinesByFile,
		)
	}
}

func collectGeminiViolations(batches []geminiBatchOutcome) []geminiViolation {
	totalViolations := 0
	for _, batch := range batches {
		totalViolations += len(batch.Result.Violations)
	}

	allViolations := make([]geminiViolation, 0, totalViolations)
	for _, batch := range batches {
		allViolations = append(allViolations, batch.Result.Violations...)
	}

	return allViolations
}

func geminiOutcomeStatus(outcome geminiCheckOutcome) string {
	if outcome.Filtered.hasBlockingCriticals() {
		return statusFail
	}

	if outcome.BatchErrors > 0 && outcome.BatchesCompleted == 0 {
		return statusError
	}

	if outcome.BatchErrors > 0 {
		return statusWarn
	}

	for _, violation := range outcome.Filtered.InDiff {
		if violation.Severity == severityWarning {
			return statusWarn
		}
	}

	return passVerdict
}

func formatGeminiReport(
	scope string,
	outcomes []geminiCheckOutcome,
	format string,
) string {
	if !hasGeminiIssues(outcomes) {
		return ""
	}

	switch format {
	case hookOutputFormatJSON:
		return formatGeminiReportJSON(scope, outcomes)
	case hookOutputFormatTOON:
		return formatGeminiReportTOON(scope, outcomes)
	default:
		return formatGeminiReportHuman(scope, outcomes)
	}
}

func formatGeminiReportHuman(scope string, outcomes []geminiCheckOutcome) string {
	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		"GEMINI AI CODE CHECKS (GO)",
		strings.Repeat("=", reportDividerWidth),
		"Scope: " + scope,
		"",
	}

	for _, outcome := range outcomes {
		if shouldSkipGeminiOutcome(outcome) {
			continue
		}

		lines = appendGeminiOutcomeReport(lines, outcome)
	}

	lines = append(lines, strings.Repeat("=", reportDividerWidth))

	return strings.Join(lines, "\n")
}

func formatGeminiReportJSON(scope string, outcomes []geminiCheckOutcome) string {
	summary := geminiReportSummaryForOutcomes(
		scope,
		outcomes,
		hookOutputFormatJSON,
	)

	content, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return formatGeminiReportHuman(scope, outcomes)
	}

	return string(content)
}

func formatGeminiReportTOON(scope string, outcomes []geminiCheckOutcome) string {
	summary := geminiReportSummaryForOutcomes(
		scope,
		outcomes,
		hookOutputFormatTOON,
	)

	lines := []string{
		"format: " + summary.Format,
		"tool: gemini",
		"scope: " + toonCell(summary.Scope),
		"status: " + summary.Status,
		fmt.Sprintf("outcomes[%d]{name,status,model,service_tier,included_files,batches}:",
			len(summary.Outcomes)),
	}
	for _, outcome := range summary.Outcomes {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%s,%s,%d,%d",
				toonCell(outcome.Name),
				outcome.Status,
				toonCell(outcome.Model),
				toonCell(outcome.ServiceTier),
				outcome.IncludedFileCount,
				outcome.BatchCount,
			),
		)
	}

	violations := geminiReportViolations(summary.Outcomes)

	lines = append(
		lines,
		fmt.Sprintf(
			"violations[%d]{scope,severity,file,line,ethos_section,message}:",
			len(violations),
		),
	)
	for _, violation := range violations {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%s,%d,%s,%s",
				toonCell(violation.Scope),
				toonCell(violation.Severity),
				toonCell(violation.File),
				violation.Line,
				toonCell(violation.EthosSection),
				toonCell(violation.Message),
			),
		)
	}

	batchErrors := geminiReportBatchErrors(summary.Outcomes)
	if len(batchErrors) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("batch_errors[%d]{check,batch,files,error}:", len(batchErrors)),
		)
		for _, item := range batchErrors {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s,%d,%s,%s",
					toonCell(item.Check),
					item.Batch,
					toonCell(strings.Join(item.Files, " ")),
					toonCell(item.Error),
				),
			)
		}
	}

	return strings.Join(lines, "\n")
}

type geminiScopedViolation struct {
	Scope        string
	Severity     string
	File         string
	Message      string
	EthosSection string
	Line         int
}

type geminiScopedBatchError struct {
	Check string
	geminiBatchError
}

func geminiReportSummaryForOutcomes(
	scope string,
	outcomes []geminiCheckOutcome,
	format string,
) geminiReportSummary {
	summary := geminiReportSummary{
		Format: format,
		Scope:  scope,
		Status: passVerdict,
	}

	for _, outcome := range outcomes {
		if shouldSkipGeminiOutcome(outcome) {
			continue
		}

		status := geminiOutcomeStatus(outcome)
		switch {
		case status == statusFail:
			summary.Status = statusFail
		case status == statusError && summary.Status != statusFail:
			summary.Status = statusError
		case status == statusWarn && summary.Status == passVerdict:
			summary.Status = statusWarn
		}

		summary.Outcomes = append(summary.Outcomes, geminiOutcomeReport{
			Name:              outcome.Plan.Name,
			Status:            status,
			Model:             outcome.Plan.Model,
			ServiceTier:       outcome.Plan.ServiceTier,
			IncludedFileCount: len(outcome.Plan.IncludedFiles),
			BatchCount:        len(outcome.Plan.Batches),
			BatchErrors:       geminiBatchErrorsForOutcome(outcome),
			SkippedLargeFiles: outcome.Plan.SkippedLargeFiles,
			InDiff:            outcome.Filtered.InDiff,
			PreExisting:       outcome.Filtered.PreExisting,
		})
	}

	return summary
}

func geminiBatchErrorsForOutcome(outcome geminiCheckOutcome) []geminiBatchError {
	errors := []geminiBatchError{}

	for index, batch := range outcome.Batches {
		if batch.Error == "" {
			continue
		}

		errors = append(errors, geminiBatchError{
			Batch: index + 1,
			Files: batch.Files,
			Error: batch.Error,
		})
	}

	return errors
}

func geminiReportViolations(
	outcomes []geminiOutcomeReport,
) []geminiScopedViolation {
	violations := []geminiScopedViolation{}

	for _, outcome := range outcomes {
		for _, violation := range outcome.InDiff {
			violations = append(
				violations,
				geminiScopedViolation{
					Scope:        "in_diff",
					Severity:     violation.Severity,
					File:         violation.File,
					Message:      violation.Message,
					EthosSection: violation.EthosSection,
					Line:         violation.Line,
				},
			)
		}

		for _, violation := range outcome.PreExisting {
			violations = append(
				violations,
				geminiScopedViolation{
					Scope:        "pre_existing",
					Severity:     violation.Severity,
					File:         violation.File,
					Message:      violation.Message,
					EthosSection: violation.EthosSection,
					Line:         violation.Line,
				},
			)
		}
	}

	sort.SliceStable(violations, func(left int, right int) bool {
		if violations[left].File != violations[right].File {
			return violations[left].File < violations[right].File
		}

		if violations[left].Line != violations[right].Line {
			return violations[left].Line < violations[right].Line
		}

		return violations[left].Severity < violations[right].Severity
	})

	return violations
}

func geminiReportBatchErrors(
	outcomes []geminiOutcomeReport,
) []geminiScopedBatchError {
	errors := []geminiScopedBatchError{}

	for _, outcome := range outcomes {
		for _, batchError := range outcome.BatchErrors {
			errors = append(errors, geminiScopedBatchError{
				Check:            outcome.Name,
				geminiBatchError: batchError,
			})
		}
	}

	return errors
}

func hasGeminiIssues(outcomes []geminiCheckOutcome) bool {
	for _, outcome := range outcomes {
		if !shouldSkipGeminiOutcome(outcome) {
			return true
		}
	}

	return false
}

func shouldSkipGeminiOutcome(outcome geminiCheckOutcome) bool {
	status := geminiOutcomeStatus(outcome)

	return status == passVerdict && !outcome.Filtered.hasAnyInDiff()
}

func appendGeminiOutcomeReport(
	lines []string,
	outcome geminiCheckOutcome,
) []string {
	status := geminiOutcomeStatus(outcome)

	lines = append(lines, geminiOutcomeHeader(outcome, status))
	if len(outcome.Plan.SkippedLargeFiles) > 0 {
		lines = append(
			lines,
			"  Skipped large files: "+strings.Join(
				outcome.Plan.SkippedLargeFiles,
				", ",
			),
		)
	}

	lines = appendGeminiViolationSection(
		lines,
		"  [In your changes]",
		outcome.Filtered.InDiff,
	)
	if len(outcome.Filtered.PreExisting) > 0 {
		lines = appendGeminiViolationSection(
			lines,
			fmt.Sprintf("  [Pre-existing (%d)]", len(outcome.Filtered.PreExisting)),
			outcome.Filtered.PreExisting,
		)
	}

	lines = appendGeminiBatchErrors(lines, outcome.Batches)

	return append(lines, "")
}

func geminiOutcomeHeader(outcome geminiCheckOutcome, status string) string {
	return fmt.Sprintf(
		"%s: %s (model=%s, tier=%s, %d included file(s), %d batch(es))",
		outcome.Plan.Name,
		status,
		outcome.Plan.Model,
		outcome.Plan.ServiceTier,
		len(outcome.Plan.IncludedFiles),
		len(outcome.Plan.Batches),
	)
}

func appendGeminiViolationSection(
	lines []string,
	header string,
	violations []geminiViolation,
) []string {
	if len(violations) == 0 {
		return lines
	}

	lines = append(lines, header)
	for _, violation := range violations {
		lines = append(lines, geminiViolationLine(violation))
		if violation.EthosSection != "" {
			lines = append(lines, fmt.Sprintf("     (ETHOS %s)", violation.EthosSection))
		}
	}

	return lines
}

func geminiViolationLine(violation geminiViolation) string {
	lineLabel := "?"
	if violation.Line > 0 {
		lineLabel = strconv.Itoa(violation.Line)
	}

	return fmt.Sprintf(
		"  %s %s:%s %s",
		formatSeverityIcon(violation.Severity),
		violation.File,
		lineLabel,
		violation.Message,
	)
}

func appendGeminiBatchErrors(
	lines []string,
	batches []geminiBatchOutcome,
) []string {
	for index, batch := range batches {
		if batch.Error != "" {
			lines = append(
				lines,
				fmt.Sprintf(
					"  !! Batch %d (%s): %s",
					index+1,
					strings.Join(batch.Files, ", "),
					batch.Error,
				),
			)
		}
	}

	return lines
}

func formatSeverityIcon(severity string) string {
	switch severity {
	case severityCritical:
		return "XX"
	case severityWarning:
		return "W "
	default:
		return "--"
	}
}

// The Go Gemini runner owns prompt-pack loading, file selection, model/service-tier
// resolution, concurrent batch execution, repo-local response caching, modal
// allowlist filtering, diff-aware reporting, and raw API transport.
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

	outcomes := executeGeminiChecks(settings, apiKey, prepared, changedLinesByFile)
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

func summarizeGeminiOutcomes(outcomes []geminiCheckOutcome) (bool, bool, bool) {
	hasErrors := false
	hasCriticals := false
	hasAnyInDiff := false

	for _, outcome := range outcomes {
		switch geminiOutcomeStatus(outcome) {
		case "ERROR":
			hasErrors = true
		case "FAIL":
			hasCriticals = true
		}

		if outcome.Filtered.hasAnyInDiff() {
			hasAnyInDiff = true
		}
	}

	return hasErrors, hasCriticals, hasAnyInDiff
}

func existingFiles(paths []string) []string {
	files := make([]string, 0, len(paths))

	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}

		if _, ok := seen[path]; ok {
			continue
		}

		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			seen[path] = struct{}{}
			files = append(files, path)
		}
	}

	return files
}

func readText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) || bytes.Contains(data, []byte{0}) {
		return "", true, nil
	}

	return string(data), false, nil
}

func fixText(_ Config, paths []string) int {
	failed := false

	for _, path := range existingFiles(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)

			failed = true

			continue
		}

		if !utf8.Valid(data) || bytes.Contains(data, []byte{0}) {
			continue
		}

		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")

		parts := strings.Split(text, "\n")
		for i, line := range parts {
			parts[i] = strings.TrimRight(line, " \t")
		}

		fixed := strings.TrimRight(strings.Join(parts, "\n"), "\n")
		if fixed != "" {
			fixed += "\n"
		}

		if fixed != string(data) {
			err := os.WriteFile(path, []byte(fixed), hookRewriteFilePerm)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)

				failed = true
			}
		}
	}

	return exitCode(failed)
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}

	return result
}

func hasPrefix(path string, prefixes []string) bool {
	normalized := filepath.ToSlash(path)
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	return false
}

func exitCode(failed bool) int {
	if failed {
		return 1
	}

	return 0
}
