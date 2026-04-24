// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
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

type pyprojectIgnoreSettings struct {
	AllowedIgnorePatterns    []string
	AllowedExcludePatterns   []string
	AllowedMypyMissingImport []string
	Enabled                  bool
}

type pyprojectIgnoreFinding struct {
	Tool    string
	Setting string
	Target  string
	Detail  string
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
	if os.Getenv("LEFTHOOK") == "0" {
		os.Exit(0)
	}
	if len(os.Args) < minCollectionItems {
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
		filter.bannerWidth = reportDividerWidth
	}

	var err error
	filter.ansi, err = compileConfiguredRegex(
		"quiet_filter.ansi_regex",
		cfg.ANSIRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.passed, err = compileConfiguredRegex(
		"quiet_filter.passed_regex",
		cfg.PassedRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.skipped, err = compileConfiguredRegex(
		"quiet_filter.skipped_regex",
		cfg.SkippedRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.failed, err = compileConfiguredRegex(
		"quiet_filter.failed_regex",
		cfg.FailedRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.status, err = compileConfiguredRegex(
		"quiet_filter.status_regex",
		cfg.StatusRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.preexisting, err = compileConfiguredRegex(
		"quiet_filter.preexisting_regex",
		cfg.PreexistingRegex,
	)
	if err != nil {
		return filter, err
	}
	filter.separator, err = compileConfiguredRegex(
		"quiet_filter.separator_regex",
		cfg.SeparatorRegex,
	)
	if err != nil {
		return filter, err
	}
	for i, pattern := range cfg.SuppressRegexes {
		compiled, err := compileConfiguredRegex(
			fmt.Sprintf("quiet_filter.suppress_regexes[%d]", i),
			pattern,
		)
		if err != nil {
			return filter, err
		}
		filter.suppressRegexes = append(filter.suppressRegexes, compiled)
	}

	return filter, nil
}

func compileConfiguredRegex(name string, pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return regexp.MustCompile(`a^`), nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return compiled, nil
}

func runQuietFilter(filter compiledQuietFilter, input io.Reader, output io.Writer) int {
	state := newQuietFilterState(filter, output)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(
		make([]byte, 0, scannerBufferCapacity),
		scannerTokenLimit,
	)
	for scanner.Scan() {
		state.processLine(scanner.Text())
	}
	err := scanner.Err()
	if err != nil {
		fmt.Fprintf(os.Stderr, "quiet-filter: %v\n", err)

		return 1
	}

	state.printSummary()

	return 0
}

type quietFilterState struct {
	output                io.Writer
	seenBanners           map[string]bool
	filter                compiledQuietFilter
	passed                int
	skipped               int
	failed                int
	suppressHowToFix      bool
	suppressBannerContent bool
	lastWasSeparator      bool
	lastWasBlank          bool
	suppressMeta          bool
	suppressPreexisting   bool
}

func newQuietFilterState(
	filter compiledQuietFilter,
	output io.Writer,
) *quietFilterState {
	return &quietFilterState{
		filter:      filter,
		output:      output,
		seenBanners: map[string]bool{},
	}
}

func (state *quietFilterState) processLine(line string) {
	clean := state.filter.ansi.ReplaceAllString(line, "")
	if state.consumeStatus(clean) ||
		state.consumeMetadata(clean) ||
		shouldSuppressQuietLine(state.filter, clean) ||
		state.consumePreexisting(clean) ||
		state.consumeSeparator(line, clean) ||
		state.consumeBannerContent(clean) ||
		state.consumeHowToFix(line, clean) ||
		state.consumeBlank(clean) {
		return
	}

	_, _ = fmt.Fprintln(state.output, line)
}

func (state *quietFilterState) consumeStatus(clean string) bool {
	if state.filter.passed.MatchString(clean) {
		state.passed++
		state.suppressMeta = true

		return true
	}
	if state.filter.skipped.MatchString(clean) {
		state.skipped++
		state.suppressMeta = true

		return true
	}
	if state.filter.failed.MatchString(clean) {
		state.failed++
	}

	return false
}

func (state *quietFilterState) consumeMetadata(clean string) bool {
	if !state.suppressMeta {
		return false
	}
	if clean == "" || hasPrefix(clean, state.filter.metadataPrefixes) {
		return true
	}
	state.suppressMeta = false

	return false
}

func (state *quietFilterState) consumePreexisting(clean string) bool {
	if state.filter.preexisting.MatchString(clean) {
		state.suppressPreexisting = true

		return true
	}
	if !state.suppressPreexisting {
		return false
	}
	if strings.HasPrefix(clean, " ") || clean == "" {
		return true
	}
	state.suppressPreexisting = false

	return false
}

func (state *quietFilterState) consumeSeparator(line string, clean string) bool {
	if state.filter.separator.MatchString(clean) {
		state.lastWasSeparator = true

		return true
	}
	if !state.lastWasSeparator || clean == "" {
		state.lastWasSeparator = false

		return false
	}
	state.lastWasSeparator = false
	if state.isBannerHeading(clean) {
		return state.handleBannerHeading(line, clean)
	}
	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))
	_, _ = fmt.Fprintln(state.output, line)

	return true
}

func (state *quietFilterState) isBannerHeading(clean string) bool {
	return !strings.HasPrefix(clean, "-") && !startsWithDigit(clean)
}

func (state *quietFilterState) handleBannerHeading(line string, clean string) bool {
	if state.seenBanners[clean] {
		state.suppressBannerContent = true

		return true
	}
	state.seenBanners[clean] = true
	if clean == "How to fix:" {
		state.seenBanners["howtofix"] = true
	}
	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))
	_, _ = fmt.Fprintln(state.output, line)
	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))

	return true
}

