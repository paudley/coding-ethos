// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolconfigs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSyncWritesGeneratedConfigsAndCheckDetectsDrift(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	repo := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())

	written, err := Sync(ethos, repo, "")
	if err != nil {
		t.Fatalf("Sync(): %v", err)
	}

	wantWritten := []string{
		filepath.Join(repo, ".bandit.yml"),
		filepath.Join(repo, ".code-ethos", "tool-config-hashes.json"),
		filepath.Join(repo, ".golangci.yml"),
		filepath.Join(repo, ".pylintrc"),
		filepath.Join(repo, ".sqlfluff"),
		filepath.Join(repo, ".yamllint.yml"),
		filepath.Join(repo, "mypy.ini"),
		filepath.Join(repo, "pyrightconfig.json"),
		filepath.Join(repo, "ruff.toml"),
		filepath.Join(repo, "tombi.toml"),
	}
	if !reflect.DeepEqual(sortedStrings(written), wantWritten) {
		t.Fatalf("written = %#v, want %#v", sortedStrings(written), wantWritten)
	}

	mismatched, err := Check(ethos, repo, "")
	if err != nil {
		t.Fatalf("Check() after sync: %v", err)
	}

	if len(mismatched) != 0 {
		t.Fatalf("Check() after sync = %#v, want none", mismatched)
	}

	writeFile(t, filepath.Join(repo, "ruff.toml"), "drift\n")

	mismatched, err = Check(ethos, repo, "")
	if err != nil {
		t.Fatalf("Check() after drift: %v", err)
	}

	if !reflect.DeepEqual(
		sortedStrings(mismatched),
		[]string{filepath.Join(repo, "ruff.toml")},
	) {
		t.Fatalf("Check() drift = %#v", mismatched)
	}
}

func TestRenderAllIncludesEnabledCI(t *testing.T) {
	t.Parallel()

	config, err := loadYAMLMapFromString(minimalConfigWithCI())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	rendered, err := RenderAll(config)
	if err != nil {
		t.Fatalf("RenderAll(): %v", err)
	}

	for _, path := range []string{
		".github/workflows/coding-ethos-sarif.yml",
		".gitlab-ci.yml",
	} {
		if rendered[path] == "" {
			t.Fatalf("RenderAll() missing %s", path)
		}
	}
}

func TestRenderAllRejectsInvalidSandboxMode(t *testing.T) {
	t.Parallel()

	config, err := loadYAMLMapFromString(minimalConfigWithCI())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	generatedConfig, generatedOK := config["generated_config"].(map[string]any)
	if !generatedOK {
		t.Fatalf("generated_config missing from %#v", config)
	}

	ciConfig, ciOK := generatedConfig["ci"].(map[string]any)
	if !ciOK {
		t.Fatalf("generated_config.ci missing from %#v", generatedConfig)
	}

	githubActions, githubOK := ciConfig["github_actions"].(map[string]any)
	if !githubOK {
		t.Fatalf("github_actions missing from %#v", ciConfig)
	}

	githubActions["sandbox_mode"] = "sometimes"

	_, err = RenderAll(config)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"invalid configured choice: generated_config.ci.github_actions.sandbox_mode",
		) {
		t.Fatalf("RenderAll() error = %v", err)
	}
}

func TestAccessAndFormatHelpers(t *testing.T) {
	t.Parallel()

	config := configMap{
		"style": map[string]any{
			"line_length":    "120",
			"python_version": "3.12",
		},
		"enabled": "yes",
		"blank":   " ",
		"paths":   []any{"src", " ", 42},
		"nested":  map[string]any{"value": "kept"},
	}

	assertConfiguredAccessHelpers(t, config)
	assertMapCloneAndValueFallback(t)
	assertRenderedYAML(t)
}

