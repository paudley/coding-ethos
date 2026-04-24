package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

const (
	configEnv         = "CODE_ETHOS_PRECOMMIT_CONFIG"
	precommitRootEnv  = "CODE_ETHOS_PRECOMMIT_ROOT"
	privateKeyPattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	textChunkSize     = 8192
)

type Config struct {
	CommitLint struct {
		AllowedTypes    []string `json:"allowed_types"`
		IgnoredPrefixes []string `json:"ignored_prefixes"`
		MaxHeaderLength int      `json:"max_header_length"`
	} `json:"commitlint"`
	CommitAttribution struct {
		BlockedNames []string `json:"blocked_names"`
	} `json:"commit_attribution"`
	Text struct {
		ForbiddenStrings         []string `json:"forbidden_strings"`
		LargeFileExcludePrefixes []string `json:"large_file_exclude_prefixes"`
		LargeFileSuffixes        []string `json:"large_file_suffixes"`
		MaxLargeFileKB           int      `json:"max_large_file_kb"`
	} `json:"text"`
	LineLimits struct {
		PythonHard int `json:"python_hard"`
		PythonWarn int `json:"python_warn"`
		ShellHard  int `json:"shell_hard"`
		ShellWarn  int `json:"shell_warn"`
	} `json:"line_limits"`
	Shell struct {
		RequireCommonForPrefixes []string `json:"require_common_for_prefixes"`
	} `json:"shell"`
	QuietFilter QuietFilterConfig `json:"quiet_filter"`
}

type GeminiSettings struct {
	Enabled                 bool                `json:"enabled"`
	Model                   string              `json:"model"`
	ModelOverrides          map[string]string   `json:"model_overrides"`
	ServiceTier             string              `json:"service_tier"`
	ServiceTierOverrides    map[string]string   `json:"service_tier_overrides"`
	ThinkingBudget          *int                `json:"thinking_budget"`
	ThinkingBudgetOverrides map[string]int      `json:"thinking_budget_overrides"`
	MaxRetries              int                 `json:"max_retries"`
	TimeoutSeconds          int                 `json:"timeout_seconds"`
	InitialBackoffSeconds   float64             `json:"initial_backoff_seconds"`
	MaxConcurrentAPICalls   int                 `json:"max_concurrent_api_calls"`
	ModalAllowlistFiles     []string            `json:"modal_allowlist_files"`
	DisableSafetyFilters    bool                `json:"disable_safety_filters"`
	Cache                   GeminiCacheSettings `json:"cache"`
}

type GeminiCacheSettings struct {
	Enabled       bool   `json:"enabled"`
	TTLSeconds    int    `json:"ttl_seconds"`
	Dirname       string `json:"dirname"`
	APIEnabled    bool   `json:"api_enabled"`
	APITTLSeconds int    `json:"api_ttl_seconds"`
}

type QuietFilterConfig struct {
	ANSIRegex        string   `json:"ansi_regex"`
	BannerWidth      int      `json:"banner_width"`
	FailedRegex      string   `json:"failed_regex"`
	MetadataPrefixes []string `json:"metadata_prefixes"`
	PassedRegex      string   `json:"passed_regex"`
	PreexistingRegex string   `json:"preexisting_regex"`
	SeparatorRegex   string   `json:"separator_regex"`
	SkippedRegex     string   `json:"skipped_regex"`
	StatusRegex      string   `json:"status_regex"`
	SuppressExact    []string `json:"suppress_exact"`
	SuppressPrefixes []string `json:"suppress_prefixes"`
	SuppressRegexes  []string `json:"suppress_regexes"`
}

type GeminiPromptCheckSpec struct {
	FileScope     string             `json:"file_scope"`
	BatchSize     int                `json:"batch_size"`
	MaxFileSizeKB int                `json:"max_file_size_kb"`
	Selector      GeminiFileSelector `json:"selector"`
}

type GeminiFileSelector struct {
	IncludeExtensions           []string `json:"include_extensions"`
	ExcludeSubstrings           []string `json:"exclude_substrings"`
	ExcludePrefixes             []string `json:"exclude_prefixes"`
	AllowExtensionlessInScripts bool     `json:"allow_extensionless_in_scripts"`
	ShebangMarkers              []string `json:"shebang_markers"`
}

type GeminiPromptPack struct {
	Version int                              `json:"version"`
	Checks  map[string]GeminiPromptCheckSpec `json:"checks"`
	Prompts map[string]string                `json:"prompts"`
}

type CommandFunc func(Config, []string) int