func (state *quietFilterState) consumeBannerContent(clean string) bool {
	if !state.suppressBannerContent {
		return false
	}
	if state.filter.status.MatchString(clean) {
		state.suppressBannerContent = false

		return false
	}

	return true
}

func (state *quietFilterState) consumeHowToFix(line string, clean string) bool {
	if clean == "How to fix:" {
		if !state.seenBanners["howtofix"] {
			state.seenBanners["howtofix"] = true
			_, _ = fmt.Fprintln(state.output, line)
			state.suppressHowToFix = false
		} else {
			state.suppressHowToFix = true
		}

		return true
	}
	if !state.suppressHowToFix {
		return false
	}
	if strings.HasPrefix(clean, " ") || clean == "" {
		return true
	}
	state.suppressHowToFix = false

	return false
}

func (state *quietFilterState) consumeBlank(clean string) bool {
	if clean == "" {
		if state.lastWasBlank {
			return true
		}
		state.lastWasBlank = true
	} else {
		state.lastWasBlank = false
	}

	return false
}

func (state *quietFilterState) printSummary() {
	if state.failed == 0 {
		return
	}
	parts := make([]string, 0, quietSummaryParts)
	if state.passed > 0 {
		parts = append(parts, fmt.Sprintf("\033[32m%d passed\033[0m", state.passed))
	}
	parts = append(parts, fmt.Sprintf("\033[31m%d failed\033[0m", state.failed))
	if state.skipped > 0 {
		parts = append(parts, fmt.Sprintf("\033[33m%d skipped\033[0m", state.skipped))
	}
	_, _ = fmt.Fprintf(state.output, "  (%s)\n", strings.Join(parts, ", "))
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

func writeBlankLine(writer io.Writer) {
	_, _ = fmt.Fprintln(writer)
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

func loadManifestValidationSettings() (manifestValidationSettings, error) {
	var settings manifestValidationSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}
	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.manifest_validation",
		"manifest_validation",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if !sectionFound {
		return settings, nil
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
	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.plan_completion",
		"plan_completion",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if !sectionFound {
		return settings, nil
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
	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.pyproject_ignores",
		"pyproject_ignores",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if !sectionFound {
		return settings, nil
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
	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.comment_suppressions",
		"comment_suppressions",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if !sectionFound {
		return settings, nil
	}
	if len(settings.Patterns) == 0 {
		settings.Patterns = []commentSuppressionPattern{
			{Regex: `#\s*ruff:\s*noqa\b`, Label: "ruff: noqa (file-level)"},
			{
				Regex: `#\s*mypy:\s*ignore-errors\b`,
				Label: "mypy: ignore-errors (file-level)",
			},
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
	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.module_docs",
		"module_docs",
		&settings,
	)
	if err != nil {
		return settings, err
	}
	if !sectionFound {
		return settings, nil
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
		gitCommonDir(paths.ConsumerRoot),
		bundleLocalBinDirname(rootConfig),
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

func gitCommonDir(root string) string {
	if dir := gitOutput(
		"-C",
		root,
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	); dir != "" {
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

func geminiCacheKey(
	settings geminiRequestSettings,
	prompt string,
	dependency string,
) string {
	thinkingBudget := "unset"
	if settings.ThinkingBudget != nil {
		thinkingBudget = strconv.Itoa(*settings.ThinkingBudget)
	}
	payload := strings.Join(
		[]string{
			"v1",
			settings.CheckName,
			settings.Model,
			settings.ServiceTier,
			thinkingBudget,
			strconv.FormatBool(settings.DisableSafetyFilters),
			dependency,
			prompt,
		},
		"\x00",
	)
	sum := sha256.Sum256([]byte(payload))

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
	err := json.Unmarshal([]byte(stripGeminiCodeFence(responseText)), &result)
	if err != nil {
		return result, fmt.Errorf("parse Gemini JSON response: %w", err)
	}
	if result.Verdict == "" {
		result.Verdict = passVerdict
	}
	for index := range result.Violations {
		result.Violations[index].Severity = strings.ToUpper(
			strings.TrimSpace(result.Violations[index].Severity),
		)
		if result.Violations[index].Severity == "" {
			result.Violations[index].Severity = "INFO"
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

func executeGeminiChecks(
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
) []geminiCheckOutcome {
	patterns := normalizedGeminiModalAllowlistPatterns(settings)
	client := &http.Client{
		Timeout: time.Duration(settings.TimeoutSeconds) * time.Second,
	}
	explicitCacheBindings := buildGeminiExplicitCacheBindings(client, apiKey, prepared)
	outcomes, jobs := initializeGeminiOutcomesAndJobs(prepared)
	if len(jobs) == 0 {
		return outcomes
	}

	semaphore := make(chan struct{}, maxGeminiConcurrency(settings))
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

			results <- geminiBatchJobResult{
				CheckIndex: job.CheckIndex,
				BatchIndex: job.BatchIndex,
				Outcome: executeGeminiBatchJob(
					client,
					apiKey,
					job,
					explicitCacheBindings,
					patterns,
				),
			}
		}(job)
	}

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	collectGeminiBatchResults(outcomes, results)
	finalizeGeminiOutcomes(outcomes, changedLinesByFile)

	return outcomes
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

	return passVerdict
}

func formatGeminiReport(scope string, outcomes []geminiCheckOutcome) string {
	if !hasGeminiIssues(outcomes) {
		return ""
	}

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
	if report := formatGeminiReport(scope, outcomes); report != "" {
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

func isBinary(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close()
	}()
	buf := make([]byte, textChunkSize)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}

	return bytes.Contains(buf[:n], []byte{0})
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

func checkSyntax(_ Config, paths []string) int {
	failed := false
	for _, path := range existingFiles(paths) {
		if checkSyntaxPath(path) {
			continue
		}
		failed = true
	}

	return exitCode(failed)
}

func checkSyntaxPath(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)

		return false
	}

	err = decodeSyntaxFile(path, data)
	if err == nil {
		return true
	}

	fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)

	return false
}

func decodeSyntaxFile(path string, data []byte) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return decodeYAMLSyntax(data)
	case ".toml":
		return decodeTOMLSyntax(data)
	case ".json":
		return decodeJSONSyntax(data)
	default:
		return nil
	}
}

func decodeYAMLSyntax(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode YAML: %w", err)
		}
	}
}

func decodeTOMLSyntax(data []byte) error {
	var value any
	err := toml.Unmarshal(data, &value)
	if err != nil {
		return fmt.Errorf("decode TOML: %w", err)
	}

	return nil
}

func decodeJSONSyntax(data []byte) error {
	var value any
	err := json.Unmarshal(data, &value)
	if err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	return nil
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

	return "", errManifestCandidateNotFound
}

func validateManifestData(
	data map[string]any,
	settings manifestValidationSettings,
) []string {
	validationErrors := validateManifestRequiredStrings(
		data,
		settings.RequiredStringFields,
	)
	for sectionName, spec := range settings.RequiredListSections {
		validationErrors = append(
			validationErrors,
			validateManifestListSection(data, sectionName, spec)...,
		)
	}

	return validationErrors
}

func validateManifestRequiredStrings(
	data map[string]any,
	fieldNames []string,
) []string {
	validationErrors := make([]string, 0)
	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}
		value, ok := data[fieldName]
		if !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("Missing required '%s' field", fieldName),
			)

			continue
		}
		if _, ok := value.(string); !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("'%s' must be a string", fieldName),
			)
		}
	}

	return validationErrors
}

