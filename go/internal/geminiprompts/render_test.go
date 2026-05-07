// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package geminiprompts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/geminiprompts"
)

const (
	geminiFixtureDirMode  = 0o700
	geminiFixtureFileMode = 0o600
)

func TestRenderPromptPackGroundsRepoIdentityAndConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	repo := filepath.Join(root, "repo")
	writeGeminiFixture(t, ethos, repo)

	rendered, err := geminiprompts.Render(geminiprompts.Options{
		EthosRoot:  ethos,
		RepoRoot:   repo,
		Primary:    filepath.Join(ethos, "coding_ethos.yml"),
		RepoEthos:  filepath.Join(repo, "repo_ethos.yml"),
		RepoConfig: filepath.Join(repo, "repo_config.yaml"),
	})
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}

	var payload map[string]any

	err = json.Unmarshal([]byte(rendered), &payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	project := jsonObject(t, payload["project"])
	if project["name"] != "Widget Service" ||
		project["context"] != "Processes widgets." {
		t.Fatalf("project grounding = %#v", project)
	}

	grounding := jsonObject(t, payload["grounding"])
	notes := stringSliceFromJSON(grounding["enforcement_notes"])
	assertContains(t, notes, "Shared line length is 100 characters.")
	assertContains(t, notes, "Target Python version is 3.13.")
	assertContains(t, notes, "Primary source paths: src/widget_service.")
	assertContains(t, notes, "Primary test paths: tests.")
	assertContains(t, notes, "Stub paths: typings.")
	assertContains(
		t,
		notes,
		"Direct internal imports are restricted for packages: widget_service.internal.",
	)
	assertContains(
		t,
		notes,
		"Utility centralization is enabled; banned direct imports: "+
			"os.path -> pathlib; legacy.util.",
	)
	assertContains(
		t,
		notes,
		"SQL centralization is enabled; keep raw query strings in "+
			"module widget_service.sql and paths src/widget_service/sql.",
	)
	assertContains(
		t,
		notes,
		"Plan workflow enforcement is enabled; plan roots docs/plans, "+
			"metadata file plan.yaml.",
	)
	assertContains(t, notes, "Pytest gate command: uv run pytest.")
	assertContains(
		t,
		notes,
		"Gemini modal-path allowlist: scripts/bootstrap.sh, scripts/legacy/*.sh.",
	)

	prompts := jsonObject(t, payload["prompts"])
	codeEthos := jsonString(t, prompts["code_ethos"])
	assertStringContains(t, codeEthos, "Widget Service")
	assertStringContains(
		t,
		codeEthos,
		"01. SOLID is Law: Enforce simple SOLID designs.",
	)
	assertStringContains(t, codeEthos, "{code_content}")
}

func TestSyncAndCheckPromptPack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	repo := filepath.Join(root, "repo")
	writeGeminiFixture(t, ethos, repo)
	options := geminiprompts.Options{
		EthosRoot:  ethos,
		RepoRoot:   repo,
		Primary:    filepath.Join(ethos, "coding_ethos.yml"),
		RepoEthos:  filepath.Join(repo, "repo_ethos.yml"),
		RepoConfig: filepath.Join(repo, "repo_config.yaml"),
	}

	written, err := geminiprompts.Sync(options)
	if err != nil {
		t.Fatalf("Sync(): %v", err)
	}

	promptPackPath := filepath.Join(
		repo,
		filepath.FromSlash(geminiprompts.PromptPackPath),
	)
	if len(written) != 1 || written[0] != promptPackPath {
		t.Fatalf("written = %#v, want %s", written, promptPackPath)
	}

	mismatched, err := geminiprompts.Check(options)
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}

	if len(mismatched) != 0 {
		t.Fatalf("Check() after sync = %#v, want none", mismatched)
	}

	err = os.WriteFile(
		promptPackPath,
		[]byte(`{"broken": true}`+"\n"),
		geminiFixtureFileMode,
	)
	if err != nil {
		t.Fatalf("write drift: %v", err)
	}

	mismatched, err = geminiprompts.Check(options)
	if err != nil {
		t.Fatalf("Check() after drift: %v", err)
	}

	if len(mismatched) != 1 || mismatched[0] != promptPackPath {
		t.Fatalf("Check() after drift = %#v", mismatched)
	}
}

func TestRenderMergesRepoPrincipleOverlay(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	repo := filepath.Join(root, "repo")
	writeGeminiFixture(t, ethos, repo)
	writeFile(t, filepath.Join(repo, "repo_ethos.yml"), `
repo:
  name: Overlay Service
principles:
  - id: solid-is-law
    summary: Overlay summary.
  - id: testing-as-specification
    order: 2
    title: Testing as Specification
    summary: Tests describe behavior.
    directive: Keep tests current.
`)

	rendered, err := geminiprompts.Render(geminiprompts.Options{
		EthosRoot:  ethos,
		RepoRoot:   repo,
		Primary:    filepath.Join(ethos, "coding_ethos.yml"),
		RepoEthos:  filepath.Join(repo, "repo_ethos.yml"),
		RepoConfig: filepath.Join(repo, "repo_config.yaml"),
	})
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}

	assertStringContains(t, rendered, `"name": "Overlay Service"`)
	assertStringContains(t, rendered, "Overlay summary.")
	assertStringContains(t, rendered, "Testing as Specification")
}

