package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLimitsForFilePreservesPythonLimitsUnderScripts(t *testing.T) {
	cfg := Config{}
	cfg.LineLimits.PythonHard = 1000
	cfg.LineLimits.PythonWarn = 800
	cfg.LineLimits.ShellHard = 500
	cfg.LineLimits.ShellWarn = 400

	tests := []struct {
		path     string
		hardWant int
		warnWant int
	}{
		{
			path:     "scripts/tool.py",
			hardWant: 1000,
			warnWant: 800,
		},
		{
			path:     "scripts/tool.sh",
			hardWant: 500,
			warnWant: 400,
		},
		{
			path:     "coding_ethos/module.py",
			hardWant: 1000,
			warnWant: 800,
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			hardGot, warnGot := limitsForFile(cfg, tc.path)
			if hardGot != tc.hardWant || warnGot != tc.warnWant {
				t.Fatalf(
					"limitsForFile(%q) = (%d, %d), want (%d, %d)",
					tc.path,
					hardGot,
					warnGot,
					tc.hardWant,
					tc.warnWant,
				)
			}
		})
	}
}

func TestLoadGeminiPromptPackParsesGeneratedContract(t *testing.T) {
	bundleRoot := filepath.Clean(filepath.Join("..", ".."))
	pack, err := loadGeminiPromptPack(bundleRoot)
	if err != nil {
		t.Fatalf("loadGeminiPromptPack(%q) returned error: %v", bundleRoot, err)
	}
	codeEthos, ok := pack.Checks["code_ethos"]
	if !ok {
		t.Fatalf("prompt pack missing code_ethos check: %#v", pack.Checks)
	}
	if codeEthos.FileScope != "code" {
		t.Fatalf("code_ethos file scope = %q, want %q", codeEthos.FileScope, "code")
	}
	if codeEthos.BatchSize != 3 {
		t.Fatalf("code_ethos batch size = %d, want %d", codeEthos.BatchSize, 3)
	}
	if codeEthos.MaxFileSizeKB != 50 {
		t.Fatalf(
			"code_ethos max_file_size_kb = %d, want %d",
			codeEthos.MaxFileSizeKB,
			50,
		)
	}
	if len(codeEthos.Selector.IncludeExtensions) == 0 {
		t.Fatal("code_ethos selector has no include_extensions")
	}
	if codeEthos.Selector.IncludeExtensions[0] != ".py" {
		t.Fatalf(
			"code_ethos first include extension = %q, want %q",
			codeEthos.Selector.IncludeExtensions[0],
			".py",
		)
	}
	if codeEthos.Selector.AllowExtensionlessInScripts {
		t.Fatal("code_ethos selector unexpectedly allows extensionless scripts")
	}
	if pack.Prompts["code_ethos"] == "" {
		t.Fatal("prompt pack has empty code_ethos prompt")
	}
}

func TestParseGeminiCLIOptions(t *testing.T) {
	options, err := parseGeminiCLIOptions(
		[]string{
			"--dry-run",
			"--full-check",
			"--check-type",
			"code_ethos",
			"one.py",
			"two.sh",
		},
	)
	if err != nil {
		t.Fatalf("parseGeminiCLIOptions() returned error: %v", err)
	}
	if !options.DryRun {
		t.Fatal("parseGeminiCLIOptions() did not enable dry-run")
	}
	if !options.FullCheck {
		t.Fatal("parseGeminiCLIOptions() did not enable full-check")
	}
	if options.CheckType != "code_ethos" {
		t.Fatalf("CheckType = %q, want %q", options.CheckType, "code_ethos")
	}
	if !reflect.DeepEqual(options.Files, []string{"one.py", "two.sh"}) {
		t.Fatalf("Files = %#v, want %#v", options.Files, []string{"one.py", "two.sh"})
	}
}

func TestParseGeminiCLIOptionsRejectsUnknownFlag(t *testing.T) {
	_, err := parseGeminiCLIOptions([]string{"--nope"})
	if err == nil {
		t.Fatal("parseGeminiCLIOptions() unexpectedly accepted unknown flag")
	}
}

