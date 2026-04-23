package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
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

	bundleRoot, err := findBundleRoot()
	if err != nil {
		return cfg, err
	}
	rootConfig, err := loadYAMLMap(filepath.Join(filepath.Dir(bundleRoot), "config.yaml"))
	if err != nil {
		return cfg, err
	}

	if overridePath := strings.TrimSpace(os.Getenv(configEnv)); overridePath != "" {
		overrideConfig, err := loadYAMLMap(overridePath)
		if err != nil {
			return cfg, err
		}
		rootConfig = deepMerge(rootConfig, overrideConfig)
	} else {
		for _, candidate := range overrideCandidates(consumerRoot(filepath.Dir(bundleRoot)), rootConfig) {
			overrideConfig, err := loadYAMLMap(candidate)
			if err == nil {
				rootConfig = deepMerge(rootConfig, overrideConfig)
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return cfg, err
			}
		}
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