func assertConfiguredAccessHelpers(t *testing.T, config configMap) {
	t.Helper()

	if got := configuredList(
		config,
		"paths",
		[]string{"fallback"},
	); !reflect.DeepEqual(
		got,
		[]string{"src", "42"},
	) {
		t.Fatalf("configuredList = %#v", got)
	}

	if got := configuredList(
		config,
		"missing",
		[]string{"fallback"},
	); !reflect.DeepEqual(
		got,
		[]string{"fallback"},
	) {
		t.Fatalf("configuredList fallback = %#v", got)
	}

	if got := configuredString(config, "blank", "fallback"); got != "fallback" {
		t.Fatalf("configuredString blank = %q", got)
	}

	if !configuredBool(config, "enabled", false) {
		t.Fatal("configuredBool should parse yes")
	}

	if got := configuredInt(config, "style.line_length", 88); got != 120 {
		t.Fatalf("configuredInt = %d", got)
	}

	choice, err := configuredChoice(
		config,
		"style.python_version",
		"3.13",
		map[string]struct{}{"3.12": {}},
	)
	if err != nil || choice != "3.12" {
		t.Fatalf("configuredChoice = %q, %v", choice, err)
	}

	if got := mappingValue(config, "nested"); got["value"] != "kept" {
		t.Fatalf("mappingValue = %#v", got)
	}
}

func assertMapCloneAndValueFallback(t *testing.T) {
	t.Helper()

	cloned := cloneMap(map[string]any{"nested": map[string]any{"x": "y"}})

	nested, nestedOK := cloned["nested"].(map[string]any)
	if !nestedOK {
		t.Fatalf("cloned nested value = %#v, want map", cloned["nested"])
	}

	nested["x"] = "z"

	if got := valueOr("", "fallback"); got != "fallback" {
		t.Fatalf("valueOr empty = %#v", got)
	}
}

func assertRenderedYAML(t *testing.T) {
	t.Helper()

	yamlText := renderYAML(orderedMap{
		{Key: "version", Value: "2"},
		{Key: "enable", Value: []string{"true", "ruff"}},
	})
	for _, want := range []string{"version: '2'", "- 'true'", "- ruff"} {
		if !strings.Contains(yamlText, want) {
			t.Fatalf("renderYAML missing %q:\n%s", want, yamlText)
		}
	}
}

func TestHashManifestIsDeterministic(t *testing.T) {
	t.Parallel()

	manifest, err := RenderHashManifest(map[string]string{
		"ruff.toml": "line-length = 88\n",
	})
	if err != nil {
		t.Fatalf("RenderHashManifest(): %v", err)
	}

	for _, want := range []string{
		`"version": 1`,
		`"ruff.toml": "sha256:`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestLoadMergedConfigUsesConfiguredRepoCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), `
style:
  line_length: 88
bundle:
  consumer_override_candidates:
    - custom.yaml
generated_config:
  ci:
    github_actions:
      enabled: false
    gitlab:
      enabled: false
`)
	writeFile(t, filepath.Join(repo, "custom.yaml"), `
style:
  line_length: 120
`)

	merged, err := LoadMergedConfig(ethos, repo, "")
	if err != nil {
		t.Fatalf("LoadMergedConfig(): %v", err)
	}

	if got := lineLength(merged); got != 120 {
		t.Fatalf("line length = %d, want 120", got)
	}

	if got := repoConfigCandidates(
		configMap{},
	); !reflect.DeepEqual(
		got[:2],
		[]string{"repo_config.yaml", "repo_config.yml"},
	) {
		t.Fatalf("default repo config candidates = %#v", got)
	}
}

func TestLoadMergedConfigAppliesPrincipleToolConfigWithProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
principles:
  - id: static-analysis-is-the-first-line-of-defense
    tool_config:
      golangci_lint:
        linters:
          enable:
            - name: gosec
              rationale: Security analyzers are required quality gates.
          disable:
            - name: misspell
              rationale: Prose spelling policy is owned outside golangci-lint.
      bandit:
        enabled:
          value: true
          rationale: Python security scanning is part of static analysis.
        skips:
          - id: B101
            rationale: Tests assert with pytest.
