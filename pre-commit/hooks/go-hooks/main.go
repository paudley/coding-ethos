package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

type manifestValidationSettings struct {
	Enabled              bool                                  `json:"enabled"`
	CandidatePaths       []string                              `json:"candidate_paths"`
	RequiredStringFields []string                              `json:"required_string_fields"`
	RequiredListSections map[string]manifestValidationListSpec `json:"required_list_sections"`
}

type manifestValidationListSpec struct {
	Required             bool     `json:"required"`
	RequiredStringFields []string `json:"required_string_fields"`
	OptionalStringFields []string `json:"optional_string_fields"`
}

type planCompletionSettings struct {
	Enabled               bool     `json:"enabled"`
	MetadataFilename      string   `json:"metadata_filename"`
	RootMarkers           []string `json:"root_markers"`
	CompletedStatusValues []string `json:"completed_status_values"`
}

type pyprojectIgnoreSettings struct {
	Enabled                  bool     `json:"enabled"`
	AllowedIgnorePatterns    []string `json:"allowed_ignore_patterns"`
	AllowedExcludePatterns   []string `json:"allowed_exclude_patterns"`
	AllowedMypyMissingImport []string `json:"allowed_mypy_missing_imports"`
}

type pyprojectIgnoreFinding struct {
	Tool    string
	Setting string
	Target  string
	Detail  string
}

type commentSuppressionSettings struct {
	Enabled  bool                        `json:"enabled"`
	Patterns []commentSuppressionPattern `json:"patterns"`
}

type commentSuppressionPattern struct {
	Regex string `json:"regex"`
	Label string `json:"label"`
}

type compiledCommentSuppressionPattern struct {
	Regex *regexp.Regexp
	Label string
}

type commentSuppressionViolation struct {
	File    string
	Line    int
	Label   string
	Comment string
}