func TestPromptPathHelpersPreferRepoConfigAndAbsoluteFallbacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	repo := filepath.Join(root, "repo")

	writeGeminiFixture(t, ethos, repo)

	err := os.Remove(filepath.Join(repo, "repo_config.yaml"))
	if err != nil {
		t.Fatalf("remove yaml repo config: %v", err)
	}

	repoConfig := filepath.Join(repo, "repo_config.yml")

	err = os.MkdirAll(repo, geminiFixtureDirMode)
	if err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	err = os.WriteFile(repoConfig, []byte("project: {}\n"), geminiFixtureFileMode)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	rendered, err := geminiprompts.Render(geminiprompts.Options{
		EthosRoot: ethos,
		RepoRoot:  repo,
		Primary:   filepath.Join(ethos, "coding_ethos.yml"),
		RepoEthos: filepath.Join(repo, "repo_ethos.yml"),
	})
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}

	assertStringContains(t, rendered, `"repo_config": "repo_config.yml"`)
}

func writeGeminiFixture(t *testing.T, ethos, repo string) {
	t.Helper()

	writePrimaryFixture(t, ethos)
	writeRepoEthosFixture(t, repo)
	writeRepoConfigFixture(t, repo)
	writeTemplates(t, ethos)
	writeBundleConfigFixture(t, ethos)
}

func writePrimaryFixture(t *testing.T, ethos string) {
	t.Helper()

	writeFile(t, filepath.Join(ethos, "coding_ethos.yml"), `
version: 2
metadata:
  title: Test Ethos
  overview: Shared overview.
agents:
  gemini:
    notes:
      - Prefer targeted reads of the matching ethos detail docs.
principles:
  - id: solid-is-law
    order: 1
    title: SOLID is Law
    summary: Structure wins over convenience.
    directive: Enforce simple SOLID designs.
    quick_ref:
      - Favor explicit, non-speculative structure.
    agent_hints:
      gemini: Keep review output architectural and concrete.
`)
}

func writeRepoEthosFixture(t *testing.T, repo string) {
	t.Helper()

	writeFile(t, filepath.Join(repo, "repo_ethos.yml"), `
repo:
  name: Widget Service
  overview: Processes widgets.
  commands:
    test:
      - uv run pytest
  paths:
    source: src/widget_service
  notes:
    - Widget IDs are immutable.
agent_notes:
  gemini:
    - Prefer narrow, file-level reads before broad summaries.
`)
}

func writeRepoConfigFixture(t *testing.T, repo string) {
	t.Helper()

	writeFile(t, filepath.Join(repo, "repo_config.yaml"), `
style:
  line_length: 100
  python_version: "3.13"
python:
  source_paths:
    - src/widget_service
  test_paths:
    - tests
  stub_paths:
    - typings
  direct_imports:
    enabled: true
    packages:
      - widget_service.internal
  util_centralization:
    enabled: true
    banned_modules:
      - module: os.path
        alternative: pathlib
      - legacy.util
  sql_centralization:
    enabled: true
    module_name: widget_service.sql
    central_paths:
      - src/widget_service/sql
  plan_completion:
    enabled: true
    root_markers:
      - docs/plans
    metadata_filename: plan.yaml
  pytest_gate:
    enabled: true
    test_command:
      - uv
      - run
      - pytest
gemini:
  modal_allowlist_files:
    - scripts/bootstrap.sh
    - scripts/legacy/*.sh
`)
}

func writeBundleConfigFixture(t *testing.T, ethos string) {
	t.Helper()

	writeFile(t, filepath.Join(ethos, "config.yaml"), `
style:
  line_length: 88
  python_version: "3.13"
python:
  source_paths:
    - pkg
generated_config:
  ci:
    github_actions:
      enabled: false
    gitlab:
      enabled: false
`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), geminiFixtureDirMode)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	err = os.WriteFile(path, []byte(content), geminiFixtureFileMode)
	if err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func writeTemplates(t *testing.T, ethos string) {
	t.Helper()

	for _, name := range geminiTemplateFileNames() {
		writeFile(
			t,
			filepath.Join(ethos, "pre-commit", "prompts", "checks", name),
			`{{ render_repo_grounding(repo_overview, repo_commands, repo_paths, repo_notes, `+
				`gemini_notes, enforcement_notes) }}

{{ render_principles(principles) }}

Project {{ project_name }}: {{ project_context }}

{{ code_content_placeholder }}
`,
		)
	}
}

func geminiTemplateFileNames() []string {
	return []string{
		"code_ethos.j2",
		"shell_review.j2",
		"shell_ethos.j2",
		"shell_documentation.j2",
		"shellcheck_suppression.j2",
		"shell_placeholder.j2",
	}
}

func stringSliceFromJSON(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok {
			result = append(result, text)
		}
	}

	return result
}

func jsonObject(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("JSON value = %#v, want object", value)
	}

	return result
}

func jsonString(t *testing.T, value any) string {
	t.Helper()

	result, ok := value.(string)
	if !ok {
		t.Fatalf("JSON value = %#v, want string", value)
	}

	return result
}

func assertContains(t *testing.T, values []string, target string) {
	t.Helper()

	if slices.Contains(values, target) {
		return
	}

	t.Fatalf("%q not found in %#v", target, values)
}

func assertStringContains(t *testing.T, value, target string) {
	t.Helper()

	if !strings.Contains(value, target) {
		t.Fatalf("%q not found in %q", target, value)
	}
}
