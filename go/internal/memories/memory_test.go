// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package memories_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/memories"
)

func TestClassifyClaudeHomeMemoryTargetsCentralStore(t *testing.T) {
	t.Parallel()

	classification := memories.Classify(
		"/repo",
		"~/.claude/projects/acme/repo/memory/project.md",
		"claude",
	)

	if !classification.Managed || classification.CanonicalPath != memories.PrimaryFile {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestClassifyIgnoresProviderSettingsFiles(t *testing.T) {
	t.Parallel()

	classification := memories.Classify("/repo", ".claude/settings.local.json", "claude")

	if classification.Managed || !classification.Protected {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestClassifyCentralMemoryPathIsNotManagedProviderMemory(t *testing.T) {
	t.Parallel()

	classification := memories.Classify("/repo", memories.PrimaryFile, "codex")

	if classification.Managed || classification.Protected ||
		classification.Kind != "central" {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestClassifyDoesNotUseProviderHintForOrdinaryMemoryPaths(t *testing.T) {
	t.Parallel()

	classification := memories.Classify("/repo", "docs/memories/project.md", "codex")

	if classification.Managed || classification.Protected {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestMayManagePathIdentifiesProviderMemoryCandidates(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		path string
		want bool
	}{
		{name: "claude", path: "~/.claude/projects/acme/memory/project.md", want: true},
		{name: "codex", path: ".codex/memories/project.md", want: true},
		{name: "ordinary", path: "docs/memories/project.md", want: false},
		{name: "settings", path: ".claude/settings.local.json", want: false},
		{name: "central", path: memories.PrimaryFile, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := memories.MayManagePath("/repo", testCase.path); got != testCase.want {
				t.Fatalf("MayManagePath() = %t, want %t", got, testCase.want)
			}
		})
	}
}

func TestEnsureCreatesSkeleton(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := memories.Ensure(root); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, path := range []string{memories.PrimaryFile, ".coding-ethos/memories/index.yaml"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
}

func TestImportExistingIsIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, ".claude", "projects", "repo", "memory", "project.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(source, []byte("remember this\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	first, err := memories.ImportExisting(root)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := memories.ImportExisting(root)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}

	if len(first.Records) != 1 || len(second.Records) != 0 {
		t.Fatalf("reports = %#v then %#v", first, second)
	}

	data, err := os.ReadFile(filepath.Join(root, ".coding-ethos", "memories", "MEMORY.md"))
	if err != nil {
		t.Fatalf("read central memory: %v", err)
	}
	if count := strings.Count(string(data), "remember this"); count != 1 {
		t.Fatalf("imported content count = %d\n%s", count, data)
	}

	indexData, err := os.ReadFile(
		filepath.Join(root, ".coding-ethos", "memories", "index.yaml"),
	)
	if err != nil {
		t.Fatalf("read memory index: %v", err)
	}
	if !strings.Contains(string(indexData), first.Records[0].ID) {
		t.Fatalf("memory index lost import record after second import:\n%s", indexData)
	}
	if strings.Contains(string(indexData), root) || strings.Contains(string(data), root) {
		t.Fatalf("memory import leaked absolute root:\nindex=%s\nmemory=%s", indexData, data)
	}
}

func TestLoadSettingsMergesRepoOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "config.toml"),
		[]byte("[memories]\nimport_existing = false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[memories]\nimport_existing = true\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	settings, err := memories.LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !settings.ImportExisting {
		t.Fatal("repo_config.toml did not override config.toml")
	}
}