`)

	merged, err := LoadMergedConfig(ethos, repo, "")
	if err != nil {
		t.Fatalf("LoadMergedConfig(): %v", err)
	}

	rendered, err := RenderAll(merged)
	if err != nil {
		t.Fatalf("RenderAll(): %v", err)
	}

	for path, wants := range map[string][]string{
		".golangci.yml": {
			"Principle-derived tool config:",
			"linters.enable gosec from static-analysis-is-the-first-line-of-defense",
			"Security analyzers are required quality gates.",
			"- gosec",
			"- misspell",
		},
		".bandit.yml": {
			"enabled true from static-analysis-is-the-first-line-of-defense",
			"Python security scanning is part of static analysis.",
			"- B101",
		},
	} {
		for _, want := range wants {
			if !strings.Contains(rendered[path], want) {
				t.Fatalf("%s missing %q:\n%s", path, want, rendered[path])
			}
		}
	}
}

func TestLoadMergedConfigPrunesProvenanceAfterRepoOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	repoConfig := filepath.Join(repo, "repo_config.yaml")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
principles:
  - id: security-by-design
    tool_config:
      golangci_lint:
        linters:
          enable:
            - name: gosec
              rationale: Security analyzer.
`)
	writeFile(t, repoConfig, `
tooling:
  golangci_lint:
    linters:
      enable:
        - govet
`)

	merged, err := LoadMergedConfig(ethos, repo, repoConfig)
	if err != nil {
		t.Fatalf("LoadMergedConfig(): %v", err)
	}

	rendered, err := renderGolangCIConfig(merged)
	if err != nil {
		t.Fatalf("renderGolangCIConfig(): %v", err)
	}

	if strings.Contains(rendered, "Principle-derived tool config") ||
		strings.Contains(rendered, "gosec") {
		t.Fatalf("rendered config kept stale provenance:\n%s", rendered)
	}

	if !strings.Contains(rendered, "- govet") {
		t.Fatalf("rendered config missing repo override:\n%s", rendered)
	}
}

func TestLoadMergedConfigAppliesRepoEthosToolConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
principles:
  - id: static-analysis-is-the-first-line-of-defense
    tool_config:
      golangci_lint:
        linters:
          enable:
            - name: gosec
              rationale: Security analyzer.
`)
	writeFile(t, filepath.Join(repo, "repo_ethos.yml"), `
principles:
  overrides:
    static-analysis-is-the-first-line-of-defense:
      tool_config:
        golangci_lint:
          linters:
            disable:
              - name: misspell
                rationale: Repo-local prose is reviewed outside golangci-lint.
  additional:
    - id: ginkgo-policy
      tool_config:
        golangci_lint:
          linters:
            enable:
              - name: ginkgolinter
                rationale: Repo-local Ginkgo tests are policy.
`)

	merged, err := LoadMergedConfig(ethos, repo, "")
	if err != nil {
		t.Fatalf("LoadMergedConfig(): %v", err)
	}

	rendered, err := renderGolangCIConfig(merged)
	if err != nil {
		t.Fatalf("renderGolangCIConfig(): %v", err)
	}

	for _, want := range []string{
		"linters.enable gosec from static-analysis-is-the-first-line-of-defense",
		"(coding_ethos.yml): Security analyzer.",
		"linters.disable misspell from static-analysis-is-the-first-line-of-defense",
		"(repo_ethos.yml): Repo-local prose is reviewed outside golangci-lint.",
		"linters.enable ginkgolinter from ginkgo-policy (repo_ethos.yml)",
		"- gosec",
		"- ginkgolinter",
		"- misspell",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
}

func TestLoadMergedConfigRejectsUnsupportedGolangCILinterKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
principles:
  - id: static-analysis-is-the-first-line-of-defense
    tool_config:
      golangci_lint:
        linters:
          enabled:
            - gosec
`)

	_, err := LoadMergedConfig(ethos, repo, "")
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"tool_config.golangci_lint.linters.enabled is not supported",
		) {
		t.Fatalf("LoadMergedConfig() error = %v", err)
	}
}

func TestRenderAllSkipsBanditWhenPrincipleDisablesIt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), "principles: []\n")
	writeFile(t, filepath.Join(repo, "repo_ethos.yml"), `
principles:
  additional:
    - id: repo-security-tools
      tool_config:
        bandit:
          enabled:
            value: false
            rationale: This repo has no Python source.
`)

	merged, err := LoadMergedConfig(ethos, repo, "")
	if err != nil {
		t.Fatalf("LoadMergedConfig(): %v", err)
	}

	rendered, err := RenderAll(merged)
	if err != nil {
		t.Fatalf("RenderAll(): %v", err)
	}

	if _, found := rendered[".bandit.yml"]; found {
		t.Fatalf("RenderAll() generated .bandit.yml despite disabled Bandit")
	}
}