func TestResolveGeminiRequestSettingsUsesOverrides(t *testing.T) {
	thinkingBudget := 512
	settings := GeminiSettings{
		Model:                "gemini-2.5-flash",
		ModelOverrides:       map[string]string{"code_ethos": "gemini-2.5-pro"},
		ServiceTier:          "standard",
		ServiceTierOverrides: map[string]string{"code_ethos": "flex"},
		ThinkingBudget:       &thinkingBudget,
		ThinkingBudgetOverrides: map[string]int{
			"code_ethos": 2048,
		},
		DisableSafetyFilters: true,
		Cache: GeminiCacheSettings{
			Enabled:    true,
			TTLSeconds: 3600,
		},
	}

	resolved, err := resolveGeminiRequestSettings(
		settings,
		"code_ethos",
		"/tmp/gemini-cache",
	)
	if err != nil {
		t.Fatalf("resolveGeminiRequestSettings() returned error: %v", err)
	}
	if resolved.Model != "gemini-2.5-pro" {
		t.Fatalf("Model = %q, want %q", resolved.Model, "gemini-2.5-pro")
	}
	if resolved.ServiceTier != "flex" {
		t.Fatalf("ServiceTier = %q, want %q", resolved.ServiceTier, "flex")
	}
	if resolved.ThinkingBudget == nil || *resolved.ThinkingBudget != 2048 {
		t.Fatalf("ThinkingBudget = %#v, want 2048", resolved.ThinkingBudget)
	}
	if !resolved.DisableSafetyFilters {
		t.Fatal("DisableSafetyFilters = false, want true")
	}
	if !resolved.Cache.Enabled {
		t.Fatal("Cache.Enabled = false, want true")
	}
	if resolved.Cache.Dir != "/tmp/gemini-cache" {
		t.Fatalf("Cache.Dir = %q, want %q", resolved.Cache.Dir, "/tmp/gemini-cache")
	}
}

func TestGeminiSafetySettingsDisabledUsesOffThresholds(t *testing.T) {
	settings := geminiSafetySettings(true)
	if len(settings) != 5 {
		t.Fatalf("len(settings) = %d, want 5", len(settings))
	}
	for _, item := range settings {
		if item.Threshold != "OFF" {
			t.Fatalf("threshold for %s = %q, want OFF", item.Category, item.Threshold)
		}
	}
}

func TestGeminiPromptForExplicitCachedContentReplacesPlaceholder(t *testing.T) {
	template := "Review these files.\n\n{code_content}\n"
	prompt := geminiPromptForExplicitCachedContent(template)
	if strings.Contains(prompt, "{code_content}") {
		t.Fatalf("prompt still contains placeholder: %q", prompt)
	}
	if !strings.Contains(prompt, "cached content") {
		t.Fatalf("prompt does not mention cached content: %q", prompt)
	}
}