type moduleDocsSettings struct {
	Enabled            bool     `json:"enabled"`
	SourceDocsPath     string   `json:"source_docs_path"`
	CheckFilenames     []string `json:"check_filenames"`
	ExcludedDirs       []string `json:"excluded_dirs"`
	BannedDocFilenames []string `json:"banned_doc_filenames"`
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
	moduleDocsEntryPattern   = regexp.MustCompile(`(?m)^\s+([A-Za-z0-9_-]+\.md)\s*[:|-]`)
	moduleDocsPathPattern    = regexp.MustCompile(`(?m)^\s+([A-Za-z0-9_/-]+/[A-Za-z0-9_-]+\.md)\s*[:|-]`)
)

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
		"check-catch-and-silence":          checkCatchAndSilenceCommand,
		"check-comment-suppressions":       checkCommentSuppressionsCommand,
		"check-conditional-imports":        checkConditionalImportsCommand,
		"check-direct-imports":             checkDirectImportsCommand,
		"check-docstring-coverage":         checkDocstringCoverageCommand,
		"check-file-docstrings":            checkFileDocstringsCommand,
		"check-forbidden-strings":          checkForbiddenStrings,
		"check-large-files":                checkLargeFiles,
		"check-line-limits":                checkLineLimits,
		"check-merge-conflict":             checkMergeConflict,
		"check-optional-returns":           checkOptionalReturnsCommand,
		"check-module-docs":                checkModuleDocsCommand,
		"check-pyproject-ignores":          checkPyprojectIgnoresCommand,
		"check-pytest-gate":                checkPytestGateCommand,
		"check-security-patterns":          checkSecurityPatternsCommand,
		"check-shebangs":                   checkShebangs,
		"check-shell-best-practices":       checkShellBestPractices,
		"check-sql-centralization":         checkSQLCentralizationCommand,
		"check-structured-logging":         checkStructuredLoggingCommand,
		"check-syntax":                     checkSyntax,
		"check-type-checkers":              checkTypeCheckersCommand,
		"check-type-checking-imports":      checkTypeCheckingImportsCommand,
		"check-plan-completion":            checkPlanCompletionCommand,
		"check-python-version-consistency": checkPythonVersionConsistencyCommand,
		"config-get":                       configGet,
		"commit-attribution":               checkCommitAttribution,
		"commitlint":                       checkCommitLint,
		"detect-private-key":               detectPrivateKey,
		"fix-text":                         fixText,
		"gemini-check":                     runGeminiCheck,
		"quiet-filter":                     quietFilter,
		"shellcheck":                       runShellcheck,
		"check-util-centralization":        checkUtilCentralizationCommand,
		"validate-manifest":                validateManifestCommand,
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
	fmt.Fprintln(
		os.Stderr,
		"  config-get <dot.path> [default] prints merged config values",
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

func loadManifestValidationSettings() (manifestValidationSettings, error) {
	var settings manifestValidationSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	value, ok := rootConfigValue(rootConfig, "python.manifest_validation")
	if !ok {
		return settings, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return settings, fmt.Errorf("encode manifest_validation config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("parse manifest_validation config: %w", err)
	}
	if len(settings.CandidatePaths) == 0 {
		settings.CandidatePaths = []string{"manifest.yaml", "code-ethos/manifest.yaml"}
	}
	if len(settings.RequiredStringFields) == 0 {
		settings.RequiredStringFields = []string{"version"}
	}
	if len(settings.RequiredListSections) == 0 {
		settings.RequiredListSections = map[string]manifestValidationListSpec{
			"symlinks": {
				Required:             true,
				RequiredStringFields: []string{"source", "target"},
			},
			"repositories": {
				Required:             false,
				RequiredStringFields: []string{"name", "url"},
				OptionalStringFields: []string{"branch"},
			},
		}
	}
	return settings, nil
}

func loadPlanCompletionSettings() (planCompletionSettings, error) {
	var settings planCompletionSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	value, ok := rootConfigValue(rootConfig, "python.plan_completion")
	if !ok {
		return settings, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return settings, fmt.Errorf("encode plan_completion config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("parse plan_completion config: %w", err)
	}
	if strings.TrimSpace(settings.MetadataFilename) == "" {
		settings.MetadataFilename = "metadata.yaml"
	}
	if len(settings.RootMarkers) == 0 {
		settings.RootMarkers = []string{"docs/plans/"}
	}
	if len(settings.CompletedStatusValues) == 0 {
		settings.CompletedStatusValues = []string{"review", "complete"}
	}
	return settings, nil
}

func loadPyprojectIgnoreSettings() (pyprojectIgnoreSettings, error) {
	var settings pyprojectIgnoreSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	value, ok := rootConfigValue(rootConfig, "python.pyproject_ignores")
	if !ok {
		return settings, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return settings, fmt.Errorf("encode pyproject_ignores config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("parse pyproject_ignores config: %w", err)
	}
	if len(settings.AllowedIgnorePatterns) == 0 {
		settings.AllowedIgnorePatterns = []string{
			"tests/**", "tests/*", "**/tests/**", "**/tests/*",
			"test_*.py", "*_test.py",
			"stubs/**", "stubs/*", "**/stubs/**", "**/stubs/*",
		}
	}
	if len(settings.AllowedExcludePatterns) == 0 {
		settings.AllowedExcludePatterns = []string{
			".git", ".venv", ".mypy_cache", ".ruff_cache", "__pycache__", "*.egg-info",
			".eggs", "build", "dist", "node_modules",
		}
	}
	return settings, nil
}

func loadCommentSuppressionSettings() (commentSuppressionSettings, error) {
	var settings commentSuppressionSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	value, ok := rootConfigValue(rootConfig, "python.comment_suppressions")
	if !ok {
		return settings, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return settings, fmt.Errorf("encode comment_suppressions config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("parse comment_suppressions config: %w", err)
	}
	if len(settings.Patterns) == 0 {
		settings.Patterns = []commentSuppressionPattern{
			{Regex: `#\s*ruff:\s*noqa\b`, Label: "ruff: noqa (file-level)"},
			{Regex: `#\s*mypy:\s*ignore-errors\b`, Label: "mypy: ignore-errors (file-level)"},
			{Regex: `#\s*noqa\b`, Label: "noqa"},
			{Regex: `#\s*type:\s*ignore\b`, Label: "type: ignore"},
			{Regex: `#\s*pragma:\s*no\s*cover\b`, Label: "pragma: no cover"},
			{Regex: `#\s*pylint:\s*disable`, Label: "pylint: disable"},
			{Regex: `#\s*noinspection\b`, Label: "noinspection"},
			{Regex: `#\s*fmt:\s*(off|on|skip)\b`, Label: "fmt: off/on/skip"},
			{Regex: `#\s*isort:\s*(skip|skip_file)\b`, Label: "isort: skip"},
			{Regex: `#\s*pyright:\s*ignore\b`, Label: "pyright: ignore"},
		}
	}
	return settings, nil
}

func loadModuleDocsSettings() (moduleDocsSettings, error) {
	var settings moduleDocsSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	value, ok := rootConfigValue(rootConfig, "python.module_docs")
	if !ok {
		return settings, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return settings, fmt.Errorf("encode module_docs config: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("parse module_docs config: %w", err)
	}
	if strings.TrimSpace(settings.SourceDocsPath) == "" {
		settings.SourceDocsPath = "docs/SOURCE_DOCS.md"
	}
	if len(settings.CheckFilenames) == 0 {
		settings.CheckFilenames = []string{"__init__.py", "conftest.py"}
	}
	if len(settings.ExcludedDirs) == 0 {
		settings.ExcludedDirs = []string{
			".venv",
			".lint-cache",
			".mypy_cache",
			".ruff_cache",
			"__pycache__",
			"node_modules",
			".git",
		}
	}
	if len(settings.BannedDocFilenames) == 0 {
		settings.BannedDocFilenames = []string{"README.md", "readme.md"}
	}
	return settings, nil
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

func rootConfigValue(root map[string]any, path string) (any, bool) {
	current := any(root)
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := nextMap[part]
		if !ok {
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
			return "", err
		}
		return string(data), nil
	}
}

func configGet(_ Config, args []string) int {
	if len(args) < 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook config-get <dot.path> [default]")
		return 1
	}

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	value, ok := rootConfigValue(rootConfig, args[0])
	if !ok {
		if len(args) >= 2 {
			fmt.Println(args[1])
			return 0
		}
		fmt.Fprintf(os.Stderr, "FATAL: config path not found: %s\n", args[0])
		return 1
	}

	formatted, err := formatRootConfigValue(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: format config value %s: %v\n", args[0], err)
		return 1
	}
	fmt.Println(formatted)
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

func findManifestPath(settings manifestValidationSettings) (string, error) {
	for _, raw := range settings.CandidatePaths {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("manifest candidate not found")
}

func validateManifestData(
	data map[string]any,
	settings manifestValidationSettings,
) []string {
	errors := make([]string, 0)

	for _, fieldName := range settings.RequiredStringFields {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}
		value, ok := data[fieldName]
		if !ok {
			errors = append(errors, fmt.Sprintf("Missing required '%s' field", fieldName))
			continue
		}
		if _, ok := value.(string); !ok {
			errors = append(errors, fmt.Sprintf("'%s' must be a string", fieldName))
		}
	}

	for sectionName, spec := range settings.RequiredListSections {
		sectionValue, ok := data[sectionName]
		if !ok || sectionValue == nil {
			if spec.Required {
				errors = append(errors, fmt.Sprintf("Missing required '%s' section", sectionName))
			}
			continue
		}
		entries, ok := sectionValue.([]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("'%s' must be a list", sectionName))
			continue
		}
		for index, entry := range entries {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				errors = append(
					errors,
					fmt.Sprintf("%s[%d]: Expected dict, got %T", sectionName, index, entry),
				)
				continue
			}
			for _, fieldName := range spec.RequiredStringFields {
				fieldName = strings.TrimSpace(fieldName)
				if fieldName == "" {
					continue
				}
				value, ok := entryMap[fieldName]
				if !ok {
					errors = append(
						errors,
						fmt.Sprintf("%s[%d]: Missing '%s' field", sectionName, index, fieldName),
					)
					continue
				}
				if _, ok := value.(string); !ok {
					errors = append(
						errors,
						fmt.Sprintf("%s[%d].%s: Expected string", sectionName, index, fieldName),
					)
				}
			}
			for _, fieldName := range spec.OptionalStringFields {
				fieldName = strings.TrimSpace(fieldName)
				if fieldName == "" {
					continue
				}
				value, ok := entryMap[fieldName]
				if ok {
					if _, ok := value.(string); !ok {
						errors = append(
							errors,
							fmt.Sprintf("%s[%d].%s: Expected string", sectionName, index, fieldName),
						)
					}
				}
			}
		}
	}

	return errors
}

func validateManifestCommand(_ Config, _ []string) int {
	settings, err := loadManifestValidationSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled {
		return 0
	}

	manifestPath, err := findManifestPath(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Could not read %s: %v\n", manifestPath, err)
		return 1
	}

	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Invalid YAML syntax in %s:\n", manifestPath)
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		return 1
	}
	if data == nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s must be a YAML mapping (dict)\n", manifestPath)
		return 1
	}

	errors := validateManifestData(data, settings)
	if len(errors) > 0 {
		fmt.Fprintf(os.Stderr, "ERROR: %s validation failed:\n", manifestPath)
		for _, item := range errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", item)
		}
		return 1
	}

	return 0
}

func stagedFiles() ([]string, error) {
	output := gitOutput("diff", "--cached", "--name-only")
	if output == "" {
		return []string{}, nil
	}
	return strings.Fields(output), nil
}

func findPlanMetadataFiles(
	paths []string,
	settings planCompletionSettings,
) []string {
	matches := make([]string, 0)
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if filepath.Base(path) != settings.MetadataFilename {
			continue
		}
		normalized := normalizeGeminiPath(path)
		for _, marker := range settings.RootMarkers {
			marker = normalizeGeminiPath(marker)
			if marker != "" && strings.Contains(normalized, marker) {
				matches = append(matches, path)
				break
			}
		}
	}
	return matches
}