func main() {
	if os.Getenv("LEFTHOOK") == "0" {
		os.Exit(0)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	commands := map[string]CommandFunc{
		"check-forbidden-strings":    checkForbiddenStrings,
		"check-large-files":          checkLargeFiles,
		"check-line-limits":          checkLineLimits,
		"check-merge-conflict":       checkMergeConflict,
		"check-shebangs":             checkShebangs,
		"check-shell-best-practices": checkShellBestPractices,
		"check-syntax":               checkSyntax,
		"commit-attribution":         checkCommitAttribution,
		"commitlint":                 checkCommitLint,
		"detect-private-key":         detectPrivateKey,
		"fix-text":                   fixText,
		"gemini-check":               runGeminiCheck,
		"quiet-filter":               quietFilter,
		"shellcheck":                 runShellcheck,
	}
	command, ok := commands[os.Args[1]]
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
}

type compiledQuietFilter struct {
	ansi             *regexp.Regexp
	failed           *regexp.Regexp
	metadataPrefixes []string
	passed           *regexp.Regexp
	preexisting      *regexp.Regexp
	separator        *regexp.Regexp
	skipped          *regexp.Regexp
	status           *regexp.Regexp
	suppressExact    map[string]bool
	suppressPrefixes []string
	suppressRegexes  []*regexp.Regexp
	bannerWidth      int
}

func quietFilter(cfg Config, _ []string) int {
	filter, err := compileQuietFilter(cfg.QuietFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	return runQuietFilter(filter, os.Stdin, os.Stdout)
}

func compileQuietFilter(cfg QuietFilterConfig) (compiledQuietFilter, error) {
	filter := compiledQuietFilter{
		bannerWidth:      cfg.BannerWidth,
		metadataPrefixes: cfg.MetadataPrefixes,
		suppressExact:    stringSet(cfg.SuppressExact),
		suppressPrefixes: cfg.SuppressPrefixes,
	}
	if filter.bannerWidth == 0 {
		filter.bannerWidth = 70
	}

	var err error
	if filter.ansi, err = compileConfiguredRegex("quiet_filter.ansi_regex", cfg.ANSIRegex); err != nil {
		return filter, err
	}
	if filter.passed, err = compileConfiguredRegex("quiet_filter.passed_regex", cfg.PassedRegex); err != nil {
		return filter, err
	}
	if filter.skipped, err = compileConfiguredRegex("quiet_filter.skipped_regex", cfg.SkippedRegex); err != nil {
		return filter, err
	}
	if filter.failed, err = compileConfiguredRegex("quiet_filter.failed_regex", cfg.FailedRegex); err != nil {
		return filter, err
	}
	if filter.status, err = compileConfiguredRegex("quiet_filter.status_regex", cfg.StatusRegex); err != nil {
		return filter, err
	}
	if filter.preexisting, err = compileConfiguredRegex("quiet_filter.preexisting_regex", cfg.PreexistingRegex); err != nil {
		return filter, err
	}
	if filter.separator, err = compileConfiguredRegex("quiet_filter.separator_regex", cfg.SeparatorRegex); err != nil {
		return filter, err
	}
	for i, pattern := range cfg.SuppressRegexes {
		compiled, err := compileConfiguredRegex(fmt.Sprintf("quiet_filter.suppress_regexes[%d]", i), pattern)
		if err != nil {
			return filter, err
		}
		filter.suppressRegexes = append(filter.suppressRegexes, compiled)
	}
	return filter, nil
}

func compileConfiguredRegex(name string, pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return regexp.Compile(`a^`)
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return compiled, nil
}

func runQuietFilter(filter compiledQuietFilter, input io.Reader, output io.Writer) int {
	passed := 0
	skipped := 0
	failed := 0
	seenBanners := map[string]bool{}
	suppressHowToFix := false
	suppressBannerContent := false
	lastWasSeparator := false
	lastWasBlank := false
	suppressMeta := false
	suppressPreexisting := false

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		clean := filter.ansi.ReplaceAllString(line, "")

		if filter.passed.MatchString(clean) {
			passed++
			suppressMeta = true
			continue
		}
		if filter.skipped.MatchString(clean) {
			skipped++
			suppressMeta = true
			continue
		}
		if filter.failed.MatchString(clean) {
			failed++
		}

		if suppressMeta {
			if clean == "" || hasPrefix(clean, filter.metadataPrefixes) {
				continue
			}
			suppressMeta = false
		}

		if shouldSuppressQuietLine(filter, clean) {
			continue
		}

		if filter.preexisting.MatchString(clean) {
			suppressPreexisting = true
			continue
		}
		if suppressPreexisting {
			if strings.HasPrefix(clean, " ") || clean == "" {
				continue
			}
			suppressPreexisting = false
		}

		if filter.separator.MatchString(clean) {
			lastWasSeparator = true
			continue
		}

		if lastWasSeparator && clean != "" {
			lastWasSeparator = false
			if !strings.HasPrefix(clean, "-") && !startsWithDigit(clean) {
				if seenBanners[clean] {
					suppressBannerContent = true
					continue
				}
				seenBanners[clean] = true
				if clean == "How to fix:" {
					seenBanners["howtofix"] = true
				}
				fmt.Fprintln(output, strings.Repeat("=", filter.bannerWidth))
				fmt.Fprintln(output, line)
				fmt.Fprintln(output, strings.Repeat("=", filter.bannerWidth))
				continue
			}
			fmt.Fprintln(output, strings.Repeat("=", filter.bannerWidth))
			fmt.Fprintln(output, line)
			continue
		}
		lastWasSeparator = false

		if suppressBannerContent {
			if filter.status.MatchString(clean) {
				suppressBannerContent = false
			} else {
				continue
			}
		}

		if clean == "How to fix:" {
			if !seenBanners["howtofix"] {
				seenBanners["howtofix"] = true
				fmt.Fprintln(output, line)
				suppressHowToFix = false
			} else {
				suppressHowToFix = true
			}
			continue
		}
		if suppressHowToFix {
			if strings.HasPrefix(clean, " ") || clean == "" {
				continue
			}
			suppressHowToFix = false
		}

		if clean == "" {
			if lastWasBlank {
				continue
			}
			lastWasBlank = true
		} else {
			lastWasBlank = false
		}
		fmt.Fprintln(output, line)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "quiet-filter: %v\n", err)
		return 1
	}

	if failed > 0 {
		var parts []string
		if passed > 0 {
			parts = append(parts, fmt.Sprintf("\033[32m%d passed\033[0m", passed))
		}
		parts = append(parts, fmt.Sprintf("\033[31m%d failed\033[0m", failed))
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("\033[33m%d skipped\033[0m", skipped))
		}
		fmt.Fprintf(output, "  (%s)\n", strings.Join(parts, ", "))
	}
	return 0
}

func shouldSuppressQuietLine(filter compiledQuietFilter, clean string) bool {
	if filter.suppressExact[clean] {
		return true
	}
	if hasPrefix(clean, filter.suppressPrefixes) {
		return true
	}
	for _, pattern := range filter.suppressRegexes {
		if pattern.MatchString(clean) {
			return true
		}
	}
	return false
}

func startsWithDigit(value string) bool {
	return value != "" && value[0] >= '0' && value[0] <= '9'
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
	data, err := json.Marshal(goConfig)
	if err != nil {
		return cfg, fmt.Errorf("encode go config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse go config: %w", err)
	}
	return cfg, nil
}