func TestMatchesGeminiSelector(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(t, "pkg/module.py", "print('ok')\n")
	mustWriteTestFile(t, "scripts/tool", "#!/usr/bin/env bash\necho ok\n")
	mustWriteTestFile(t, "vendor/generated.py", "print('skip')\n")
	mustWriteTestFile(t, "notes.txt", "hello\n")

	selector := GeminiFileSelector{
		IncludeExtensions:           []string{".py"},
		ExcludePrefixes:             []string{"vendor/"},
		AllowExtensionlessInScripts: true,
		ShebangMarkers:              []string{"bash", "sh"},
	}

	tests := []struct {
		path string
		want bool
	}{
		{path: "pkg/module.py", want: true},
		{path: "scripts/tool", want: true},
		{path: "vendor/generated.py", want: false},
		{path: "notes.txt", want: false},
	}

	for _, tc := range tests {
		got, err := matchesGeminiSelector(tc.path, selector)
		if err != nil {
			t.Fatalf("matchesGeminiSelector(%q) returned error: %v", tc.path, err)
		}
		if got != tc.want {
			t.Fatalf("matchesGeminiSelector(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}

func TestPrepareGeminiChecksBuildsBatches(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(t, "a.py", "print('a')\n")
	mustWriteTestFile(t, "b.py", "print('b')\n")
	mustWriteTestFile(t, "c.py", "print('c')\n")
	mustWriteTestFile(t, "large.py", strings.Repeat("x", 2048))

	pack := GeminiPromptPack{
		Checks: map[string]GeminiPromptCheckSpec{
			"code_ethos": {
				FileScope:     "code",
				BatchSize:     2,
				MaxFileSizeKB: 1,
				Selector: GeminiFileSelector{
					IncludeExtensions: []string{".py"},
				},
			},
		},
		Prompts: map[string]string{
			"code_ethos": "Review this batch.\n{code_content}",
		},
	}

	prepared, err := prepareGeminiChecks(
		pack,
		[]string{"a.py", "b.py", "c.py", "large.py"},
		"",
		GeminiSettings{
			Model:       "gemini-2.5-flash",
			ServiceTier: "standard",
			Cache: GeminiCacheSettings{
				Enabled: true,
			},
		},
		filepath.Join(tempDir, ".cache"),
	)
	if err != nil {
		t.Fatalf("prepareGeminiChecks() returned error: %v", err)
	}
	if len(prepared) != 1 {
		t.Fatalf("len(prepared) = %d, want 1", len(prepared))
	}
	plan := prepared[0].Plan
	if !reflect.DeepEqual(plan.SelectedFiles, []string{"a.py", "b.py", "c.py", "large.py"}) {
		t.Fatalf("SelectedFiles = %#v", plan.SelectedFiles)
	}
	if !reflect.DeepEqual(plan.IncludedFiles, []string{"a.py", "b.py", "c.py"}) {
		t.Fatalf("IncludedFiles = %#v", plan.IncludedFiles)
	}
	if !reflect.DeepEqual(plan.SkippedLargeFiles, []string{"large.py"}) {
		t.Fatalf("SkippedLargeFiles = %#v", plan.SkippedLargeFiles)
	}
	if len(plan.Batches) != 2 {
		t.Fatalf("len(plan.Batches) = %d, want 2", len(plan.Batches))
	}
	if !reflect.DeepEqual(plan.Batches[0].Files, []string{"a.py", "b.py"}) {
		t.Fatalf("first batch files = %#v", plan.Batches[0].Files)
	}
	if !reflect.DeepEqual(plan.Batches[1].Files, []string{"c.py"}) {
		t.Fatalf("second batch files = %#v", plan.Batches[1].Files)
	}
	if plan.Model != "gemini-2.5-flash" {
		t.Fatalf("Model = %q, want %q", plan.Model, "gemini-2.5-flash")
	}
	if plan.ServiceTier != "standard" {
		t.Fatalf("ServiceTier = %q, want %q", plan.ServiceTier, "standard")
	}
	if !plan.CacheEnabled {
		t.Fatal("CacheEnabled = false, want true")
	}
	if prepared[0].Batches[0].ExplicitAPIKey == "" {
		t.Fatal("first batch ExplicitAPIKey is empty")
	}
	if !strings.Contains(prepared[0].Batches[0].CachedPrompt, "cached content") {
		t.Fatalf("CachedPrompt = %q, want cached-content guidance", prepared[0].Batches[0].CachedPrompt)
	}
}

func TestFilterGeminiModalAllowlistedViolations(t *testing.T) {
	violations := []geminiViolation{
		{
			Severity:     "CRITICAL",
			File:         "app/handlers/modal.py",
			Message:      "This modal gating feature enablement silently degrades behavior.",
			EthosSection: "Section 19",
		},
		{
			Severity:     "WARNING",
			File:         "app/handlers/modal.py",
			Message:      "Use a clearer variable name.",
			EthosSection: "Section 8",
		},
	}

	filtered := filterGeminiModalAllowlistedViolations(
		violations,
		[]string{"app/**/*.py"},
	)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Message != "Use a clearer variable name." {
		t.Fatalf("unexpected remaining violation: %#v", filtered[0])
	}
}

func TestParseGeminiChangedLines(t *testing.T) {
	diff := strings.Join([]string{
		"@@ -10,2 +10,3 @@",
		"@@ -20,0 +25,2 @@",
	}, "\n")
	changed := parseGeminiChangedLines(diff)
	for _, line := range []int{10, 11, 12, 25, 26} {
		if _, ok := changed[line]; !ok {
			t.Fatalf("parseGeminiChangedLines() missing line %d", line)
		}
	}
}

func TestFilterGeminiViolationsByDiff(t *testing.T) {
	violations := []geminiViolation{
		{
			Severity: "CRITICAL",
			File:     "pkg/module.py",
			Line:     12,
			Message:  "Changed-code failure",
		},
		{
			Severity: "WARNING",
			File:     "pkg/module.py",
			Line:     99,
			Message:  "Pre-existing issue",
		},
		{
			Severity: "INFO",
			File:     "pkg/module.py",
			Line:     0,
			Message:  "Unknown line should stay in diff",
		},
	}

	filtered := filterGeminiViolationsByDiff(
		violations,
		map[string]map[int]struct{}{
			"pkg/module.py": {
				12: {},
			},
		},
	)

	if len(filtered.InDiff) != 2 {
		t.Fatalf("len(filtered.InDiff) = %d, want 2", len(filtered.InDiff))
	}
	if len(filtered.PreExisting) != 1 {
		t.Fatalf("len(filtered.PreExisting) = %d, want 1", len(filtered.PreExisting))
	}
	if !filtered.hasBlockingCriticals() {
		t.Fatal("filtered.hasBlockingCriticals() = false, want true")
	}
	if !filtered.hasAnyInDiff() {
		t.Fatal("filtered.hasAnyInDiff() = false, want true")
	}
	if filtered.PreExisting[0].Message != "Pre-existing issue" {
		t.Fatalf("unexpected pre-existing violation: %#v", filtered.PreExisting[0])
	}
}

func TestGeminiCacheRoundTrip(t *testing.T) {
	cache := geminiResponseCache{
		Enabled: true,
		Dir:     t.TempDir(),
		TTL:     time.Hour,
	}
	key := "abc123"
	if err := writeGeminiCache(cache, key, "{\"ok\":true}"); err != nil {
		t.Fatalf("writeGeminiCache() returned error: %v", err)
	}
	text, ok, err := readGeminiCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiCache() returned error: %v", err)
	}
	if !ok {
		t.Fatal("readGeminiCache() returned cache miss, want hit")
	}
	if text != "{\"ok\":true}" {
		t.Fatalf("cached text = %q, want %q", text, "{\"ok\":true}")
	}
}

func TestReadGeminiCacheExpiresEntries(t *testing.T) {
	cacheDir := t.TempDir()
	cache := geminiResponseCache{
		Enabled: true,
		Dir:     cacheDir,
		TTL:     time.Second,
	}
	key := "expired"
	path := geminiCachePath(cache, key)
	entry := geminiCacheEntry{
		CreatedAt: time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339Nano),
		Text:      "stale",
	}
	mustWriteTestFile(t, path, mustJSON(t, entry))

	_, ok, err := readGeminiCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiCache() returned error: %v", err)
	}
	if ok {
		t.Fatal("readGeminiCache() returned hit for expired entry")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expired cache file still exists: %v", statErr)
	}
}

func TestGeminiExplicitCacheRoundTrip(t *testing.T) {
	cache := geminiResponseCache{
		APIEnabled: true,
		Dir:        t.TempDir(),
		APITTL:     time.Hour,
	}
	key := "explicit"
	expire := time.Now().Add(time.Hour)
	if err := writeGeminiExplicitCache(cache, key, "cachedContents/123", expire); err != nil {
		t.Fatalf("writeGeminiExplicitCache() returned error: %v", err)
	}
	name, ok, err := readGeminiExplicitCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiExplicitCache() returned error: %v", err)
	}
	if !ok {
		t.Fatal("readGeminiExplicitCache() returned miss, want hit")
	}
	if name != "cachedContents/123" {
		t.Fatalf("cache name = %q, want %q", name, "cachedContents/123")
	}
}

func mustWriteTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}
	return string(data)
}