func planStatus(metadataPath string) (string, error) {
	content, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return "", err
	}
	status, _ := data["status"].(string)
	return strings.TrimSpace(status), nil
}

type uncheckedPlanItem struct {
	File string
	Line int
	Text string
}

func findUncheckedPlanItems(planDir string) ([]uncheckedPlanItem, error) {
	items := make([]uncheckedPlanItem, 0)
	pattern := regexp.MustCompile(`^-\s*\[\s*\]\s+.+`)
	err := filepath.WalkDir(planDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for index, line := range strings.Split(string(content), "\n") {
			if pattern.MatchString(line) {
				items = append(items, uncheckedPlanItem{
					File: path,
					Line: index + 1,
					Text: strings.TrimSpace(line),
				})
			}
		}
		return nil
	})
	return items, err
}

func checkPlanCompletionErrors(
	metadataPath string,
	settings planCompletionSettings,
) ([]string, error) {
	status, err := planStatus(metadataPath)
	if err != nil {
		return nil, err
	}

	completed := make(map[string]struct{}, len(settings.CompletedStatusValues))
	for _, value := range settings.CompletedStatusValues {
		value = strings.TrimSpace(value)
		if value != "" {
			completed[value] = struct{}{}
		}
	}
	if _, ok := completed[status]; !ok {
		return []string{}, nil
	}

	planDir := filepath.Dir(metadataPath)
	unchecked, err := findUncheckedPlanItems(planDir)
	if err != nil {
		return nil, err
	}
	if len(unchecked) == 0 {
		return []string{}, nil
	}

	errors := []string{
		"",
		strings.Repeat("=", 60),
		"PLAN COMPLETION FRAUD DETECTED",
		strings.Repeat("=", 60),
		"",
		fmt.Sprintf("Plan: %s", filepath.Base(planDir)),
		fmt.Sprintf("Claimed status: %s", status),
		"",
		"But these items are still unchecked:",
	}
	for _, item := range unchecked {
		relative, relErr := filepath.Rel(planDir, item.File)
		if relErr != nil {
			relative = item.File
		}
		errors = append(errors, fmt.Sprintf("  %s:%d: %s", relative, item.Line, item.Text))
	}
	errors = append(
		errors,
		"",
		strings.Repeat("=", 60),
		fmt.Sprintf("BLOCKED: Cannot mark plan as %q with incomplete items.", status),
		"",
		"Options:",
		"  1. Complete the work (check off items when done)",
		"  2. Get explicit user approval to remove items from scope",
		"  3. Change status back to 'in_progress'",
		"",
		"DO NOT:",
		"  - Use 'git commit --no-verify' to bypass this check",
		"  - Rationalize why incomplete items don't matter",
		"  - Claim YAGNI/KISS for spec'd requirements",
		strings.Repeat("=", 60),
	)
	return errors, nil
}

func checkPlanCompletionCommand(_ Config, args []string) int {
	settings, err := loadPlanCompletionSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled {
		return 0
	}

	paths := args
	if len(paths) == 0 {
		paths, err = stagedFiles()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: collect staged files: %v\n", err)
			return 1
		}
	}
	metadataFiles := findPlanMetadataFiles(paths, settings)

	allErrors := make([]string, 0)
	for _, metadataPath := range metadataFiles {
		errors, err := checkPlanCompletionErrors(metadataPath, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: %s: %v\n", metadataPath, err)
			return 1
		}
		allErrors = append(allErrors, errors...)
	}
	if len(allErrors) > 0 {
		fmt.Fprintln(os.Stderr, strings.Join(allErrors, "\n"))
		return 1
	}
	return 0
}