func TestLoadMergedConfigRejectsUnsupportedPrincipleToolConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "ethos")
	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(ethos, "config.yaml"), minimalConfig())
	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
principles:
  - id: security-by-design
    tool_config:
      golangci_lint:
        arbitrary:
          shell: rm -rf /
`)

	_, err := LoadMergedConfig(ethos, repo, "")
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"tool_config.golangci_lint.arbitrary is not supported",
		) {
		t.Fatalf("LoadMergedConfig() error = %v", err)
	}
}

func TestRuffRendererIncludesPolicySpecificSections(t *testing.T) {
	t.Parallel()

	config, err := loadYAMLMapFromString(`
style:
  python_version: "3.13"
  line_length: 100
python:
  source_paths:
    - src
  test_paths:
    - tests
  stub_paths:
    - typings
  sql_centralization:
    enabled: true
    central_paths:
      - src/app/sql
      - src/app/queries.py
tooling:
  ruff:
    select:
      - ALL
    ignore:
      - D203
    exclude:
      - dist
    max_args: 4
    stub_per_file_ignores:
      - PYI021
    test_per_file_ignores:
      - S101
    sql_per_file_ignores:
      - S608
    extra_per_file_ignores:
      scripts/*.py:
        - T201
    banned_api:
      os.path: Use pathlib.
generated_config:
  ci:
    github_actions:
      enabled: false
    gitlab:
      enabled: false
`)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	rendered, err := renderRuffTOML(config)
	if err != nil {
		t.Fatalf("renderRuffTOML(): %v", err)
	}

	for _, want := range []string{
		`target-version = "py313"`,
		`line-length = 100`,
		`exclude = ["dist"]`,
		`max-args = 4`,
		`"typings/**" = ["PYI021"]`,
		`"tests/**" = ["S101"]`,
		`"src/app/sql/**" = ["S608"]`,
		`"src/app/queries.py" = ["S608"]`,
		`"scripts/*.py" = ["T201"]`,
		`"os.path" = { msg = "Use pathlib." }`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("ruff config missing %q:\n%s", want, rendered)
		}
	}
}

func minimalConfigWithCI() string {
	return `
style:
  python_version: "3.13"
  line_length: 88
python:
  source_paths:
    - pkg
  extra_paths:
    - .
tooling:
  ruff:
    ignore: []
  mypy:
    plugins: []
  pyright: {}
  pylint: {}
  yamllint:
    rules: {}
  bandit: {}
  sqlfluff: {}
  golangci_lint: {}
generated_config:
  ci:
    github_actions:
      enabled: true
      sarif_category: .github/workflows/ci.yml:policy
    gitlab:
      enabled: true
      test_command: uv run pytest
      build_command: uv build
      package_check_command: uvx twine check dist/*
`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func minimalConfig() string {
	return `
style:
  python_version: "3.13"
  line_length: 88
python:
  source_paths:
    - pkg
  extra_paths:
    - .
tooling:
  ruff:
    ignore: []
  mypy:
    plugins: []
  pyright: {}
  pylint: {}
  yamllint:
    rules: {}
  bandit: {}
  sqlfluff: {}
  golangci_lint: {}
generated_config:
  ci:
    github_actions:
      enabled: false
    gitlab:
      enabled: false
`
}

func loadYAMLMapFromString(content string) (configMap, error) {
	var decoded map[string]any

	err := yaml.Unmarshal([]byte(content), &decoded)
	if err != nil {
		return nil, fmt.Errorf("parse YAML fixture: %w", err)
	}

	if decoded == nil {
		decoded = map[string]any{}
	}

	return decoded, nil
}

func sortedStrings(values []string) []string {
	cloned := append([]string(nil), values...)
	slices.Sort(cloned)

	return cloned
}
