// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcapture

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseToolConfigDriftNormalizesOutput(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	drift := parseToolConfigDrift(
		root,
		"\n"+filepath.Join(root, "ruff.toml")+"\n../outside.toml\n",
	)

	got := make([]string, 0, len(drift))
	for _, item := range drift {
		got = append(got, item.File)
	}

	want := []string{"ruff.toml", "../outside.toml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drift = %#v, want %#v", got, want)
	}
}

func TestParseToolConfigDriftAddsFallbackWhenOutputIsEmpty(t *testing.T) {
	t.Parallel()

	drift := parseToolConfigDrift(t.TempDir(), " \n")
	if len(drift) != 1 || drift[0].File != "generated tool configs" {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestRepoRelativePathRejectsExternalAbsolutePaths(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	if rel, ok := repoRelativePath(
		root,
		filepath.Join(root, "nested", "file.txt"),
	); !ok ||
		rel != "nested/file.txt" {
		t.Fatalf("repoRelativePath internal = %q, %v", rel, ok)
	}

	if rel, ok := repoRelativePath(
		root,
		filepath.Join(filepath.Dir(root), "outside.txt"),
	); ok ||
		rel != "" {
		t.Fatalf("repoRelativePath external = %q, %v", rel, ok)
	}
}