func loadGeminiSettings() (GeminiSettings, geminiRuntimePaths, error) {
	var settings GeminiSettings
	var paths geminiRuntimePaths
	bundleRoot, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, paths, err
	}
	paths.BundleRoot = bundleRoot
	paths.ConsumerRoot = consumerRoot(filepath.Dir(bundleRoot))
	geminiConfig, ok := rootConfig["gemini"]
	if !ok {
		paths.CacheDir = filepath.Join(
			gitCommonDir(paths.ConsumerRoot),
			bundleLocalBinDirname(rootConfig),
			"gemini-cache",
		)
		return settings, paths, nil
	}
	data, err := json.Marshal(geminiConfig)
	if err != nil {
		return settings, paths, fmt.Errorf("encode gemini config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, paths, fmt.Errorf("parse gemini config: %w", err)
	}
	if settings.Model == "" {
		settings.Model = "gemini-2.5-flash"
	}
	serviceTier, err := normalizeGeminiServiceTier(settings.ServiceTier)
	if err != nil {
		return settings, paths, fmt.Errorf("gemini.service_tier: %w", err)
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
	for checkName, tier := range settings.ServiceTierOverrides {
		normalized, err := normalizeGeminiServiceTier(tier)
		if err != nil {
			return settings, paths, fmt.Errorf("gemini.service_tier_overrides.%s: %w", checkName, err)
		}
		settings.ServiceTierOverrides[checkName] = normalized
	}
	paths.CacheDir = filepath.Join(
		gitCommonDir(paths.ConsumerRoot),
		bundleLocalBinDirname(rootConfig),
		settings.Cache.Dirname,
	)
	return settings, paths, nil
}

func loadMergedRootConfig() (string, map[string]any, error) {
	bundleRoot, err := findBundleRoot()
	if err != nil {
		return "", nil, err
	}
	rootConfig, err := loadYAMLMap(filepath.Join(filepath.Dir(bundleRoot), "config.yaml"))
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

	for _, candidate := range overrideCandidates(consumerRoot(filepath.Dir(bundleRoot)), rootConfig) {
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
	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func repoRoot() string {
	if root := gitOutput("rev-parse", "--show-toplevel"); root != "" {
		return root
	}
	return "."
}

func consumerRoot(ethosRoot string) string {
	if root := gitOutput("-C", ethosRoot, "rev-parse", "--show-superproject-working-tree"); root != "" {
		return root
	}
	if root := gitOutput("-C", ethosRoot, "rev-parse", "--show-toplevel"); root != "" {
		return root
	}
	return ethosRoot
}

func gitCommonDir(root string) string {
	if dir := gitOutput("-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir"); dir != "" {
		return dir
	}
	return filepath.Join(root, ".git")
}

func bundleLocalBinDirname(rootConfig map[string]any) string {
	if bundle, ok := rootConfig["bundle"].(map[string]any); ok {
		name := strings.TrimSpace(fmt.Sprint(bundle["local_bin_dirname"]))
		if name != "" && name != "<nil>" {
			return name
		}
	}
	return "coding-ethos-hooks"
}

func isBundleRoot(path string) bool {
	info, err := os.Stat(filepath.Join(path, "lefthook.yml"))
	if err != nil || info.IsDir() {
		return false
	}
	hooks, err := os.Stat(filepath.Join(path, "hooks"))
	return err == nil && hooks.IsDir()
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

	return "", fmt.Errorf("could not locate pre-commit bundle from %s", root)
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
		if err := json.Unmarshal(data, &pack); err != nil {
			return pack, fmt.Errorf("parse %s: %w", candidate, err)
		}
		if len(pack.Prompts) == 0 {
			return pack, fmt.Errorf("%s: prompt pack missing prompts", candidate)
		}
		if len(pack.Checks) == 0 {
			return pack, fmt.Errorf("%s: prompt pack missing checks", candidate)
		}
		return pack, nil
	}
	return pack, fmt.Errorf("could not locate Gemini prompt pack from %s", bundleRoot)
}

type GeminiCLIOptions struct {
	DryRun    bool
	FullCheck bool
	CheckType string
	Files     []string
}

type GeminiBatchPlan struct {
	Files []string `json:"files"`
}

type GeminiCheckPlan struct {
	Name              string            `json:"name"`
	FileScope         string            `json:"file_scope"`
	Model             string            `json:"model"`
	ServiceTier       string            `json:"service_tier"`
	ThinkingBudget    *int              `json:"thinking_budget,omitempty"`
	CacheEnabled      bool              `json:"cache_enabled"`
	SelectedFiles     []string          `json:"selected_files"`
	IncludedFiles     []string          `json:"included_files"`
	SkippedLargeFiles []string          `json:"skipped_large_files"`
	BatchSize         int               `json:"batch_size"`
	MaxFileSizeKB     int               `json:"max_file_size_kb"`
	Batches           []GeminiBatchPlan `json:"batches"`
}

type GeminiExecutionPlan struct {
	Scope  string            `json:"scope"`
	DryRun bool              `json:"dry_run"`
	Checks []GeminiCheckPlan `json:"checks"`
}

type geminiPreparedBatch struct {
	Files          []string
	Prompt         string
	CachedPrompt   string
	Content        string
	ExplicitAPIKey string
}

type geminiPreparedCheck struct {
	Plan    GeminiCheckPlan
	Prompt  string
	Request geminiRequestSettings
	Batches []geminiPreparedBatch
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	CachedContent    string                 `json:"cachedContent,omitempty"`
	SafetySettings   []geminiSafetySetting  `json:"safetySettings,omitempty"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig,omitempty"`
	ServiceTier      string                 `json:"serviceTier,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerationConfig struct {
	ResponseMIMEType string                `json:"responseMimeType,omitempty"`
	ThinkingConfig   *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback map[string]any `json:"promptFeedback"`
}

type geminiCachedContentCreateRequest struct {
	Model       string          `json:"model"`
	DisplayName string          `json:"displayName,omitempty"`
	Contents    []geminiContent `json:"contents,omitempty"`
	TTL         string          `json:"ttl,omitempty"`
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
	Line         int    `json:"line"`
	Message      string `json:"message"`
	EthosSection string `json:"ethos_section"`
}

type geminiBatchOutcome struct {
	Files  []string     `json:"files"`
	Result geminiResult `json:"result"`
	Error  string       `json:"error,omitempty"`
}

type geminiFilteredViolations struct {
	InDiff      []geminiViolation `json:"in_diff"`
	PreExisting []geminiViolation `json:"pre_existing"`
}

type geminiCheckOutcome struct {
	Plan             GeminiCheckPlan          `json:"plan"`
	Batches          []geminiBatchOutcome     `json:"batches"`
	Filtered         geminiFilteredViolations `json:"filtered"`
	BatchErrors      int                      `json:"batch_errors"`
	BatchesCompleted int                      `json:"batches_completed"`
}

type geminiRuntimePaths struct {
	BundleRoot   string
	ConsumerRoot string
	CacheDir     string
}

type geminiRequestSettings struct {
	CheckName             string
	Model                 string
	ServiceTier           string
	ThinkingBudget        *int
	MaxRetries            int
	InitialBackoffSeconds float64
	DisableSafetyFilters  bool
	Cache                 geminiResponseCache
}

type geminiResponseCache struct {
	Enabled    bool
	Dir        string
	TTL        time.Duration
	APIEnabled bool
	APITTL     time.Duration
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

func parseGeminiCLIOptions(args []string) (GeminiCLIOptions, error) {
	options := GeminiCLIOptions{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dry-run":
			options.DryRun = true
		case arg == "--full-check":
			options.FullCheck = true
		case arg == "--check-type":
			if i+1 >= len(args) {
				return options, fmt.Errorf("--check-type requires a value")
			}
			i++
			options.CheckType = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--check-type="):
			options.CheckType = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
		case strings.HasPrefix(arg, "--"):
			return options, fmt.Errorf("unknown flag: %s", arg)
		default:
			options.Files = append(options.Files, arg)
		}
	}
	return options, nil
}

func checkNamesFromPromptPack(pack GeminiPromptPack, checkType string) ([]string, error) {
	names := make([]string, 0, len(pack.Checks))
	for name := range pack.Checks {
		names = append(names, name)
	}
	sort.Strings(names)
	if checkType == "" {
		return names, nil
	}
	if _, ok := pack.Checks[checkType]; !ok {
		return nil, fmt.Errorf("unknown Gemini check type: %s", checkType)
	}
	return []string{checkType}, nil
}

func normalizeGeminiPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "./")
}

func matchesGeminiSelector(path string, selector GeminiFileSelector) (bool, error) {
	normalized := normalizeGeminiPath(path)
	for _, pattern := range selector.ExcludeSubstrings {
		if pattern != "" && strings.Contains(normalized, pattern) {
			return false, nil
		}
	}
	for _, pattern := range selector.ExcludePrefixes {
		if pattern != "" && strings.HasPrefix(normalized, pattern) {
			return false, nil
		}
	}
	ext := strings.ToLower(filepath.Ext(normalized))
	for _, candidate := range selector.IncludeExtensions {
		if ext == strings.ToLower(candidate) {
			return true, nil
		}
	}
	if selector.AllowExtensionlessInScripts && ext == "" {
		if strings.Contains(normalized, "scripts/") || strings.Contains(normalized, "scripts\\") {
			return true, nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false, err
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")
	if !strings.HasPrefix(firstLine, "#!") {
		return false, nil
	}
	for _, marker := range selector.ShebangMarkers {
		if marker != "" && strings.Contains(strings.ToLower(firstLine), strings.ToLower(marker)) {
			return true, nil
		}
	}
	return false, nil
}

func unionGeminiFileFilter(paths []string, checks map[string]GeminiPromptCheckSpec, names []string) ([]string, error) {
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
	cmd := exec.Command("git", "diff", "--name-only", "origin/main...HEAD")
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

func candidateFilesForGemini(options GeminiCLIOptions, pack GeminiPromptPack) ([]string, string, error) {
	checkNames, err := checkNamesFromPromptPack(pack, options.CheckType)
	if err != nil {
		return nil, "", err
	}
	var candidates []string
	scope := "staged"
	if options.FullCheck {
		scope = "branch"
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
		requestSettings, err := resolveGeminiRequestSettings(settings, name, cacheDir)
		if err != nil {
			return nil, err
		}
		spec := pack.Checks[name]
		if spec.BatchSize <= 0 {
			spec.BatchSize = 1
		}
		if spec.MaxFileSizeKB <= 0 {
			spec.MaxFileSizeKB = 100
		}
		promptTemplate := pack.Prompts[name]
		selected := make([]string, 0, len(files))
		included := make([]string, 0, len(files))
		skippedLarge := make([]string, 0)
		formattedContents := make([]string, 0, len(files))

		for _, path := range files {
			matches, err := matchesGeminiSelector(path, spec.Selector)
			if err != nil {
				return nil, err
			}
			if !matches {
				continue
			}
			selected = append(selected, path)
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if info.Size() > int64(spec.MaxFileSizeKB*1024) {
				skippedLarge = append(skippedLarge, path)
				continue
			}
			text, binary, err := readText(path)
			if err != nil {
				return nil, err
			}
			if binary {
				continue
			}
			included = append(included, path)
			formattedContents = append(formattedContents, fmt.Sprintf("--- %s ---\n%s\n", path, text))
		}

		batchPlans := make([]GeminiBatchPlan, 0)
		batches := make([]geminiPreparedBatch, 0)
		for i := 0; i < len(formattedContents); i += spec.BatchSize {
			end := i + spec.BatchSize
			if end > len(formattedContents) {
				end = len(formattedContents)
			}
			batchFiles := append([]string{}, included[i:end]...)
			batchContent := strings.Join(formattedContents[i:end], "\n")
			batchPrompt := geminiPromptWithInlineContent(promptTemplate, batchContent)
			batches = append(batches, geminiPreparedBatch{
				Files:          batchFiles,
				Prompt:         batchPrompt,
				CachedPrompt:   geminiPromptForExplicitCachedContent(promptTemplate),
				Content:        batchContent,
				ExplicitAPIKey: geminiExplicitContentKey(requestSettings.Model, batchContent),
			})
			batchPlans = append(batchPlans, GeminiBatchPlan{Files: batchFiles})
		}

		prepared = append(prepared, geminiPreparedCheck{
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
		})
	}
	return prepared, nil
}

func geminiAPIKey() string {
	return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
}

func normalizeGeminiServiceTier(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == "unspecified" {
		return "standard", nil
	}
	switch normalized {
	case "standard", "flex", "priority":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported service tier %q", value)
	}
}

func resolveGeminiRequestSettings(
	settings GeminiSettings,
	checkName string,
	cacheDir string,
) (geminiRequestSettings, error) {
	model := strings.TrimSpace(settings.Model)
	if override := strings.TrimSpace(settings.ModelOverrides[checkName]); override != "" {
		model = override
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}

	serviceTier := settings.ServiceTier
	if override, ok := settings.ServiceTierOverrides[checkName]; ok {
		serviceTier = override
	}
	if serviceTier == "" {
		serviceTier = "standard"
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
	}, nil
}

func geminiModelPath(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gemini-2.5-flash"
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
	if err := json.Unmarshal(body, &apiError); err == nil {
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
		"The source corpus to review is provided as cached content attached to this request. " +
			"Analyze that cached file corpus directly; do not ask for it again.",
	)
	if strings.Contains(template, "{code_content}") {
		return strings.Replace(template, "{code_content}", replacement, 1)
	}
	return strings.TrimSpace(template) + "\n\n" + replacement
}

func geminiExplicitContentKey(model string, content string) string {
	sum := sha256.Sum256([]byte(geminiModelPath(model) + "\x00" + content))
	return fmt.Sprintf("%x", sum)
}

func geminiCacheKey(settings geminiRequestSettings, prompt string, dependency string) string {
	thinkingBudget := "unset"
	if settings.ThinkingBudget != nil {
		thinkingBudget = fmt.Sprintf("%d", *settings.ThinkingBudget)
	}
	payload := strings.Join(
		[]string{
			"v1",
			settings.CheckName,
			settings.Model,
			settings.ServiceTier,
			thinkingBudget,
			fmt.Sprintf("%t", settings.DisableSafetyFilters),
			dependency,
			prompt,
		},
		"\x00",
	)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum)
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
	data, err := os.ReadFile(geminiCachePath(cache, key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var entry geminiCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", false, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	if err != nil {
		return "", false, err
	}
	if cache.TTL > 0 && time.Since(createdAt) > cache.TTL {
		_ = os.Remove(geminiCachePath(cache, key))
		return "", false, nil
	}
	return entry.Text, true, nil
}

func writeGeminiCache(cache geminiResponseCache, key string, text string) error {
	if !cache.Enabled {
		return nil
	}
	if err := os.MkdirAll(cache.Dir, 0o755); err != nil {
		return err
	}
	entry := geminiCacheEntry{
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Text:      text,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := geminiCachePath(cache, key)
	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func readGeminiExplicitCache(cache geminiResponseCache, key string) (string, bool, error) {
	if !cache.APIEnabled {
		return "", false, nil
	}
	data, err := os.ReadFile(geminiExplicitCachePath(cache, key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var entry geminiExplicitCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(entry.Name) == "" {
		return "", false, nil
	}
	expireTime, err := time.Parse(time.RFC3339Nano, entry.ExpireTime)
	if err != nil {
		return "", false, err
	}
	if time.Now().UTC().After(expireTime) {
		_ = os.Remove(geminiExplicitCachePath(cache, key))
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := geminiExplicitCacheEntry{
		Name:       name,
		ExpireTime: expireTime.UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
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
		return created, fmt.Errorf("encode Gemini cachedContents.create request: %w", err)
	}

	request, err := http.NewRequest(
		http.MethodPost,
		"https://generativelanguage.googleapis.com/v1beta/cachedContents",
		bytes.NewReader(payload),
	)
	if err != nil {
		return created, fmt.Errorf("build Gemini cachedContents.create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", apiKey)

	response, err := client.Do(request)
	if err != nil {
		return created, fmt.Errorf("Gemini cachedContents.create failed: %w", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return created, fmt.Errorf("read Gemini cachedContents.create response: %w", readErr)
	}
	if closeErr != nil {
		return created, fmt.Errorf("close Gemini cachedContents.create response: %w", closeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return created, fmt.Errorf(
			"Gemini cachedContents.create returned %s: %s",
			response.Status,
			geminiAPIErrorMessage(body, response.Status),
		)
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return created, fmt.Errorf("parse Gemini cachedContents.create response: %w", err)
	}
	if strings.TrimSpace(created.Name) == "" {
		return created, fmt.Errorf("Gemini cachedContents.create returned no cache name")
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
	if cachedName, ok, err := readGeminiExplicitCache(seed.Cache, key); err == nil && ok {
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
	if parsed, err := time.Parse(time.RFC3339Nano, created.ExpireTime); err == nil {
		expireTime = parsed
	}
	_ = writeGeminiExplicitCache(seed.Cache, key, created.Name, expireTime)
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
	if cachedText, ok, err := readGeminiCache(settings.Cache, cacheKey); err == nil && ok {
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
		request, err := http.NewRequest(
			http.MethodPost,
			endpoint,
			bytes.NewReader(payload),
		)
		if err != nil {
			return "", fmt.Errorf("build Gemini request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("x-goog-api-key", apiKey)

		response, err := client.Do(request)
		if err != nil {
			lastErr = fmt.Errorf("Gemini request failed: %w", err)
		} else {
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("read Gemini response: %w", readErr)
			} else if closeErr != nil {
				lastErr = fmt.Errorf("close Gemini response: %w", closeErr)
			} else if response.StatusCode < 200 || response.StatusCode >= 300 {
				lastErr = fmt.Errorf(
					"Gemini API returned %s: %s",
					response.Status,
					geminiAPIErrorMessage(body, response.Status),
				)
				if !isRetryableGeminiStatus(response.StatusCode) {
					return "", lastErr
				}
			} else {
				var parsed geminiGenerateResponse
				if err := json.Unmarshal(body, &parsed); err != nil {
					return "", fmt.Errorf("parse Gemini API response: %w", err)
				}
				text := extractGeminiText(parsed)
				if text == "" {
					return "", fmt.Errorf("Gemini API returned no candidate text")
				}
				_ = writeGeminiCache(settings.Cache, cacheKey, text)
				return text, nil
			}
		}

		if attempt >= settings.MaxRetries {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}

	return "", fmt.Errorf(
		"Gemini request failed after %d attempts: %w",
		settings.MaxRetries+1,
		lastErr,
	)
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
	if err := json.Unmarshal([]byte(stripGeminiCodeFence(responseText)), &result); err != nil {
		return result, fmt.Errorf("parse Gemini JSON response: %w", err)
	}
	if result.Verdict == "" {
		result.Verdict = "PASS"
	}
	for index := range result.Violations {
		result.Violations[index].Severity = strings.ToUpper(
			strings.TrimSpace(result.Violations[index].Severity),
		)
		if result.Violations[index].Severity == "" {
			result.Violations[index].Severity = "INFO"
		}
		result.Violations[index].File = normalizeGeminiPath(result.Violations[index].File)
		result.Violations[index].Message = strings.TrimSpace(result.Violations[index].Message)
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
	modalSection := strings.Contains(text, "section 19") ||
		strings.Contains(text, "one path for critical operations") ||
		strings.Contains(text, "sections 5+7+19") ||
		strings.Contains(text, "no optional internal state for capabilities") ||
		strings.Contains(text, "section 7") ||
		strings.Contains(text, "if available")
	modalShape := strings.Contains(text, "modal") ||
		strings.Contains(text, "gates the") ||
		strings.Contains(text, "gates ") ||
		strings.Contains(text, "gating feature enablement") ||
		strings.Contains(text, "conditionally disables") ||
		strings.Contains(text, "conditional execution paths") ||
		strings.Contains(text, "different execution paths") ||
		strings.Contains(text, "based on a configuration field") ||
		strings.Contains(text, "based on an input type") ||
		strings.Contains(text, "via configuration") ||
		strings.Contains(text, "enabled/disabled") ||
		strings.Contains(text, "silently degrade") ||
		strings.Contains(text, "silent degradation") ||
		strings.Contains(text, "skipping the") ||
		strings.Contains(text, "skip the") ||
		strings.Contains(text, "full job")
	nonModalSection7 := strings.Contains(text, "section 7") &&
		!modalShape &&
		!strings.Contains(text, "sections 5+7+19") &&
		!strings.Contains(text, "if available")
	return modalSection && modalShape && !nonModalSection7
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
		start := 0
		fmt.Sscanf(match[1], "%d", &start)
		count := 1
		if match[2] != "" {
			fmt.Sscanf(match[2], "%d", &count)
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
		cmd = exec.Command(
			"git",
			"diff",
			"--no-ext-diff",
			"-U0",
			"origin/main...HEAD",
			"--",
			path,
		)
	default:
		cmd = exec.Command("git", "diff", "--no-ext-diff", "-U0", "--staged", path)
	}
	output, err := cmd.Output()
	if err != nil {
		return map[int]struct{}{}
	}
	return parseGeminiChangedLines(string(output))
}

func collectGeminiChangedLines(files []string, scope string) map[string]map[int]struct{} {
	changed := make(map[string]map[int]struct{}, len(files))
	for _, file := range files {
		normalized := normalizeGeminiPath(file)
		changed[normalized] = changedLinesForGeminiFile(file, scope)
	}
	return changed
}

func isGeminiAddedOrUntracked(path string) bool {
	output, err := exec.Command("git", "status", "--porcelain", path).Output()
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
			if violation.File == "" {
				filtered.InDiff = append(filtered.InDiff, violation)
				continue
			}
			if _, err := os.Stat(violation.File); err != nil {
				filtered.InDiff = append(filtered.InDiff, violation)
				continue
			}
			if isGeminiAddedOrUntracked(violation.File) {
				filtered.InDiff = append(filtered.InDiff, violation)
			} else {
				filtered.PreExisting = append(filtered.PreExisting, violation)
			}
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

func (filtered geminiFilteredViolations) hasBlockingCriticals() bool {
	for _, violation := range filtered.InDiff {
		if violation.Severity == "CRITICAL" {
			return true
		}
	}
	return false
}

func (filtered geminiFilteredViolations) hasAnyInDiff() bool {
	return len(filtered.InDiff) > 0
}

type geminiBatchJob struct {
	CheckIndex int
	BatchIndex int
	Request    geminiRequestSettings
	Batch      geminiPreparedBatch
}

type geminiBatchJobResult struct {
	CheckIndex int
	BatchIndex int
	Outcome    geminiBatchOutcome
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
		if count < 2 {
			continue
		}
		if cacheName, ok := ensureGeminiExplicitCache(client, apiKey, seeds[key], key); ok {
			bindings[key] = cacheName
		}
	}
	return bindings
}

func executeGeminiChecks(
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
) []geminiCheckOutcome {
	patterns := make([]string, 0, len(settings.ModalAllowlistFiles))
	for _, pattern := range settings.ModalAllowlistFiles {
		normalized := normalizeGeminiModalAllowlistPattern(pattern)
		if normalized != "" {
			patterns = append(patterns, normalized)
		}
	}

	client := &http.Client{
		Timeout: time.Duration(settings.TimeoutSeconds) * time.Second,
	}
	explicitCacheBindings := buildGeminiExplicitCacheBindings(client, apiKey, prepared)
	outcomes := make([]geminiCheckOutcome, 0, len(prepared))
	jobs := make([]geminiBatchJob, 0)
	for checkIndex, check := range prepared {
		outcome := geminiCheckOutcome{
			Plan:     check.Plan,
			Batches:  make([]geminiBatchOutcome, len(check.Batches)),
			Filtered: geminiFilteredViolations{InDiff: []geminiViolation{}, PreExisting: []geminiViolation{}},
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

	if len(jobs) == 0 {
		return outcomes
	}

	limit := settings.MaxConcurrentAPICalls
	if limit <= 0 {
		limit = 1
	}
	semaphore := make(chan struct{}, limit)
	results := make(chan geminiBatchJobResult, len(jobs))
	var waitGroup sync.WaitGroup

	for _, job := range jobs {
		waitGroup.Add(1)
		go func(job geminiBatchJob) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() {
				<-semaphore
			}()

			batchOutcome := geminiBatchOutcome{
				Files: append([]string{}, job.Batch.Files...),
			}
			prompt := job.Batch.Prompt
			responseDependency := ""
			cachedContent := ""
			if cacheName, ok := explicitCacheBindings[job.Batch.ExplicitAPIKey]; ok {
				prompt = job.Batch.CachedPrompt
				responseDependency = job.Batch.ExplicitAPIKey
				cachedContent = cacheName
			}
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
				results <- geminiBatchJobResult{
					CheckIndex: job.CheckIndex,
					BatchIndex: job.BatchIndex,
					Outcome:    batchOutcome,
				}
				return
			}
			result, err := parseGeminiResult(responseText)
			if err != nil {
				batchOutcome.Error = err.Error()
				results <- geminiBatchJobResult{
					CheckIndex: job.CheckIndex,
					BatchIndex: job.BatchIndex,
					Outcome:    batchOutcome,
				}
				return
			}
			result.Violations = filterGeminiModalAllowlistedViolations(
				result.Violations,
				patterns,
			)
			batchOutcome.Result = result
			results <- geminiBatchJobResult{
				CheckIndex: job.CheckIndex,
				BatchIndex: job.BatchIndex,
				Outcome:    batchOutcome,
			}
		}(job)
	}

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	for result := range results {
		outcome := &outcomes[result.CheckIndex]
		outcome.Batches[result.BatchIndex] = result.Outcome
		if result.Outcome.Error != "" {
			outcome.BatchErrors++
			continue
		}
		outcome.BatchesCompleted++
	}

	for outcomeIndex := range outcomes {
		allViolations := make([]geminiViolation, 0)
		for _, batch := range outcomes[outcomeIndex].Batches {
			allViolations = append(allViolations, batch.Result.Violations...)
		}
		outcomes[outcomeIndex].Filtered = filterGeminiViolationsByDiff(allViolations, changedLinesByFile)
	}

	return outcomes
}

func geminiOutcomeViolations(outcome geminiCheckOutcome) []geminiViolation {
	violations := make([]geminiViolation, 0)
	for _, batch := range outcome.Batches {
		violations = append(violations, batch.Result.Violations...)
	}
	return violations
}

func geminiOutcomeStatus(outcome geminiCheckOutcome) string {
	if outcome.Filtered.hasBlockingCriticals() {
		return "FAIL"
	}
	if outcome.BatchErrors > 0 && outcome.BatchesCompleted == 0 {
		return "ERROR"
	}
	if outcome.BatchErrors > 0 {
		return "WARN"
	}
	for _, violation := range outcome.Filtered.InDiff {
		if violation.Severity == "WARNING" {
			return "WARN"
		}
	}
	return "PASS"
}

func formatGeminiReport(scope string, outcomes []geminiCheckOutcome) string {
	hasIssues := false
	for _, outcome := range outcomes {
		if geminiOutcomeStatus(outcome) != "PASS" || outcome.Filtered.hasAnyInDiff() {
			hasIssues = true
			break
		}
	}
	if !hasIssues {
		return ""
	}

	lines := []string{
		"",
		strings.Repeat("=", 70),
		"GEMINI AI CODE CHECKS (GO)",
		strings.Repeat("=", 70),
		fmt.Sprintf("Scope: %s", scope),
		"",
	}

	for _, outcome := range outcomes {
		status := geminiOutcomeStatus(outcome)
		if status == "PASS" && !outcome.Filtered.hasAnyInDiff() {
			continue
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"%s: %s (model=%s, tier=%s, %d included file(s), %d batch(es))",
				outcome.Plan.Name,
				status,
				outcome.Plan.Model,
				outcome.Plan.ServiceTier,
				len(outcome.Plan.IncludedFiles),
				len(outcome.Plan.Batches),
			),
		)
		if len(outcome.Plan.SkippedLargeFiles) > 0 {
			lines = append(
				lines,
				fmt.Sprintf(
					"  Skipped large files: %s",
					strings.Join(outcome.Plan.SkippedLargeFiles, ", "),
				),
			)
		}
		if len(outcome.Filtered.InDiff) > 0 {
			lines = append(lines, "  [In your changes]")
			for _, violation := range outcome.Filtered.InDiff {
				lineLabel := "?"
				if violation.Line > 0 {
					lineLabel = fmt.Sprintf("%d", violation.Line)
				}
				lines = append(
					lines,
					fmt.Sprintf(
						"  %s %s:%s %s",
						formatSeverityIcon(violation.Severity),
						violation.File,
						lineLabel,
						violation.Message,
					),
				)
				if violation.EthosSection != "" {
					lines = append(lines, fmt.Sprintf("     (ETHOS %s)", violation.EthosSection))
				}
			}
		}
		if len(outcome.Filtered.PreExisting) > 0 {
			lines = append(
				lines,
				fmt.Sprintf("  [Pre-existing (%d)]", len(outcome.Filtered.PreExisting)),
			)
			for _, violation := range outcome.Filtered.PreExisting {
				lineLabel := "?"
				if violation.Line > 0 {
					lineLabel = fmt.Sprintf("%d", violation.Line)
				}
				lines = append(
					lines,
					fmt.Sprintf(
						"  %s %s:%s %s",
						formatSeverityIcon(violation.Severity),
						violation.File,
						lineLabel,
						violation.Message,
					),
				)
				if violation.EthosSection != "" {
					lines = append(lines, fmt.Sprintf("     (ETHOS %s)", violation.EthosSection))
				}
			}
		}
		for index, batch := range outcome.Batches {
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
		lines = append(lines, "")
	}

	lines = append(lines, strings.Repeat("=", 70))
	return strings.Join(lines, "\n")
}

func formatSeverityIcon(severity string) string {
	switch severity {
	case "CRITICAL":
		return "XX"
	case "WARNING":
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

	pack, err := loadGeminiPromptPack(runtimePaths.BundleRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	files, scope, err := candidateFilesForGemini(options, pack)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	prepared, err := prepareGeminiChecks(
		pack,
		files,
		options.CheckType,
		settings,
		runtimePaths.CacheDir,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	plan := buildGeminiExecutionPlan(prepared, scope, options.DryRun)
	if options.DryRun {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: write Gemini dry-run plan: %v\n", err)
			return 1
		}
		return 0
	}

	totalBatches := 0
	for _, check := range prepared {
		totalBatches += len(check.Batches)
	}
	if totalBatches == 0 {
		return 0
	}

	apiKey := geminiAPIKey()
	if apiKey == "" {
		fmt.Fprintln(
			os.Stderr,
			"FATAL: GEMINI_API_KEY not set. AI code review is required. Add GEMINI_API_KEY to your environment.",
		)
		return 1
	}

	changedLinesByFile := collectGeminiChangedLines(files, scope)
	outcomes := executeGeminiChecks(settings, apiKey, prepared, changedLinesByFile)
	if report := formatGeminiReport(scope, outcomes); report != "" {
		fmt.Println(report)
	}

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

	switch {
	case hasCriticals:
		fmt.Fprint(
			os.Stderr,
			"\nXX Commit blocked: CRITICAL Gemini violations were found in the checked files.\n\n",
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

func existingFiles(paths []string) []string {
	files := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}
	}
	return files
}

func isBinary(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	buf := make([]byte, textChunkSize)
	n, _ := file.Read(buf)
	return bytes.Contains(buf[:n], []byte{0})
}

func readText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
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
			if err := os.WriteFile(path, []byte(fixed), 0o666); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				failed = true
			}
		}
	}
	return exitCode(failed)
}

func checkSyntax(_ Config, paths []string) int {
	failed := false
	for _, path := range existingFiles(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml":
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			for {
				var value any
				err := decoder.Decode(&value)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
					failed = true
					break
				}
			}
		case ".toml":
			var value any
			if err := toml.Unmarshal(data, &value); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				failed = true
			}
		case ".json":
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				failed = true
			}
		}
	}
	return exitCode(failed)
}

func checkMergeConflict(_ Config, paths []string) int {
	failed := false
	markers := []string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"}
	for _, path := range existingFiles(paths) {
		if isBinary(path) {
			continue
		}
		text, binary, err := readText(path)
		if err != nil || binary {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			for _, marker := range markers {
				if strings.HasPrefix(line, marker) {
					fmt.Fprintf(os.Stderr, "%s: unresolved merge conflict marker\n", path)
					failed = true
					goto nextFile
				}
			}
		}
	nextFile:
	}
	return exitCode(failed)
}

func checkShebangs(_ Config, paths []string) int {
	failed := false
	for _, path := range existingFiles(paths) {
		text, binary, err := readText(path)
		if err != nil || binary {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		executable := info.Mode()&0o111 != 0
		hasShebang := strings.HasPrefix(text, "#!")
		if executable && !hasShebang {
			fmt.Fprintf(os.Stderr, "%s: executable file has no shebang\n", path)
			failed = true
		}
		if hasShebang && !executable {
			fmt.Fprintf(os.Stderr, "%s: shebang script is not executable\n", path)
			failed = true
		}
	}
	return exitCode(failed)
}

func detectPrivateKey(_ Config, paths []string) int {
	failed := false
	privateKey := regexp.MustCompile(privateKeyPattern)
	for _, path := range existingFiles(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		if privateKey.Match(data) {
			fmt.Fprintf(os.Stderr, "%s: possible private key detected\n", path)
			failed = true
		}
	}
	return exitCode(failed)
}

func checkLargeFiles(cfg Config, paths []string) int {
	failed := false
	suffixes := stringSet(cfg.Text.LargeFileSuffixes)
	maxBytes := int64(cfg.Text.MaxLargeFileKB * 1024)
	for _, path := range existingFiles(paths) {
		if !suffixes[strings.ToLower(filepath.Ext(path))] || hasPrefix(path, cfg.Text.LargeFileExcludePrefixes) {
			continue
		}
		if !isAddedFile(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		if info.Size() > maxBytes {
			fmt.Fprintf(os.Stderr, "%s: %d KiB exceeds %d KiB limit\n", path, info.Size()/1024, cfg.Text.MaxLargeFileKB)
			failed = true
		}
	}
	return exitCode(failed)
}

func checkForbiddenStrings(cfg Config, paths []string) int {
	failed := false
	for _, path := range existingFiles(paths) {
		if isBinary(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: could not read file: %v\n", path, err)
			failed = true
			continue
		}
		for _, forbidden := range cfg.Text.ForbiddenStrings {
			if bytes.Contains(data, []byte(forbidden)) {
				fmt.Fprintf(os.Stderr, "ERROR: %s: contains forbidden string %q\n", path, forbidden)
				failed = true
			}
		}
	}
	return exitCode(failed)
}

func runShellcheck(_ Config, paths []string) int {
	files := shellFiles(existingFiles(paths))
	if len(files) == 0 {
		return 0
	}
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		fmt.Fprintln(os.Stderr, "FATAL: shellcheck is required. Install: apt/brew install shellcheck")
		return 1
	}
	args := append([]string{"--severity=warning", "-x"}, files...)
	cmd := exec.Command(shellcheck, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func checkShellBestPractices(cfg Config, paths []string) int {
	failed := false
	setPattern := regexp.MustCompile(`(?m)^\s*set\s+-[euo]+\s*pipefail|^\s*set\s+-euo\s+pipefail`)
	commonPattern := regexp.MustCompile(`(?m)source\s+.*common\.sh|^\.\s+.*common\.sh`)
	for _, path := range shellFiles(existingFiles(paths)) {
		text, binary, err := readText(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not read file: %v\n", path, err)
			failed = true
			continue
		}
		if binary {
			continue
		}
		var errs []string
		if !validShellShebang(text) {
			errs = append(errs, "missing or invalid shell shebang")
		}
		if !setPattern.MatchString(text) {
			errs = append(errs, "missing 'set -euo pipefail'")
		}
		if hasPrefix(path, cfg.Shell.RequireCommonForPrefixes) && !commonPattern.MatchString(text) {
			errs = append(errs, "scripts/ shell files must source the repository common shell helpers")
		}
		if len(errs) > 0 {
			failed = true
			fmt.Fprintf(os.Stderr, "\n%s:\n", path)
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "  - %s\n", err)
			}
		}
	}
	return exitCode(failed)
}

func checkLineLimits(cfg Config, paths []string) int {
	failed := false
	for _, path := range existingFiles(paths) {
		if !isLineLimited(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("ERROR: %s: Could not read file: %v\n", path, err)
			failed = true
			continue
		}
		lineCount := countLines(string(data))
		hardLimit, _ := limitsForFile(cfg, path)
		if lineCount <= hardLimit {
			continue
		}
		originalCount := originalLineCount(path)
		if originalCount < 0 {
			fmt.Printf("ERROR: %s: New file has %d lines (limit: %d)\n", path, lineCount, hardLimit)
			failed = true
		} else if lineCount > originalCount {
			fmt.Printf(
				"ERROR: %s: File grew from %d to %d lines (over %d limit). Must refactor to reduce size.\n",
				path,
				originalCount,
				lineCount,
				hardLimit,
			)
			failed = true
		}
	}
	if failed {
		fmt.Println()
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println("File size check FAILED")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println()
		fmt.Println("Refactoring suggestions:")
		fmt.Println("  - Extract helper functions to separate modules")
		fmt.Println("  - Split large files into focused submodules")
		fmt.Println("  - Move reusable code to lib/")
	}
	return exitCode(failed)
}

func checkCommitLint(cfg Config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook commitlint <commit-msg-file>")
		return 1
	}
	lines, err := commitMessageLines(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", args[0], err)
		return 1
	}
	errs := validateCommitMessage(cfg, lines)
	if len(errs) == 0 {
		return 0
	}
	fmt.Fprintln(os.Stderr, "Commit message does not satisfy conventional commit rules:")
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "  - %s\n", err)
	}
	return 1
}

func checkCommitAttribution(cfg Config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook commit-attribution <commit-msg-file>")
		return 1
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Could not read commit message: %v\n", err)
		return 1
	}
	patterns := attributionPatterns(cfg.CommitAttribution.BlockedNames)
	var violations []string
	for lineNo, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, pattern := range patterns {
			if match := pattern.FindString(trimmed); match != "" {
				violations = append(violations, fmt.Sprintf("  Line %d: %q in: %s", lineNo+1, match, trimmed))
				break
			}
		}
	}
	if len(violations) == 0 {
		return 0
	}
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("COMMIT MESSAGE CONTAINS FORBIDDEN AI ATTRIBUTION")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()
	fmt.Println("Per ETHOS §16 (No Self-Promotion), commit messages must not")
	fmt.Println("contain AI co-author lines, attribution, or promotional content.")
	fmt.Println()
	fmt.Println("Violations found:")
	for _, violation := range violations {
		fmt.Println(violation)
	}
	fmt.Println()
	fmt.Println("Remove the AI attribution and commit again.")
	fmt.Println(strings.Repeat("=", 60))
	return 1
}

func commitMessageLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			lines = append(lines, strings.TrimRight(line, " \t\r"))
		}
	}
	return lines, nil
}

func validateCommitMessage(cfg Config, lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return []string{"commit message is empty"}
	}
	header := lines[0]
	for _, prefix := range cfg.CommitLint.IgnoredPrefixes {
		if strings.HasPrefix(header, prefix) {
			return nil
		}
	}
	var errs []string
	if len(header) > cfg.CommitLint.MaxHeaderLength {
		errs = append(errs, fmt.Sprintf("header must be <= %d characters", cfg.CommitLint.MaxHeaderLength))
	}
	match := regexp.MustCompile(`^([a-z]+)\(([A-Za-z0-9_.-]+)\)!?: (.+)$`).FindStringSubmatch(header)
	if match == nil {
		return append(errs, "header must match: type(scope): subject")
	}
	if !stringSet(cfg.CommitLint.AllowedTypes)[match[1]] {
		allowed := slices.Clone(cfg.CommitLint.AllowedTypes)
		sort.Strings(allowed)
		errs = append(errs, "type must be one of: "+strings.Join(allowed, ", "))
	}
	if strings.TrimSpace(match[2]) == "" {
		errs = append(errs, "scope is required")
	}
	if strings.TrimSpace(match[3]) == "" {
		errs = append(errs, "subject is required")
	}
	hasBodyOrFooter := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			hasBodyOrFooter = true
			break
		}
	}
	if hasBodyOrFooter && len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		errs = append(errs, "body/footer must be separated from header by a blank line")
	}
	return errs
}

func attributionPatterns(names []string) []*regexp.Regexp {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	namesPattern := strings.Join(quoted, "|")
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)co-?authored-?by:\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)signed-?off-?by:\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)generated\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)created\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)written\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)assisted\s+by\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)\x{1F916}\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)(` + namesPattern + `)\s*\x{1F916}`),
		regexp.MustCompile(`\x{1F916}`),
	}
}