func (finding pyprojectIgnoreFinding) render() string {
	if finding.Detail != "" {
		return fmt.Sprintf("%s %s: %s -> %s", finding.Tool, finding.Setting, finding.Target, finding.Detail)
	}
	return fmt.Sprintf("%s %s: %s", finding.Tool, finding.Setting, finding.Target)
}

func normalizeStringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		return append([]string{}, typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				items = append(items, fmt.Sprint(item))
			}
		}
		return items
	case map[string]any:
		return []string{fmt.Sprint(typed)}
	default:
		return []string{fmt.Sprint(value)}
	}
}

func addPyprojectPerFileFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	value any,
) {
	if typed, ok := value.(map[string]any); ok {
		for pattern, codes := range typed {
			codeList := normalizeStringList(codes)
			if len(codeList) == 0 {
				findings[pyprojectIgnoreFinding{Tool: tool, Setting: setting, Target: pattern, Detail: "<all>"}] = struct{}{}
				continue
			}
			for _, code := range codeList {
				findings[pyprojectIgnoreFinding{Tool: tool, Setting: setting, Target: pattern, Detail: code}] = struct{}{}
			}
		}
		return
	}
	for _, entry := range normalizeStringList(value) {
		findings[pyprojectIgnoreFinding{Tool: tool, Setting: setting, Target: entry}] = struct{}{}
	}
}

func addPyprojectPatternFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	value any,
) {
	for _, pattern := range normalizeStringList(value) {
		findings[pyprojectIgnoreFinding{Tool: tool, Setting: setting, Target: pattern}] = struct{}{}
	}
}

func pyprojectMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func extractRuffFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}
	ruff := pyprojectMap(toolTable["ruff"])
	if ruff == nil {
		return findings
	}
	lint := pyprojectMap(ruff["lint"])
	for _, key := range []string{"per-file-ignores", "extend-per-file-ignores", "per_file_ignores", "extend_per_file_ignores"} {
		if lint != nil {
			if value, ok := lint[key]; ok {
				addPyprojectPerFileFindings(findings, "ruff", key, value)
			}
		}
		if value, ok := ruff[key]; ok {
			addPyprojectPerFileFindings(findings, "ruff", key, value)
		}
	}
	for _, key := range []string{"exclude", "extend-exclude", "extend_exclude"} {
		if value, ok := ruff[key]; ok {
			addPyprojectPatternFindings(findings, "ruff", key, value)
		}
	}
	return findings
}

func addMypyOverrideFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	override map[string]any,
) {
	modules := normalizeStringList(firstNonNil(override["module"], override["modules"]))
	if len(modules) == 0 {
		modules = []string{"<unknown>"}
	}
	for _, key := range []string{"ignore_errors", "ignore_missing_imports", "disable_error_code", "disable_error_codes"} {
		value, ok := override[key]
		if !ok {
			continue
		}
		switch key {
		case "disable_error_code", "disable_error_codes":
			for _, code := range normalizeStringList(value) {
				if strings.TrimSpace(code) == "" {
					continue
				}
				for _, module := range modules {
					findings[pyprojectIgnoreFinding{
						Tool:    "mypy",
						Setting: "override." + key,
						Target:  module,
						Detail:  code,
					}] = struct{}{}
				}
			}
		default:
			if boolean, ok := value.(bool); ok {
				if boolean {
					for _, module := range modules {
						findings[pyprojectIgnoreFinding{
							Tool:    "mypy",
							Setting: "override." + key,
							Target:  module,
						}] = struct{}{}
					}
				}
				continue
			}
			for _, module := range modules {
				findings[pyprojectIgnoreFinding{
					Tool:    "mypy",
					Setting: "override." + key,
					Target:  module,
					Detail:  fmt.Sprint(value),
				}] = struct{}{}
			}
		}
	}
}

func extractMypyFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}
	mypy := pyprojectMap(toolTable["mypy"])
	if mypy == nil {
		return findings
	}
	for _, key := range []string{"per-file-ignores", "per_file_ignores"} {
		if value, ok := mypy[key]; ok {
			addPyprojectPerFileFindings(findings, "mypy", key, value)
		}
	}
	if value, ok := mypy["exclude"]; ok {
		addPyprojectPatternFindings(findings, "mypy", "exclude", value)
	}
	if overrides, ok := mypy["overrides"].([]any); ok {
		for _, rawOverride := range overrides {
			override := pyprojectMap(rawOverride)
			if override != nil {
				addMypyOverrideFindings(findings, override)
			}
		}
	}
	return findings
}

func extractPyrightFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}
	pyright := pyprojectMap(toolTable["pyright"])
	if pyright == nil {
		return findings
	}
	for _, key := range []string{"exclude", "ignore"} {
		if value, ok := pyright[key]; ok {
			addPyprojectPatternFindings(findings, "pyright", key, value)
		}
	}
	return findings
}

func extractPylintFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}
	pylint := pyprojectMap(toolTable["pylint"])
	if pylint == nil {
		return findings
	}
	sections := []map[string]any{pylint}
	if mainSection := pyprojectMap(pylint["main"]); mainSection != nil {
		sections = append(sections, mainSection)
	}
	for _, section := range sections {
		for _, key := range []string{"ignore", "ignore-patterns", "ignore-paths", "ignore-modules", "ignored-modules"} {
			if value, ok := section[key]; ok {
				addPyprojectPatternFindings(findings, "pylint", key, value)
			}
		}
	}
	return findings
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func extractPyprojectFindings(config map[string]any) map[pyprojectIgnoreFinding]struct{} {
	toolTable := pyprojectMap(config["tool"])
	if toolTable == nil {
		return map[pyprojectIgnoreFinding]struct{}{}
	}
	findings := map[pyprojectIgnoreFinding]struct{}{}
	for finding := range extractRuffFindings(toolTable) {
		findings[finding] = struct{}{}
	}
	for finding := range extractMypyFindings(toolTable) {
		findings[finding] = struct{}{}
	}
	for finding := range extractPyrightFindings(toolTable) {
		findings[finding] = struct{}{}
	}
	for finding := range extractPylintFindings(toolTable) {
		findings[finding] = struct{}{}
	}
	return findings
}

func filterAllowedPyprojectFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	settings pyprojectIgnoreSettings,
) []pyprojectIgnoreFinding {
	allowedIgnore := stringSet(settings.AllowedIgnorePatterns)
	allowedExclude := stringSet(settings.AllowedExcludePatterns)
	allowedMypyMissing := stringSet(settings.AllowedMypyMissingImport)

	filtered := make([]pyprojectIgnoreFinding, 0, len(findings))
	for finding := range findings {
		if allowedIgnore[finding.Target] {
			continue
		}
		if (finding.Setting == "exclude" || finding.Setting == "extend-exclude" || finding.Setting == "extend_exclude") &&
			allowedExclude[finding.Target] {
			continue
		}
		if finding.Tool == "mypy" &&
			finding.Setting == "override.ignore_missing_imports" &&
			allowedMypyMissing[finding.Target] {
			continue
		}
		filtered = append(filtered, finding)
	}

	sort.Slice(filtered, func(i, j int) bool {
		left := filtered[i]
		right := filtered[j]
		return fmt.Sprintf("%s|%s|%s|%s", left.Tool, left.Setting, left.Target, left.Detail) <
			fmt.Sprintf("%s|%s|%s|%s", right.Tool, right.Setting, right.Target, right.Detail)
	})
	return filtered
}

func loadPyprojectConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid TOML: %w", err)
	}
	return config, nil
}

func checkPyprojectIgnoresCommand(_ Config, args []string) int {
	settings, err := loadPyprojectIgnoreSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	hasErrors := false
	for _, rawPath := range args {
		path := strings.TrimSpace(rawPath)
		if filepath.Base(path) != "pyproject.toml" {
			continue
		}

		config, err := loadPyprojectConfig(path)
		if err != nil {
			hasErrors = true
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", path, err)
			continue
		}

		findings := filterAllowedPyprojectFindings(extractPyprojectFindings(config), settings)
		if len(findings) == 0 {
			continue
		}
		hasErrors = true
		fmt.Fprintf(os.Stderr, "ERROR: %s contains forbidden linter file ignores:\n", path)
		for _, finding := range findings {
			fmt.Fprintf(os.Stderr, "  %s\n", finding.render())
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", 60))
		fmt.Fprintln(os.Stderr, "Pyproject linter ignore check FAILED")
		fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintln(
			os.Stderr,
			"Move file-specific ignores into the files themselves with documentation (e.g., # noqa / # type: ignore[code]).",
		)
		return 1
	}
	return 0
}

func compileCommentSuppressionPatterns(
	settings commentSuppressionSettings,
) ([]compiledCommentSuppressionPattern, error) {
	compiled := make([]compiledCommentSuppressionPattern, 0, len(settings.Patterns))
	for _, pattern := range settings.Patterns {
		expr := strings.TrimSpace(pattern.Regex)
		label := strings.TrimSpace(pattern.Label)
		if expr == "" || label == "" {
			continue
		}
		regex, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid comment suppression regex %q: %w", expr, err)
		}
		compiled = append(compiled, compiledCommentSuppressionPattern{
			Regex: regex,
			Label: label,
		})
	}
	return compiled, nil
}

func classifyCommentSuppression(
	comment string,
	patterns []compiledCommentSuppressionPattern,
) string {
	for _, pattern := range patterns {
		if pattern.Regex.MatchString(comment) {
			return pattern.Label
		}
	}
	return ""
}

func findPythonComments(text string) []commentSuppressionViolation {
	const (
		scanNormal = iota
		scanSingleQuote
		scanDoubleQuote
		scanTripleSingleQuote
		scanTripleDoubleQuote
	)

	violations := make([]commentSuppressionViolation, 0)
	state := scanNormal
	line := 1

	for i := 0; i < len(text); {
		ch := text[i]
		switch state {
		case scanNormal:
			switch ch {
			case '#':
				start := i
				for i < len(text) && text[i] != '\n' {
					i++
				}
				violations = append(violations, commentSuppressionViolation{
					Line:    line,
					Comment: strings.TrimSpace(text[start:i]),
				})
				continue
			case '\'':
				if i+2 < len(text) && text[i+1] == '\'' && text[i+2] == '\'' {
					state = scanTripleSingleQuote
					i += 3
					continue
				}
				state = scanSingleQuote
				i++
				continue
			case '"':
				if i+2 < len(text) && text[i+1] == '"' && text[i+2] == '"' {
					state = scanTripleDoubleQuote
					i += 3
					continue
				}
				state = scanDoubleQuote
				i++
				continue
			case '\n':
				line++
			}
			i++
		case scanSingleQuote:
			switch ch {
			case '\\':
				if i+1 < len(text) && text[i+1] == '\n' {
					line++
				}
				i += 2
			case '\n':
				line++
				state = scanNormal
				i++
			case '\'':
				state = scanNormal
				i++
			default:
				i++
			}
		case scanDoubleQuote:
			switch ch {
			case '\\':
				if i+1 < len(text) && text[i+1] == '\n' {
					line++
				}
				i += 2
			case '\n':
				line++
				state = scanNormal
				i++
			case '"':
				state = scanNormal
				i++
			default:
				i++
			}
		case scanTripleSingleQuote:
			if ch == '\n' {
				line++
			}
			if i+2 < len(text) && text[i] == '\'' && text[i+1] == '\'' && text[i+2] == '\'' {
				state = scanNormal
				i += 3
				continue
			}
			i++
		case scanTripleDoubleQuote:
			if ch == '\n' {
				line++
			}
			if i+2 < len(text) && text[i] == '"' && text[i+1] == '"' && text[i+2] == '"' {
				state = scanNormal
				i += 3
				continue
			}
			i++
		}
	}

	return violations
}

