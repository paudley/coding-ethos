// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

const (
	configEnv          = "CODE_ETHOS_PRECOMMIT_CONFIG"
	precommitRootEnv   = "CODE_ETHOS_PRECOMMIT_ROOT"
	consumerRootEnv    = "CODE_ETHOS_CONSUMER_ROOT"
	localRootEnv       = "CODE_ETHOS_LOCAL_ROOT"
	privateKeyPattern  = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	textChunkSize      = 8192
	severityCritical   = "CRITICAL"
	severityInfo       = "INFO"
	severityWarning    = "WARNING"
	hookStagePreCommit = "pre-commit"
	hookStagePrePush   = "pre-push"
)

type Config struct {
	QuietFilter       QuietFilterConfig
	HookStage         string
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

func usage() {
	writeText(
		os.Stderr,
		strings.Join([]string{
			"Usage: coding-ethos-hook <command> [files...]",
			"  gemini-check supports --dry-run, --full-check, and --check-type <name>",
			"  config-get <dot.path> [default] prints merged config values",
		}, "\n"),
	)
}

func normalizeConfigKey(value string) string {
	replacer := strings.NewReplacer("_", "", "-", "", ".", "")

	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func decodeYAMLValue(value, target any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		MatchName: func(mapKey, fieldName string) bool {
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

func writeLine(writer io.Writer, values ...any) {
	feedback.Emit(
		writer,
		feedback.Text{Text: strings.TrimSuffix(fmt.Sprintln(values...), "\n")},
		feedback.FormatTOON,
	)
}

func writef(writer io.Writer, format string, args ...any) {
	writeText(writer, fmt.Sprintf(format, args...))
}

func writeText(writer io.Writer, text string) {
	if text == "" {
		return
	}

	if trimmed, found := strings.CutSuffix(text, "\n"); found {
		text = trimmed
	}

	feedback.Emit(writer, feedback.Text{Text: text}, feedback.FormatTOON)
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
	return gitOutputInRoot("", args...)
}

func gitOutputInRoot(root string, args ...string) string {
	return gitCommandOutput(evaluators.GitCommand(root, args...))
}

func gitCommandOutput(cmd interface{ Output() ([]byte, error) }) string {
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func repoRoot() string {
	if root := strings.TrimSpace(os.Getenv(consumerRootEnv)); root != "" {
		cwd, err := os.Getwd()
		if err == nil && explicitConsumerRootApplies(root, cwd) {
			return root
		}
	}

	if root := gitOutput("rev-parse", "--show-toplevel"); root != "" {
		cwd, err := os.Getwd()
		if err != nil || explicitConsumerRootApplies(root, cwd) {
			return root
		}
	}

	return "."
}

func localRepoRoot() string {
	if root := strings.TrimSpace(os.Getenv(localRootEnv)); root != "" {
		return root
	}

	return repoRoot()
}

func consumerRoot(ethosRoot string) string {
	return resolveConsumerRoot(
		ethosRoot,
		os.Getenv(consumerRootEnv),
		gitOutput("-C", ethosRoot, "rev-parse", "--show-toplevel"),
		gitOutput("-C", ethosRoot, "rev-parse", "--show-superproject-working-tree"),
	)
}

func resolveConsumerRoot(
	ethosRoot string,
	explicitRoot string,
	gitTopLevel string,
	superprojectRoot string,
) string {
	if root := strings.TrimSpace(explicitRoot); root != "" {
		if explicitConsumerRootApplies(root, ethosRoot) {
			return root
		}
	}

	if root := strings.TrimSpace(gitTopLevel); root != "" {
		if explicitConsumerRootApplies(root, ethosRoot) {
			return root
		}
	}

	if root := strings.TrimSpace(superprojectRoot); root != "" {
		if explicitConsumerRootApplies(root, ethosRoot) {
			return root
		}
	}

	return ethosRoot
}

func explicitConsumerRootApplies(root, ethosRoot string) bool {
	absRoot, rootErr := filepath.Abs(root)

	absEthosRoot, ethosErr := filepath.Abs(ethosRoot)
	if rootErr != nil || ethosErr != nil {
		return root == ethosRoot
	}

	rel, err := filepath.Rel(absRoot, absEthosRoot)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	if relativePathIsRuntimeScratch(rel) {
		return false
	}

	return !gitPathIgnoredByRoot(absRoot, absEthosRoot)
}

func relativePathIsRuntimeScratch(relative string) bool {
	rel := "/" + filepath.ToSlash(filepath.Clean(relative)) + "/"

	return strings.Contains(rel, "/.coding-ethos/cache/sandbox-tmp/") ||
		strings.Contains(rel, "/.coding-ethos/")
}

func gitPathIgnoredByRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == "" ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	cmd := evaluators.GitCommand(
		root,
		"check-ignore",
		"--quiet",
		"--",
		filepath.ToSlash(rel),
	)

	return cmd.Run() == nil
}

func consumerRuntimeCacheDir(root string) string {
	return filepath.Join(root, ".coding-ethos", "cache")
}

func isBundleRoot(path string) bool {
	info, err := statRootedFile(filepath.Join(path, "hooks", "managed-toolchain.tsv"))
	if err != nil || info.IsDir() {
		return false
	}

	hookProject, err := statRootedFile(filepath.Join(path, "hooks", "pyproject.toml"))

	return err == nil && !hookProject.IsDir()
}

func findBundleRoot() (string, error) {
	if envRoot := strings.TrimSpace(os.Getenv(precommitRootEnv)); envRoot != "" {
		if isBundleRoot(envRoot) {
			return envRoot, nil
		}
	}

	if explicitRoot := strings.TrimSpace(os.Getenv(consumerRootEnv)); explicitRoot != "" {
		if bundleRoot, found := findBundleRootFromRoot(explicitRoot); found {
			return bundleRoot, nil
		}

		return "", fmt.Errorf("%w: %s", errBundleRootNotFound, explicitRoot)
	}

	root := repoRoot()
	if bundleRoot, found := findBundleRootFromRoot(root); found {
		return bundleRoot, nil
	}

	if bundleRoot, found := findBundleRootFromSource(); found {
		return bundleRoot, nil
	}

	return "", fmt.Errorf("%w: %s", errBundleRootNotFound, root)
}

func findBundleRootFromRoot(root string) (string, bool) {
	for _, candidate := range []string{"code-ethos/pre-commit", "pre-commit"} {
		resolved := filepath.Join(root, candidate)
		if isBundleRoot(resolved) {
			return resolved, true
		}
	}

	return "", false
}

func findBundleRootFromSource() (string, bool) {
	root, found := sourceRepoRoot()
	if !found {
		return "", false
	}

	return findBundleRootFromRoot(root)
}

func sourceRepoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}

	for current := filepath.Dir(file); ; current = filepath.Dir(current) {
		if isBundleRoot(filepath.Join(current, "pre-commit")) {
			return current, true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
	}
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

	data, err := readRootedFile(path)
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

func deepMerge(base, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base))
	maps.Copy(merged, base)

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

	for part := range strings.SplitSeq(strings.TrimSpace(path), ".") {
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
		writef(os.Stderr, "FATAL: %v\n", err)

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

		info, err := statRootedFile(path)
		if err == nil && !info.IsDir() {
			seen[path] = struct{}{}
			files = append(files, path)
		}
	}

	return files
}

func readText(path string) (string, bool, error) {
	data, err := readRootedFile(path)
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
			writef(os.Stderr, "%s: %v\n", path, err)

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
			err := writeRootedFile(path, []byte(fixed), hookRewriteFilePerm)
			if err != nil {
				writef(os.Stderr, "%s: %v\n", path, err)

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