func shellFiles(paths []string) []string {
	var files []string
	for _, path := range paths {
		if isShellFile(path) {
			files = append(files, path)
		}
	}
	return files
}

func isShellFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".sh" || ext == ".bash" {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")
	return strings.HasPrefix(firstLine, "#!") && (strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}

func validShellShebang(text string) bool {
	firstLine, _, _ := strings.Cut(text, "\n")
	return strings.HasPrefix(firstLine, "#!") && (strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}

func isLineLimited(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".py" || ext == ".sh" || ext == ".bash" || strings.Contains(path, "scripts/")
}

func limitsForFile(cfg Config, path string) (int, int) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".py" {
		return cfg.LineLimits.PythonHard, cfg.LineLimits.PythonWarn
	}
	if ext == ".sh" || ext == ".bash" || strings.Contains(path, "scripts/") {
		return cfg.LineLimits.ShellHard, cfg.LineLimits.ShellWarn
	}
	return cfg.LineLimits.PythonHard, cfg.LineLimits.PythonWarn
}

func originalLineCount(path string) int {
	cmd := exec.Command("git", "show", "HEAD:"+path)
	output, err := cmd.Output()
	if err != nil {
		return -1
	}
	return countLines(string(output))
}

func countLines(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func isAddedFile(path string) bool {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=A", "--", path)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, added := range strings.Split(string(output), "\n") {
		if added == path {
			return true
		}
	}
	return false
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