func findCommentSuppressions(
	path string,
	patterns []compiledCommentSuppressionPattern,
) ([]commentSuppressionViolation, error) {
	text, binary, err := readText(path)
	if err != nil {
		return nil, err
	}
	if binary {
		return nil, nil
	}
	comments := findPythonComments(text)
	violations := make([]commentSuppressionViolation, 0)
	for _, comment := range comments {
		label := classifyCommentSuppression(comment.Comment, patterns)
		if label == "" {
			continue
		}
		comment.File = path
		comment.Label = label
		violations = append(violations, comment)
	}
	return violations, nil
}

func checkCommentSuppressionsCommand(_ Config, args []string) int {
	settings, err := loadCommentSuppressionSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled || len(args) == 0 {
		return 0
	}
	patterns, err := compileCommentSuppressionPatterns(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	allViolations := make([]commentSuppressionViolation, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != ".py" {
			continue
		}
		violations, err := findCommentSuppressions(path, patterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", path, err)
			return 1
		}
		allViolations = append(allViolations, violations...)
	}

	if len(allViolations) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", 70))
	fmt.Fprintln(os.Stderr, "COMMENT-BASED LINT SUPPRESSION DETECTED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", 70))
	fmt.Fprintln(os.Stderr, "Comment-based suppressions (noqa, type: ignore, pragma, etc.)")
	fmt.Fprintln(os.Stderr, "are banned. Fix the underlying issue instead of suppressing it.")
	fmt.Fprintln(os.Stderr, "Per ETHOS §14: linters are not suggestions, they are enforcement.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations found:")
	for _, violation := range allViolations {
		fmt.Fprintf(
			os.Stderr,
			"  %s:%d: [%s] %s\n",
			violation.File,
			violation.Line,
			violation.Label,
			violation.Comment,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(os.Stderr, "  Remove the suppression comment and fix the code.")
	fmt.Fprintln(os.Stderr, "  If a linter flags an issue, apply SOLID principles:")
	fmt.Fprintln(os.Stderr, "    - Long function?  Split into focused units (SRP)")
	fmt.Fprintln(os.Stderr, "    - Too many params? Use config objects (ISP)")
	fmt.Fprintln(os.Stderr, "    - Complex logic?   Use polymorphism (OCP)")
	fmt.Fprintln(os.Stderr, "    - Tight coupling?  Inject dependencies (DIP)")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", 70))
	return 1
}

func shouldCheckModuleDocsFile(path string, settings moduleDocsSettings) bool {
	checkNames := stringSet(settings.CheckFilenames)
	if !checkNames[filepath.Base(path)] {
		return false
	}
	excluded := stringSet(settings.ExcludedDirs)
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if excluded[part] {
			return false
		}
	}
	return true
}

func discoverModuleDocsFiles(root string, settings moduleDocsSettings) ([]string, error) {
	matches := make([]string, 0)
	excluded := stringSet(settings.ExcludedDirs)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if excluded[entry.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldCheckModuleDocsFile(path, settings) {
			matches = append(matches, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func listColocatedMarkdownFiles(path string) ([]string, error) {
	directory := filepath.Dir(path)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(files, filepath.ToSlash(filepath.Join(directory, entry.Name())))
		}
	}
	sort.Strings(files)
	return files, nil
}

func extractModuleDocstringFromFile(path string) (string, error) {
	text, binary, err := readText(path)
	if err != nil {
		return "", err
	}
	if binary {
		return "", nil
	}
	return extractModuleDocstring(text)
}

func extractModuleDocstring(text string) (string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	index := 0
	for index < len(text) {
		for index < len(text) {
			switch text[index] {
			case ' ', '\t', '\r', '\n':
				index++
			default:
				goto afterWhitespace
			}
		}
	afterWhitespace:
		if index >= len(text) {
			return "", nil
		}
		if text[index] == '#' {
			for index < len(text) && text[index] != '\n' {
				index++
			}
			continue
		}
		return parseModuleDocstringLiteral(text, index)
	}
	return "", nil
}

func parseModuleDocstringLiteral(text string, start int) (string, error) {
	index := start
	for index < len(text) && ((text[index] >= 'a' && text[index] <= 'z') || (text[index] >= 'A' && text[index] <= 'Z')) {
		index++
	}
	prefix := strings.ToLower(text[start:index])
	for _, r := range prefix {
		if r != 'r' && r != 'u' {
			return "", nil
		}
	}
	if index >= len(text) {
		return "", nil
	}
	quote := text[index]
	if quote != '\'' && quote != '"' {
		return "", nil
	}
	triple := index+2 < len(text) && text[index+1] == quote && text[index+2] == quote
	if triple {
		contentStart := index + 3
		for cursor := contentStart; cursor+2 < len(text); cursor++ {
			if text[cursor] == '\\' {
				cursor++
				continue
			}
			if text[cursor] == quote && text[cursor+1] == quote && text[cursor+2] == quote {
				return text[contentStart:cursor], nil
			}
		}
		return "", fmt.Errorf("unterminated triple-quoted module docstring")
	}
	contentStart := index + 1
	for cursor := contentStart; cursor < len(text); cursor++ {
		switch text[cursor] {
		case '\\':
			cursor++
		case '\n':
			return "", fmt.Errorf("unterminated module docstring")
		case quote:
			return text[contentStart:cursor], nil
		}
	}
	return "", fmt.Errorf("unterminated module docstring")
}

func hasMeaningfulModuleDocstring(docstring string) bool {
	return strings.TrimSpace(docstring) != ""
}

func extractModuleSeeAlsoContent(docstring string) string {
	location := moduleDocsSeeAlsoPattern.FindStringIndex(docstring)
	if location == nil {
		return ""
	}
	return docstring[location[1]:]
}

func extractModuleSeeAlsoReferences(docstring string) []string {
	content := extractModuleSeeAlsoContent(docstring)
	if content == "" {
		return nil
	}
	refs := make(map[string]struct{})
	for _, match := range moduleDocsEntryPattern.FindAllStringSubmatch(content, -1) {
		if len(match) >= 2 {
			refs[match[1]] = struct{}{}
		}
	}
	return sortedKeys(refs)
}

func extractModulePathPrefixedReferences(docstring string) []string {
	content := extractModuleSeeAlsoContent(docstring)
	if content == "" {
		return nil
	}
	refs := make(map[string]struct{})
	for _, match := range moduleDocsPathPattern.FindAllStringSubmatch(content, -1) {
		if len(match) >= 2 {
			refs[match[1]] = struct{}{}
		}
	}
	return sortedKeys(refs)
}

func missingModuleDocstringReferences(docstring string, markdownFiles []string) []string {
	if docstring == "" {
		return markdownFiles
	}
	referenced := stringSet(extractModuleSeeAlsoReferences(docstring))
	missing := make([]string, 0)
	for _, markdownFile := range markdownFiles {
		if !referenced[filepath.Base(markdownFile)] {
			missing = append(missing, markdownFile)
		}
	}
	return missing
}

func nonexistentModuleReferences(path string, docstring string) []string {
	directory := filepath.Dir(path)
	missing := make([]string, 0)
	for _, ref := range extractModuleSeeAlsoReferences(docstring) {
		if _, err := os.Stat(filepath.Join(directory, ref)); errors.Is(err, os.ErrNotExist) {
			missing = append(missing, ref)
		}
	}
	sort.Strings(missing)
	return missing
}

func loadModuleDocsIndex(settings moduleDocsSettings) (string, error) {
	if strings.TrimSpace(settings.SourceDocsPath) == "" {
		return "", nil
	}
	data, err := os.ReadFile(settings.SourceDocsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func missingSourceDocsEntries(markdownFiles []string, index string) []string {
	if strings.TrimSpace(index) == "" {
		return append([]string{}, markdownFiles...)
	}
	missing := make([]string, 0)
	for _, markdownFile := range markdownFiles {
		directory := strings.TrimPrefix(filepath.ToSlash(filepath.Dir(markdownFile))+"/", "./")
		name := filepath.Base(markdownFile)
		if !strings.Contains(index, directory) || !strings.Contains(index, name) {
			missing = append(missing, markdownFile)
		}
	}
	return missing
}

func bannedModuleDocFilenames(markdownFiles []string, settings moduleDocsSettings) []string {
	banned := stringSet(settings.BannedDocFilenames)
	violations := make([]string, 0)
	for _, markdownFile := range markdownFiles {
		if banned[filepath.Base(markdownFile)] {
			violations = append(violations, markdownFile)
		}
	}
	sort.Strings(violations)
	return violations
}

func collectModuleDocsViolations(
	files []string,
	settings moduleDocsSettings,
) (moduleDocsViolations, error) {
	violations := moduleDocsViolations{}
	allMarkdown := make(map[string]struct{})

	for _, path := range files {
		if !shouldCheckModuleDocsFile(path, settings) {
			continue
		}

		docstring, err := extractModuleDocstringFromFile(path)
		if err != nil {
			return violations, fmt.Errorf("%s: %w", path, err)
		}
		if !hasMeaningfulModuleDocstring(docstring) {
			violations.MissingDocstring = append(violations.MissingDocstring, filepath.ToSlash(path))
		}

		markdownFiles, err := listColocatedMarkdownFiles(path)
		if err != nil {
			return violations, fmt.Errorf("%s: %w", path, err)
		}
		if len(markdownFiles) == 0 {
			violations.MissingMarkdown = append(violations.MissingMarkdown, filepath.ToSlash(path))
		} else {
			for _, markdownFile := range markdownFiles {
				allMarkdown[markdownFile] = struct{}{}
			}
			if missingRefs := missingModuleDocstringReferences(docstring, markdownFiles); len(missingRefs) > 0 {
				violations.MissingRefs = append(violations.MissingRefs, moduleDocsMissingRefs{
					PythonFile: filepath.ToSlash(path),
					Markdown:   append([]string{}, missingRefs...),
				})
			}
		}

		if docstring != "" {
			if refs := extractModulePathPrefixedReferences(docstring); len(refs) > 0 {
				violations.PathPrefixed = append(violations.PathPrefixed, moduleDocsPathRefs{
					PythonFile: filepath.ToSlash(path),
					Refs:       refs,
				})
			}
			if refs := nonexistentModuleReferences(path, docstring); len(refs) > 0 {
				violations.NonexistentRefs = append(violations.NonexistentRefs, moduleDocsBadRefs{
					PythonFile: filepath.ToSlash(path),
					Refs:       refs,
				})
			}
		}
	}

	allMarkdownFiles := sortedKeys(allMarkdown)
	index, err := loadModuleDocsIndex(settings)
	if err != nil {
		return violations, err
	}
	violations.MissingIndex = missingSourceDocsEntries(allMarkdownFiles, index)
	violations.BannedFilenames = bannedModuleDocFilenames(allMarkdownFiles, settings)
	sort.Strings(violations.MissingDocstring)
	sort.Strings(violations.MissingMarkdown)
	sort.Strings(violations.MissingIndex)
	return violations, nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func printModuleDocsMissingDocstring(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Modules missing docstrings!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "The following __init__.py/conftest.py files have no module docstring:")
	fmt.Fprintln(os.Stderr)
	for _, path := range violations {
		fmt.Fprintf(os.Stderr, "  - %s\n", path)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Add a module docstring at the top of each file:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, `    """Brief description of this module.`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    Longer description explaining the module's purpose,")
	fmt.Fprintln(os.Stderr, "    key classes/functions, and usage patterns.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    See Also:")
	fmt.Fprintln(os.Stderr, "        README.md: Detailed documentation for this module.")
	fmt.Fprintln(os.Stderr, `    """`)
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsMissingMarkdown(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Modules missing documentation files!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Every __init__.py/conftest.py directory MUST have at least one .md file.")
	fmt.Fprintln(os.Stderr, "The following directories have no documentation:")
	fmt.Fprintln(os.Stderr)
	for _, path := range violations {
		fmt.Fprintf(os.Stderr, "  - %s/\n", filepath.ToSlash(filepath.Dir(path)))
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Create a README.md or similar documentation file in each directory:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    # Module Name")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    Brief description of this module's purpose and usage.")
}

func printModuleDocsMissingRefs(violations []moduleDocsMissingRefs) {
	fmt.Fprintln(os.Stderr, "ERROR: Documentation reference violations found!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "When __init__.py/conftest.py files have co-located .md documentation,")
	fmt.Fprintln(os.Stderr, `the module docstring MUST include a "See Also:" section referencing those files.`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "  %s missing references to:\n", violation.PythonFile)
		for _, markdownFile := range violation.Markdown {
			fmt.Fprintf(os.Stderr, "    - %s\n", filepath.Base(markdownFile))
		}
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, `Add a "See Also:" section to the module docstring:`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, `    """Module docstring.`)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    See Also:")
	fmt.Fprintln(os.Stderr, "        FILENAME.md: Brief description of the documentation.")
	fmt.Fprintln(os.Stderr, `    """`)
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsMissingIndex(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Source documentation not indexed!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "The following .md files must be added to docs/SOURCE_DOCS.md:")
	fmt.Fprintln(os.Stderr)
	for _, path := range violations {
		fmt.Fprintf(os.Stderr, "  - %s\n", path)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Add entries to docs/SOURCE_DOCS.md:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    | `directory/` | `FILENAME.md` | Description here |")
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsPathPrefixed(violations []moduleDocsPathRefs) {
	fmt.Fprintln(os.Stderr, "ERROR: Path-prefixed documentation references found!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, `References in 'See Also:' sections must be to CO-LOCATED files only.`)
	fmt.Fprintln(os.Stderr, "Path prefixes like 'subdir/FILE.md' are an anti-pattern.")
	fmt.Fprintln(os.Stderr, "Each module should reference its own documentation, not reach into subdirs.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "  %s:\n", violation.PythonFile)
		for _, ref := range violation.Refs {
			fmt.Fprintf(os.Stderr, "    - %s\n", ref)
		}
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Fix by either:")
	fmt.Fprintln(os.Stderr, "  1. Moving the reference to the submodule's own __init__.py docstring")
	fmt.Fprintln(os.Stderr, "  2. Describing the submodule without a file path reference")
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsNonexistentRefs(violations []moduleDocsBadRefs) {
	fmt.Fprintln(os.Stderr, "ERROR: References to non-existent documentation files!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "The 'See Also:' section references .md files that do not exist.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Violations:")
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "  %s:\n", violation.PythonFile)
		for _, ref := range violation.Refs {
			fmt.Fprintf(os.Stderr, "    - %s (does not exist)\n", ref)
		}
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Fix by either:")
	fmt.Fprintln(os.Stderr, "  1. Creating the missing .md file")
	fmt.Fprintln(os.Stderr, "  2. Updating the reference to the correct filename")
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsBannedFilenames(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Banned documentation filename(s) found!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Documentation files must follow the MODULE.md naming convention:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  - Primary docs: Named after containing directory (e.g., foo/FOO.md)")
	fmt.Fprintln(os.Stderr, "  - Secondary docs: Any name EXCEPT 'README.md'")
	fmt.Fprintln(os.Stderr, "  - All docs: Must be linked in __init__.py/conftest.py AND SOURCE_DOCS.md")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Files with banned names:")
	for _, path := range violations {
		expected := strings.ToUpper(filepath.Base(filepath.Dir(path))) + ".md"
		fmt.Fprintf(os.Stderr, "  - %s\n", path)
		fmt.Fprintf(os.Stderr, "    Rename to: %s/%s\n", filepath.ToSlash(filepath.Dir(path)), expected)
	}
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsSection(printFunc func(), printed bool) bool {
	if printed {
		fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("-", 70))
	}
	printFunc()
	return true
}

func checkModuleDocsCommand(_ Config, args []string) int {
	settings, err := loadModuleDocsSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled {
		return 0
	}

	files := make([]string, 0)
	if len(args) > 0 {
		for _, path := range existingFiles(args) {
			if filepath.Ext(path) == ".py" {
				files = append(files, filepath.ToSlash(path))
			}
		}
	} else {
		files, err = discoverModuleDocsFiles(".", settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
			return 1
		}
	}

	violations, err := collectModuleDocsViolations(files, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}

	exitCode := 0
	printedSection := false
	if len(violations.MissingDocstring) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsMissingDocstring(violations.MissingDocstring) }, printedSection)
		exitCode = 1
	}
	if len(violations.MissingMarkdown) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsMissingMarkdown(violations.MissingMarkdown) }, printedSection)
		exitCode = 1
	}
	if len(violations.MissingRefs) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsMissingRefs(violations.MissingRefs) }, printedSection)
		exitCode = 1
	}
	if len(violations.MissingIndex) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsMissingIndex(violations.MissingIndex) }, printedSection)
		exitCode = 1
	}
	if len(violations.PathPrefixed) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsPathPrefixed(violations.PathPrefixed) }, printedSection)
		exitCode = 1
	}
	if len(violations.NonexistentRefs) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsNonexistentRefs(violations.NonexistentRefs) }, printedSection)
		exitCode = 1
	}
	if len(violations.BannedFilenames) > 0 {
		printedSection = printModuleDocsSection(func() { printModuleDocsBannedFilenames(violations.BannedFilenames) }, printedSection)
		exitCode = 1
	}
	return exitCode
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
