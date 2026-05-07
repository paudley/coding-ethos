// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentskills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentskills"
)

const expectedSkillSurfaceCount = 4

func TestRenderPreservesRepoPrincipleAppendInSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	primary := filepath.Join(root, "coding_ethos.yml")
	repo := filepath.Join(root, "repo")
	repoEthos := filepath.Join(repo, "repo_ethos.yml")

	writeTestFile(t, primary, `
repo:
  name: demo
skills:
  - id: demo-skill
    title: Demo Skill
    description: Use for demo work.
    principle_ids:
      - demo-principle
principles:
  - id: demo-principle
    order: 1
    title: Demo Principle
    summary: Shared summary.
    directive: Shared directive.
    sections:
      - id: overview
        title: Overview
        body: Shared body.
`)
	writeTestFile(t, repoEthos, `
principles:
  overrides:
    demo-principle:
      append: |-
        Repo-specific addendum.
`)

	rendered, err := agentskills.Render(agentskills.Options{
		RepoRoot:  repo,
		Primary:   primary,
		RepoEthos: repoEthos,
	})
	if err != nil {
		t.Fatalf("agentskills.Render(): %v", err)
	}

	content := rendered[".agents/skills/demo-skill/SKILL.md"]
	assertContainsAll(t, content, []string{
		"Shared body.",
		"#### Repo Addendum",
		"Repo-specific addendum.",
	})
}

func TestRenderMergesOverlayShapesAndFiltersUnknownPrinciples(t *testing.T) {
	t.Parallel()

	content, manifest := renderOverlayShapeSkill(t)
	assertOverlaySkillContent(t, content)

	if strings.Contains(content, "missing-principle") ||
		strings.Contains(content, "- four") {
		t.Fatalf("rendered skill kept filtered data:\n%s", content)
	}

	if !strings.Contains(manifest, "ETHOS skills for repo-name: demo-skill") {
		t.Fatalf("manifest did not use merged repo name:\n%s", manifest)
	}
}

func renderOverlayShapeSkill(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	primary := filepath.Join(root, "coding_ethos.yml")
	repo := filepath.Join(root, "repo")
	repoEthos := filepath.Join(repo, "repo_ethos.yml")

	writeTestFile(t, primary, `
repo:
  name: base-name
skills:
  - id: demo-skill
    title: Demo Skill
    description: Use for "quoted" demo work.
    principle_ids:
      - demo-principle
      - missing-principle
    trigger_terms:
      - demo
    short_hint: Keep it focused.
    focus: Use this skill for focused demo remediation.
    remediation_steps:
      - Inspect the finding.
      - Run the focused check.
principles:
  - id: demo-principle
    order: 2
    title: Demo Principle
    summary: Shared summary.
    directive: Shared directive.
    quick_ref:
      - one
      - two
      - three
      - four
    sections:
      - id: overview
        title: Overview
        body: "See [Section 2: Demo Principle] before editing."
`)
	writeTestFile(t, repoEthos, `
repo:
  name: repo-name
principles:
  - id: demo-principle
    sections:
      - id: extra
        title: Extra
        body: Extra body.
  - id: new-principle
    order: 3
    title: New Principle
    summary: New summary.
    directive: New directive.
`)

	rendered, err := agentskills.Render(agentskills.Options{
		RepoRoot:  repo,
		Primary:   primary,
		RepoEthos: repoEthos,
	})
	if err != nil {
		t.Fatalf("agentskills.Render(): %v", err)
	}

	return rendered[".codex/skills/demo-skill/SKILL.md"],
		rendered[".gemini/extensions/coding-ethos/gemini-extension.json"]
}

func assertOverlaySkillContent(t *testing.T, content string) {
	t.Helper()

	assertContainsAll(t, content, []string{
		`description: "Use for \"quoted\" demo work."`,
		"metadata:\n  source: coding_ethos.yml",
		"    - demo-principle",
		"Use this skill for focused demo remediation.",
		"## Short Hint\nKeep it focused.",
		"## Use When\n- demo",
		"1. Inspect the finding.",
		"2. Run the focused check.",
		"- one\n- two\n- three",
	})
	assertContainsInOrder(t, content, "#### Extra", "Extra body.")
}

func TestSyncAndCheckSkillSurfaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	primary := filepath.Join(root, "coding_ethos.yml")
	repo := filepath.Join(root, "repo")

	writeTestFile(t, primary, `
repo:
  name: demo
skills:
  - id: demo-skill
    title: Demo Skill
    description: Use for demo work.
    principle_ids:
      - demo-principle
principles:
  - id: demo-principle
    order: 1
    title: Demo Principle
    summary: Shared summary.
    directive: Shared directive.
`)

	options := agentskills.Options{RepoRoot: repo, Primary: primary}

	written, err := agentskills.Sync(options)
	if err != nil {
		t.Fatalf("agentskills.Sync(): %v", err)
	}

	if len(written) != expectedSkillSurfaceCount+1 {
		t.Fatalf(
			"written %d files, want %d: %#v",
			len(written),
			expectedSkillSurfaceCount+1,
			written,
		)
	}

	mismatched, err := agentskills.Check(options)
	if err != nil {
		t.Fatalf("agentskills.Check() after sync: %v", err)
	}

	if len(mismatched) != 0 {
		t.Fatalf("agentskills.Check() after sync = %#v, want none", mismatched)
	}

	driftPath := filepath.Join(repo, ".agents", "skills", "demo-skill", "SKILL.md")
	writeRawTestFile(t, driftPath, "drift\n")

	mismatched, err = agentskills.Check(options)
	if err != nil {
		t.Fatalf("agentskills.Check() after drift: %v", err)
	}

	if len(mismatched) != 1 || mismatched[0] != driftPath {
		t.Fatalf(
			"agentskills.Check() after drift = %#v, want %s",
			mismatched,
			driftPath,
		)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	writeRawTestFile(t, path, strings.TrimSpace(content)+"\n")
}

func writeRawTestFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertContainsAll(t *testing.T, content string, expected []string) {
	t.Helper()

	for _, want := range expected {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered content missing %q:\n%s", want, content)
		}
	}
}

func assertContainsInOrder(t *testing.T, content, first, second string) {
	t.Helper()

	firstIndex := strings.Index(content, first)
	if firstIndex < 0 {
		t.Fatalf("rendered content missing %q:\n%s", first, content)
	}

	remaining := content[firstIndex+len(first):]
	if !strings.Contains(remaining, second) {
		t.Fatalf("rendered content missing %q after %q:\n%s", second, first, content)
	}
}