func validateManifestListSection(
	data map[string]any,
	sectionName string,
	spec manifestValidationListSpec,
) []string {
	sectionValue, hasSection := data[sectionName]
	if !hasSection || sectionValue == nil {
		if spec.Required {
			return []string{fmt.Sprintf("Missing required '%s' section", sectionName)}
		}

		return nil
	}

	entries, ok := sectionValue.([]any)
	if !ok {
		return []string{fmt.Sprintf("'%s' must be a list", sectionName)}
	}

	validationErrors := make([]string, 0)
	for index, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("%s[%d]: Expected dict, got %T", sectionName, index, entry),
			)

			continue
		}
		validationErrors = append(
			validationErrors,
			validateManifestEntryStrings(
				entryMap,
				sectionName,
				index,
				spec.RequiredStringFields,
				true,
			)...,
		)
		validationErrors = append(
			validationErrors,
			validateManifestEntryStrings(
				entryMap,
				sectionName,
				index,
				spec.OptionalStringFields,
				false,
			)...,
		)
	}

	return validationErrors
}

func validateManifestEntryStrings(
	entryMap map[string]any,
	sectionName string,
	index int,
	fieldNames []string,
	required bool,
) []string {
	validationErrors := make([]string, 0)
	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}
		value, ok := entryMap[fieldName]
		if !ok {
			if required {
				validationErrors = append(
					validationErrors,
					fmt.Sprintf("%s[%d]: Missing '%s' field", sectionName, index, fieldName),
				)
			}

			continue
		}
		if _, ok := value.(string); !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("%s[%d].%s: Expected string", sectionName, index, fieldName),
			)
		}
	}

	return validationErrors
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
	err = yaml.Unmarshal(content, &data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Invalid YAML syntax in %s:\n", manifestPath)
		fmt.Fprintf(os.Stderr, "  %v\n", err)

		return 1
	}
	if data == nil {
		fmt.Fprintf(
			os.Stderr,
			"ERROR: %s must be a YAML mapping (dict)\n",
			manifestPath,
		)

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

func stagedFiles() []string {
	output := gitOutput("diff", "--cached", "--name-only")
	if output == "" {
		return []string{}
	}

	return strings.Fields(output)
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
		return "", fmt.Errorf("read %s: %w", metadataPath, err)
	}
	var data map[string]any
	err = yaml.Unmarshal(content, &data)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", metadataPath, err)
	}

	return normalizedConfigString(data["status"]), nil
}

type uncheckedPlanItem struct {
	File string
	Text string
	Line int
}

func findUncheckedPlanItems(planDir string) ([]uncheckedPlanItem, error) {
	items := make([]uncheckedPlanItem, 0)
	pattern := regexp.MustCompile(`^-\s*\[\s*\]\s+.+`)
	err := filepath.WalkDir(
		planDir,
		func(path string, entry os.DirEntry, walkErr error) error {
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
				return fmt.Errorf("read %s: %w", path, err)
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
		},
	)

	if err != nil {
		return items, fmt.Errorf("walk %s: %w", planDir, err)
	}

	return items, nil
}

func checkPlanCompletionErrors(
	metadataPath string,
	settings planCompletionSettings,
) ([]string, error) {
	status, err := planStatus(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("read %s status: %w", metadataPath, err)
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
		return nil, fmt.Errorf("scan %s plan items: %w", planDir, err)
	}
	if len(unchecked) == 0 {
		return []string{}, nil
	}

	errors := []string{
		"",
		strings.Repeat("=", compactDividerWidth),
		"PLAN COMPLETION FRAUD DETECTED",
		strings.Repeat("=", compactDividerWidth),
		"",
		"Plan: " + filepath.Base(planDir),
		"Claimed status: " + status,
		"",
		"But these items are still unchecked:",
	}
	for _, item := range unchecked {
		relative, relErr := filepath.Rel(planDir, item.File)
		if relErr != nil {
			relative = item.File
		}
		errors = append(
			errors,
			fmt.Sprintf("  %s:%d: %s", relative, item.Line, item.Text),
		)
	}
	errors = append(
		errors,
		"",
		strings.Repeat("=", compactDividerWidth),
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
		strings.Repeat("=", compactDividerWidth),
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
		paths = stagedFiles()
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
		return fmt.Sprintf(
			"%s %s: %s -> %s",
			finding.Tool,
			finding.Setting,
			finding.Target,
			finding.Detail,
		)
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
				addPyprojectFinding(findings, tool, setting, pattern, "<all>")

				continue
			}
			for _, code := range codeList {
				addPyprojectFinding(findings, tool, setting, pattern, code)
			}
		}

		return
	}
	for _, entry := range normalizeStringList(value) {
		addPyprojectFinding(findings, tool, setting, entry, "")
	}
}

func addPyprojectPatternFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	value any,
) {
	for _, pattern := range normalizeStringList(value) {
		addPyprojectFinding(findings, tool, setting, pattern, "")
	}
}

func addPyprojectFinding(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	target string,
	detail string,
) {
	findings[pyprojectIgnoreFinding{
		Tool:    tool,
		Setting: setting,
		Target:  target,
		Detail:  detail,
	}] = struct{}{}
}

func ruffPerFileIgnoreKeys() []string {
	return []string{
		"per-file-ignores",
		"extend-per-file-ignores",
		"per_file_ignores",
		"extend_per_file_ignores",
	}
}

func mypyIgnoreKeys() []string {
	return []string{
		"ignore_errors",
		"ignore_missing_imports",
		"disable_error_code",
		"disable_error_codes",
	}
}

func pylintIgnoreKeys() []string {
	return []string{
		"ignore",
		"ignore-patterns",
		"ignore-paths",
		"ignore-modules",
		"ignored-modules",
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
	for _, key := range ruffPerFileIgnoreKeys() {
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
	for _, key := range mypyIgnoreKeys() {
		value, ok := override[key]
		if !ok {
			continue
		}
		addMypyOverrideFindingsForKey(findings, modules, key, value)
	}
}

func addMypyOverrideFindingsForKey(
	findings map[pyprojectIgnoreFinding]struct{},
	modules []string,
	key string,
	value any,
) {
	switch key {
	case "disable_error_code", "disable_error_codes":
		addMypyOverrideErrorCodeFindings(findings, modules, key, value)
	default:
		addMypyOverrideGenericFindings(findings, modules, key, value)
	}
}

func addMypyOverrideErrorCodeFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	modules []string,
	key string,
	value any,
) {
	for _, code := range normalizeStringList(value) {
		if strings.TrimSpace(code) == "" {
			continue
		}
		for _, module := range modules {
			addPyprojectFinding(
				findings,
				"mypy",
				"override."+key,
				module,
				code,
			)
		}
	}
}

func addMypyOverrideGenericFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	modules []string,
	key string,
	value any,
) {
	if boolean, ok := value.(bool); ok {
		if boolean {
			for _, module := range modules {
				addPyprojectFinding(findings, "mypy", "override."+key, module, "")
			}
		}

		return
	}
	for _, module := range modules {
		addPyprojectFinding(
			findings,
			"mypy",
			"override."+key,
			module,
			fmt.Sprint(value),
		)
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

func extractPyrightFindings(
	toolTable map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
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

func extractPylintFindings(
	toolTable map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
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
		for _, key := range pylintIgnoreKeys() {
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

func extractPyprojectFindings(
	config map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
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
		if isExcludeSetting(finding.Setting) &&
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

		return fmt.Sprintf(
			"%s|%s|%s|%s",
			left.Tool,
			left.Setting,
			left.Target,
			left.Detail,
		) <
			fmt.Sprintf(
				"%s|%s|%s|%s",
				right.Tool,
				right.Setting,
				right.Target,
				right.Detail,
			)
	})

	return filtered
}

func isExcludeSetting(setting string) bool {
	return setting == "exclude" ||
		setting == "extend-exclude" ||
		setting == "extend_exclude"
}

func loadPyprojectConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}
	var config map[string]any
	err = toml.Unmarshal(data, &config)
	if err != nil {
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

		findings := filterAllowedPyprojectFindings(
			extractPyprojectFindings(config),
			settings,
		)
		if len(findings) == 0 {
			continue
		}
		hasErrors = true
		fmt.Fprintf(
			os.Stderr,
			"ERROR: %s contains forbidden linter file ignores:\n",
			path,
		)
		for _, finding := range findings {
			fmt.Fprintf(os.Stderr, "  %s\n", finding.render())
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", compactDividerWidth))
		fmt.Fprintln(os.Stderr, "Pyproject linter ignore check FAILED")
		fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", compactDividerWidth))
		fmt.Fprintln(
			os.Stderr,
			"Move file-specific ignores into the files themselves with "+
				"documentation (e.g., # noqa / # type: ignore[code]).",
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
			return nil, fmt.Errorf(
				"invalid comment suppression regex %q: %w",
				expr,
				err,
			)
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
	scanner := newPythonCommentScanner(text)

	return scanner.scan()
}

type pythonCommentScanState int

const (
	scanNormal pythonCommentScanState = iota
	scanSingleQuote
	scanDoubleQuote
	scanTripleSingleQuote
	scanTripleDoubleQuote
)

type pythonCommentScanner struct {
	text       string
	violations []commentSuppressionViolation
	state      pythonCommentScanState
	line       int
	cursor     int
}

func newPythonCommentScanner(text string) *pythonCommentScanner {
	return &pythonCommentScanner{
		text:       text,
		violations: make([]commentSuppressionViolation, 0),
		state:      scanNormal,
		line:       1,
	}
}

func (scanner *pythonCommentScanner) scan() []commentSuppressionViolation {
	for scanner.cursor < len(scanner.text) {
		scanner.step()
	}

	return scanner.violations
}

func (scanner *pythonCommentScanner) step() {
	switch scanner.state {
	case scanNormal:
		scanner.stepNormal()
	case scanSingleQuote:
		scanner.stepQuoted('\'')
	case scanDoubleQuote:
		scanner.stepQuoted('"')
	case scanTripleSingleQuote:
		scanner.stepTripleQuoted('\'')
	case scanTripleDoubleQuote:
		scanner.stepTripleQuoted('"')
	}
}

func (scanner *pythonCommentScanner) stepNormal() {
	currentChar := scanner.text[scanner.cursor]
	switch currentChar {
	case '#':
		scanner.recordComment()
	case '\'':
		scanner.enterQuote(scanSingleQuote, scanTripleSingleQuote, '\'')
	case '"':
		scanner.enterQuote(scanDoubleQuote, scanTripleDoubleQuote, '"')
	case '\n':
		scanner.line++
		scanner.cursor++
	default:
		scanner.cursor++
	}
}

func (scanner *pythonCommentScanner) recordComment() {
	start := scanner.cursor
	for scanner.cursor < len(scanner.text) &&
		scanner.text[scanner.cursor] != '\n' {
		scanner.cursor++
	}
	scanner.violations = append(scanner.violations, commentSuppressionViolation{
		Line:    scanner.line,
		Comment: strings.TrimSpace(scanner.text[start:scanner.cursor]),
	})
}

func (scanner *pythonCommentScanner) enterQuote(
	singleState pythonCommentScanState,
	tripleState pythonCommentScanState,
	quote byte,
) {
	if scanner.hasTripleQuote(quote) {
		scanner.state = tripleState
		scanner.cursor += tripleQuoteLen

		return
	}
	scanner.state = singleState
	scanner.cursor++
}

func (scanner *pythonCommentScanner) stepQuoted(quote byte) {
	currentChar := scanner.text[scanner.cursor]
	switch currentChar {
	case '\\':
		scanner.advanceEscaped()
	case '\n':
		scanner.line++
		scanner.state = scanNormal
		scanner.cursor++
	case quote:
		scanner.state = scanNormal
		scanner.cursor++
	default:
		scanner.cursor++
	}
}

func (scanner *pythonCommentScanner) advanceEscaped() {
	if scanner.cursor+1 < len(scanner.text) &&
		scanner.text[scanner.cursor+1] == '\n' {
		scanner.line++
	}
	scanner.cursor += splitNParts
}

func (scanner *pythonCommentScanner) stepTripleQuoted(quote byte) {
	if scanner.text[scanner.cursor] == '\n' {
		scanner.line++
	}
	if scanner.hasTripleQuote(quote) {
		scanner.state = scanNormal
		scanner.cursor += tripleQuoteLen

		return
	}
	scanner.cursor++
}

func (scanner *pythonCommentScanner) hasTripleQuote(quote byte) bool {
	return scanner.cursor+2 < len(scanner.text) &&
		scanner.text[scanner.cursor] == quote &&
		scanner.text[scanner.cursor+1] == quote &&
		scanner.text[scanner.cursor+2] == quote
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
		if filepath.Ext(path) != extPy {
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

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "COMMENT-BASED LINT SUPPRESSION DETECTED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(
		os.Stderr,
		"Comment-based suppressions (noqa, type: ignore, pragma, etc.)",
	)
	fmt.Fprintln(
		os.Stderr,
		"are banned. Fix the underlying issue instead of suppressing it.",
	)
	fmt.Fprintln(
		os.Stderr,
		"Per ETHOS §14: linters are not suggestions, they are enforcement.",
	)
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
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

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

func discoverModuleDocsFiles(
	root string,
	settings moduleDocsSettings,
) ([]string, error) {
	matches := make([]string, 0)
	excluded := stringSet(settings.ExcludedDirs)
	err := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, walkErr error) error {
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
		},
	)
	if err != nil {
		return nil, fmt.Errorf("walk module docs files: %w", err)
	}
	sort.Strings(matches)

	return matches, nil
}

func listColocatedMarkdownFiles(path string) ([]string, error) {
	directory := filepath.Dir(path)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", directory, err)
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(
				files,
				filepath.ToSlash(filepath.Join(directory, entry.Name())),
			)
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
	for index < len(text) && isASCIIAlpha(text[index]) {
		index++
	}
	if !isModuleDocstringPrefix(text[start:index]) {
		return "", nil
	}
	if index >= len(text) {
		return "", nil
	}
	quote := text[index]
	if quote != '\'' && quote != '"' {
		return "", nil
	}
	triple := index+minCollectionItems < len(text) &&
		text[index+1] == quote &&
		text[index+2] == quote
	if triple {
		return parseTripleQuotedDocstring(text, index, quote)
	}

	return parseSingleQuotedDocstring(text, index, quote)
}

func isASCIIAlpha(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isModuleDocstringPrefix(prefix string) bool {
	for _, char := range strings.ToLower(prefix) {
		if char != 'r' && char != 'u' {
			return false
		}
	}

	return true
}

func parseTripleQuotedDocstring(
	text string,
	index int,
	quote byte,
) (string, error) {
	contentStart := index + tripleQuoteLen
	for cursor := contentStart; cursor+2 < len(text); cursor++ {
		if text[cursor] == '\\' {
			cursor++

			continue
		}
		if text[cursor] == quote &&
			text[cursor+1] == quote &&
			text[cursor+2] == quote {
			return text[contentStart:cursor], nil
		}
	}

	return "", errUnterminatedTripleDoc
}

func parseSingleQuotedDocstring(
	text string,
	index int,
	quote byte,
) (string, error) {
	contentStart := index + 1
	for cursor := contentStart; cursor < len(text); cursor++ {
		switch text[cursor] {
		case '\\':
			cursor++
		case '\n':
			return "", errUnterminatedModuleDoc
		case quote:
			return text[contentStart:cursor], nil
		}
	}

	return "", errUnterminatedModuleDoc
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
		if len(match) >= minCollectionItems {
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
		if len(match) >= minCollectionItems {
			refs[match[1]] = struct{}{}
		}
	}

	return sortedKeys(refs)
}

func missingModuleDocstringReferences(
	docstring string,
	markdownFiles []string,
) []string {
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
		refPath := filepath.Join(directory, ref)
		_, err := os.Stat(refPath)
		if errors.Is(err, os.ErrNotExist) {
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

		return "", fmt.Errorf("read %s: %w", settings.SourceDocsPath, err)
	}

	return string(data), nil
}

func missingSourceDocsEntries(markdownFiles []string, index string) []string {
	if strings.TrimSpace(index) == "" {
		return append([]string{}, markdownFiles...)
	}
	missing := make([]string, 0)
	for _, markdownFile := range markdownFiles {
		directory := strings.TrimPrefix(
			filepath.ToSlash(filepath.Dir(markdownFile))+"/",
			"./",
		)
		name := filepath.Base(markdownFile)
		if !strings.Contains(index, directory) || !strings.Contains(index, name) {
			missing = append(missing, markdownFile)
		}
	}

	return missing
}

func bannedModuleDocFilenames(
	markdownFiles []string,
	settings moduleDocsSettings,
) []string {
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
		err := collectModuleDocsFileViolations(path, settings, &violations, allMarkdown)
		if err != nil {
			return violations, fmt.Errorf("%s: %w", path, err)
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

func collectModuleDocsFileViolations(
	path string,
	settings moduleDocsSettings,
	violations *moduleDocsViolations,
	allMarkdown map[string]struct{},
) error {
	if !shouldCheckModuleDocsFile(path, settings) {
		return nil
	}

	docstring, err := extractModuleDocstringFromFile(path)
	if err != nil {
		return err
	}
	if !hasMeaningfulModuleDocstring(docstring) {
		violations.MissingDocstring = append(
			violations.MissingDocstring,
			filepath.ToSlash(path),
		)
	}

	markdownFiles, err := listColocatedMarkdownFiles(path)
	if err != nil {
		return err
	}
	collectModuleMarkdownViolations(
		path,
		docstring,
		markdownFiles,
		violations,
		allMarkdown,
	)
	collectModuleDocstringReferenceViolations(path, docstring, violations)

	return nil
}

func collectModuleMarkdownViolations(
	path string,
	docstring string,
	markdownFiles []string,
	violations *moduleDocsViolations,
	allMarkdown map[string]struct{},
) {
	if len(markdownFiles) == 0 {
		violations.MissingMarkdown = append(
			violations.MissingMarkdown,
			filepath.ToSlash(path),
		)

		return
	}

	for _, markdownFile := range markdownFiles {
		allMarkdown[markdownFile] = struct{}{}
	}
	missingRefs := missingModuleDocstringReferences(docstring, markdownFiles)
	if len(missingRefs) > 0 {
		violations.MissingRefs = append(violations.MissingRefs, moduleDocsMissingRefs{
			PythonFile: filepath.ToSlash(path),
			Markdown:   append([]string{}, missingRefs...),
		})
	}
}

func collectModuleDocstringReferenceViolations(
	path string,
	docstring string,
	violations *moduleDocsViolations,
) {
	if docstring == "" {
		return
	}

	if refs := extractModulePathPrefixedReferences(docstring); len(refs) > 0 {
		violations.PathPrefixed = append(
			violations.PathPrefixed,
			moduleDocsPathRefs{
				PythonFile: filepath.ToSlash(path),
				Refs:       refs,
			},
		)
	}
	if refs := nonexistentModuleReferences(path, docstring); len(refs) > 0 {
		violations.NonexistentRefs = append(
			violations.NonexistentRefs,
			moduleDocsBadRefs{
				PythonFile: filepath.ToSlash(path),
				Refs:       refs,
			},
		)
	}
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
	fmt.Fprintln(
		os.Stderr,
		"The following __init__.py/conftest.py files have no module docstring:",
	)
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
	fmt.Fprintln(
		os.Stderr,
		"        MODULE_NAME.md: Detailed documentation for this module.",
	)
	fmt.Fprintln(os.Stderr, `    """`)
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsMissingMarkdown(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Modules missing documentation files!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"Every __init__.py/conftest.py directory MUST have at least one .md file.",
	)
	fmt.Fprintln(os.Stderr, "The following directories have no documentation:")
	fmt.Fprintln(os.Stderr)
	for _, path := range violations {
		fmt.Fprintf(os.Stderr, "  - %s/\n", filepath.ToSlash(filepath.Dir(path)))
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Create a directory-named markdown file in each directory:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    DIRECTORY_NAME.md")
	fmt.Fprintln(os.Stderr, "    # Module Name")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "    Brief description of this module's purpose and usage.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"Avoid README.md here; this hook bans generic README names for module docs.",
	)
}

func printModuleDocsMissingRefs(violations []moduleDocsMissingRefs) {
	fmt.Fprintln(os.Stderr, "ERROR: Documentation reference violations found!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"When __init__.py/conftest.py files have co-located .md documentation,",
	)
	fmt.Fprintln(
		os.Stderr,
		`the module docstring MUST include a "See Also:" section referencing those files.`,
	)
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
	fmt.Fprintln(
		os.Stderr,
		"        FILENAME.md: Brief description of the documentation.",
	)
	fmt.Fprintln(os.Stderr, `    """`)
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsMissingIndex(violations []string) {
	fmt.Fprintln(os.Stderr, "ERROR: Source documentation not indexed!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"The following .md files must be added to docs/SOURCE_DOCS.md:",
	)
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
	fmt.Fprintln(
		os.Stderr,
		`References in 'See Also:' sections must be to CO-LOCATED files only.`,
	)
	fmt.Fprintln(os.Stderr, "Path prefixes like 'subdir/FILE.md' are an anti-pattern.")
	fmt.Fprintln(
		os.Stderr,
		"Each module should reference its own documentation, not reach into subdirs.",
	)
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
	fmt.Fprintln(
		os.Stderr,
		"  1. Moving the reference to the submodule's own __init__.py docstring",
	)
	fmt.Fprintln(
		os.Stderr,
		"  2. Describing the submodule without a file path reference",
	)
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsNonexistentRefs(violations []moduleDocsBadRefs) {
	fmt.Fprintln(os.Stderr, "ERROR: References to non-existent documentation files!")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"The 'See Also:' section references .md files that do not exist.",
	)
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
	fmt.Fprintln(
		os.Stderr,
		"Documentation files must follow the MODULE.md naming convention:",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(
		os.Stderr,
		"  - Primary docs: Named after containing directory (e.g., foo/FOO.md)",
	)
	fmt.Fprintln(os.Stderr, "  - Secondary docs: Any name EXCEPT 'README.md'")
	fmt.Fprintln(
		os.Stderr,
		"  - All docs: Must be linked in __init__.py/conftest.py AND SOURCE_DOCS.md",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Files with banned names:")
	for _, path := range violations {
		expected := strings.ToUpper(filepath.Base(filepath.Dir(path))) + ".md"
		fmt.Fprintf(os.Stderr, "  - %s\n", path)
		fmt.Fprintf(
			os.Stderr,
			"    Rename to: %s/%s\n",
			filepath.ToSlash(filepath.Dir(path)),
			expected,
		)
	}
	fmt.Fprintln(os.Stderr)
}

func printModuleDocsSection(printFunc func(), printed bool) bool {
	if printed {
		fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("-", reportDividerWidth))
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

	files, err := moduleDocsCommandFiles(args, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	violations, err := collectModuleDocsViolations(files, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return reportModuleDocsViolations(violations)
}

func moduleDocsCommandFiles(
	args []string,
	settings moduleDocsSettings,
) ([]string, error) {
	if len(args) == 0 {
		return discoverModuleDocsFiles(".", settings)
	}

	files := make([]string, 0)
	for _, path := range existingFiles(args) {
		if filepath.Ext(path) == extPy {
			files = append(files, filepath.ToSlash(path))
		}
	}

	return files, nil
}

func reportModuleDocsViolations(violations moduleDocsViolations) int {
	exitCode := 0
	printedSection := false
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.MissingDocstring) > 0,
		func() { printModuleDocsMissingDocstring(violations.MissingDocstring) },
		printedSection,
		exitCode,
	)
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.MissingMarkdown) > 0,
		func() { printModuleDocsMissingMarkdown(violations.MissingMarkdown) },
		printedSection,
		exitCode,
	)
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.MissingRefs) > 0,
		func() { printModuleDocsMissingRefs(violations.MissingRefs) },
		printedSection,
		exitCode,
	)
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.MissingIndex) > 0,
		func() { printModuleDocsMissingIndex(violations.MissingIndex) },
		printedSection,
		exitCode,
	)
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.PathPrefixed) > 0,
		func() { printModuleDocsPathPrefixed(violations.PathPrefixed) },
		printedSection,
		exitCode,
	)
	printedSection, exitCode = printModuleDocsViolationSection(
		len(violations.NonexistentRefs) > 0,
		func() { printModuleDocsNonexistentRefs(violations.NonexistentRefs) },
		printedSection,
		exitCode,
	)
	_, exitCode = printModuleDocsViolationSection(
		len(violations.BannedFilenames) > 0,
		func() { printModuleDocsBannedFilenames(violations.BannedFilenames) },
		printedSection,
		exitCode,
	)

	return exitCode
}

func printModuleDocsViolationSection(
	hasViolations bool,
	printer func(),
	printedSection bool,
	exitCode int,
) (bool, int) {
	if !hasViolations {
		return printedSection, exitCode
	}

	printedSection = printModuleDocsSection(printer, printedSection)

	return printedSection, 1
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
					fmt.Fprintf(
						os.Stderr,
						"%s: unresolved merge conflict marker\n",
						path,
					)
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
		executable := info.Mode()&executePermissionMask != 0
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
	maxBytes := int64(cfg.Text.MaxLargeFileKB * kibibyte)
	for _, path := range existingFiles(paths) {
		if !suffixes[strings.ToLower(filepath.Ext(path))] ||
			hasPrefix(path, cfg.Text.LargeFileExcludePrefixes) {
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
			fmt.Fprintf(
				os.Stderr,
				"%s: %d KiB exceeds %d KiB limit\n",
				path,
				info.Size()/kibibyte,
				cfg.Text.MaxLargeFileKB,
			)
			failed = true
		}
	}

	return exitCode(failed)
}

func forbiddenStringExemptPath() string {
	bundleRoot, err := findBundleRoot()
	if err != nil {
		return ""
	}

	return filepath.Clean(filepath.Join(filepath.Dir(bundleRoot), "config.yaml"))
}

func isForbiddenStringExempt(path string, exemptPath string) bool {
	if strings.TrimSpace(exemptPath) == "" {
		return false
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	return filepath.Clean(absolutePath) == filepath.Clean(exemptPath)
}

func checkForbiddenStrings(cfg Config, paths []string) int {
	failed := false
	exemptPath := forbiddenStringExemptPath()
	for _, path := range existingFiles(paths) {
		if isForbiddenStringExempt(path, exemptPath) {
			continue
		}
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
				fmt.Fprintf(
					os.Stderr,
					"ERROR: %s: contains forbidden string %q\n",
					path,
					forbidden,
				)
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
		fmt.Fprintln(
			os.Stderr,
			"FATAL: shellcheck is required. Install: apt/brew install shellcheck",
		)

		return 1
	}
	args := append([]string{"--severity=warning", "-x"}, files...)
	cmd := exec.CommandContext(context.Background(), shellcheck, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return 1
	}

	return 0
}

func checkShellBestPractices(cfg Config, paths []string) int {
	failed := false
	setPattern := regexp.MustCompile(
		`(?m)^\s*set\s+-[euo]+\s*pipefail|^\s*set\s+-euo\s+pipefail`,
	)
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
		if hasPrefix(path, cfg.Shell.RequireCommonForPrefixes) &&
			!commonPattern.MatchString(text) {
			errs = append(
				errs,
				"scripts/ shell files must source the repository common shell helpers",
			)
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
			writef(os.Stderr, "ERROR: %s: Could not read file: %v\n", path, err)
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
			writef(
				os.Stderr,
				"ERROR: %s: New file has %d lines (limit: %d)\n",
				path,
				lineCount,
				hardLimit,
			)
			failed = true
		} else if lineCount > originalCount {
			writef(
				os.Stderr,
				"ERROR: %s: File grew from %d to %d lines (over %d limit). "+
					"Must refactor to reduce size.\n",
				path,
				originalCount,
				lineCount,
				hardLimit,
			)
			failed = true
		}
	}
	if failed {
		writeBlankLine(os.Stdout)
		writeLine(os.Stdout, strings.Repeat("=", compactDividerWidth))
		writeLine(os.Stdout, "File size check FAILED")
		writeLine(os.Stdout, strings.Repeat("=", compactDividerWidth))
		writeBlankLine(os.Stdout)
		writeLine(os.Stdout, "Refactoring suggestions:")
		writeLine(os.Stdout, "  - Extract helper functions to separate modules")
		writeLine(os.Stdout, "  - Split large files into focused submodules")
		writeLine(os.Stdout, "  - Move reusable code to lib/")
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
	fmt.Fprintln(
		os.Stderr,
		"Commit message does not satisfy conventional commit rules:",
	)
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "  - %s\n", err)
	}

	return 1
}

