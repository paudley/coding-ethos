// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/lintcapture"
)

func TestTargetResolverResolvesPackageRelativeTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(
		root,
		"lbox-platform",
		"lib",
		"python",
		"lbox",
		"corpus",
		"inline.py",
	)
	writeFile(t, target, "import os\n")

	resolver, err := lintcapture.NewTargetResolver(
		root,
		root,
		[]string{"lbox-platform/lib/python"},
	)
	if err != nil {
		t.Fatalf("NewTargetResolver(): %v", err)
	}

	got, err := resolver.ResolveArgs([]string{"lbox/corpus/inline.py"})
	if err != nil {
		t.Fatalf("ResolveArgs(): %v", err)
	}

	if !reflect.DeepEqual(got, []string{target}) {
		t.Fatalf("ResolveArgs() = %#v, want %#v", got, []string{target})
	}
}

func TestTargetResolverResolvesGlobsFromPolicyRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := filepath.Join(root, "lbox-platform", "lib", "python", "lbox", "corpus")
	first := filepath.Join(base, "a.py")
	second := filepath.Join(base, "b.py")

	writeFile(t, first, "import os\n")
	writeFile(t, second, "import sys\n")

	resolver, err := lintcapture.NewTargetResolver(
		root,
		root,
		[]string{"lbox-platform/lib/python"},
	)
	if err != nil {
		t.Fatalf("NewTargetResolver(): %v", err)
	}

	got, err := resolver.ResolveArgs([]string{"lbox/corpus/*.py"})
	if err != nil {
		t.Fatalf("ResolveArgs(): %v", err)
	}

	if !reflect.DeepEqual(got, []string{first, second}) {
		t.Fatalf("ResolveArgs() = %#v, want sorted glob matches", got)
	}
}

func TestTargetResolverRejectsEscapingRoots(t *testing.T) {
	t.Parallel()

	_, err := lintcapture.NewTargetResolver(t.TempDir(), "", []string{".."})
	if err == nil {
		t.Fatal("NewTargetResolver() accepted repo-escaping root")
	}
}

func TestTargetResolverPreservesMissingPackagePathIntent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	resolver, err := lintcapture.NewTargetResolver(root, root, []string{"src"})
	if err != nil {
		t.Fatalf("NewTargetResolver(): %v", err)
	}

	got := resolver.ResolvePath("pkg/missing.py")

	want := filepath.Join(root, "pkg", "missing.py")
	if got != want {
		t.Fatalf("ResolvePath() = %q, want %q", got, want)
	}
}

func TestTargetResolverRelativizesResolvedConsumerPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	resolver, err := lintcapture.NewTargetResolver(root, root, nil)
	if err != nil {
		t.Fatalf("NewTargetResolver(): %v", err)
	}

	got := resolver.RelativizeArgs([]string{
		"--check",
		filepath.Join(root, "lbox-platform", "lib", "python", "tests", "app.py"),
		"/outside/app.py",
	})

	want := []string{
		"--check",
		"lbox-platform/lib/python/tests/app.py",
		"/outside/app.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RelativizeArgs() = %#v, want %#v", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}