func checkCommitAttribution(cfg Config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(
			os.Stderr,
			"Usage: coding-ethos-hook commit-attribution <commit-msg-file>",
		)

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
				violations = append(
					violations,
					fmt.Sprintf("  Line %d: %q in: %s", lineNo+1, match, trimmed),
				)

				break
			}
		}
	}
	if len(violations) == 0 {
		return 0
	}
	writeLine(os.Stdout, strings.Repeat("=", compactDividerWidth))
	writeLine(os.Stdout, "COMMIT MESSAGE CONTAINS FORBIDDEN AI ATTRIBUTION")
	writeLine(os.Stdout, strings.Repeat("=", compactDividerWidth))
	writeBlankLine(os.Stdout)
	writeLine(os.Stdout, "Per ETHOS §16 (No Self-Promotion), commit messages must not")
	writeLine(
		os.Stdout,
		"contain AI co-author lines, attribution, or promotional content.",
	)
	writeBlankLine(os.Stdout)
	writeLine(os.Stdout, "Violations found:")
	for _, violation := range violations {
		writeLine(os.Stdout, violation)
	}
	writeBlankLine(os.Stdout)
	writeLine(os.Stdout, "Remove the AI attribution and commit again.")
	writeLine(os.Stdout, strings.Repeat("=", compactDividerWidth))

	return 1
}

func commitMessageLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read commit message %s: %w", path, err)
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
	lines = trimLeadingBlankCommitLines(lines)
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
		errs = append(
			errs,
			fmt.Sprintf(
				"header must be <= %d characters",
				cfg.CommitLint.MaxHeaderLength,
			),
		)
	}
	match := regexp.MustCompile(`^([a-z]+)\(([A-Za-z0-9_.-]+)\)!?: (.+)$`).
		FindStringSubmatch(header)
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
	if hasCommitBodyOrFooter(lines) &&
		len(lines) > 1 &&
		strings.TrimSpace(lines[1]) != "" {
		errs = append(errs, "body/footer must be separated from header by a blank line")
	}

	return errs
}

func trimLeadingBlankCommitLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	return lines
}

func hasCommitBodyOrFooter(lines []string) bool {
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}

	return false
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
	if ext == extShell || ext == extBash {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")

	return strings.HasPrefix(firstLine, "#!") &&
		(strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}

func validShellShebang(text string) bool {
	firstLine, _, _ := strings.Cut(text, "\n")

	return strings.HasPrefix(firstLine, "#!") &&
		(strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}

func isLineLimited(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".py" || ext == ".sh" || ext == extBash ||
		strings.Contains(path, "scripts/")
}

func limitsForFile(cfg Config, path string) (int, int) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".py" {
		return cfg.LineLimits.PythonHard, cfg.LineLimits.PythonWarn
	}
	if ext == ".sh" || ext == extBash || strings.Contains(path, "scripts/") {
		return cfg.LineLimits.ShellHard, cfg.LineLimits.ShellWarn
	}

	return cfg.LineLimits.PythonHard, cfg.LineLimits.PythonWarn
}

func originalLineCount(path string) int {
	cmd := exec.CommandContext(context.Background(), "git", "show", "HEAD:"+path)
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
	cmd := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=A",
		"--",
		path,
	)
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
