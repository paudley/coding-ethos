// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/sandboxexec"
)

func TestSafeCgroupNameSanitizesToolNames(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                  toolFallbackName,
		"  go-test  ":       "go-test",
		"agent shell/run":   "agent-shell-run",
		"Gemini_Check.v1":   "Gemini_Check-v1",
		"policy:git#status": "policy-git-status",
	}

	for input, want := range tests {
		got := safeCgroupName(input)
		if got != want {
			t.Fatalf("safeCgroupName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWriteNativeProbeExecutableCreatesPrivateExecutable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "probe")
	err := writeNativeProbeExecutable(path, nil)
	if err != nil {
		t.Fatalf("writeNativeProbeExecutable() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat native probe executable: %v", err)
	}
	if info.Mode().Perm() != nativeProbeFileMode {
		t.Fatalf("native probe mode = %s, want %o", info.Mode().Perm(), nativeProbeFileMode)
	}
}

func TestNativeGitBindProbeCreatesExecutableFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files, err := nativeGitBindProbe(repoRoot)
	if err != nil {
		t.Fatalf("nativeGitBindProbe() error = %v", err)
	}

	for _, path := range []string{files.realGitBind, files.targetGit} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat native git probe fixture %s: %v", path, statErr)
		}
		if info.Mode().Perm() != nativeProbeFileMode {
			t.Fatalf("probe fixture mode for %s = %s", path, info.Mode().Perm())
		}
		if info.Size() != 0 {
			t.Fatalf("probe fixture %s size = %d, want zero", path, info.Size())
		}
	}

	command := exec.Command(files.targetGit, sandboxexec.NativeGitBindProbeMode)
	command.Dir = repoRoot
	if runErr := command.Run(); runErr == nil {
		t.Fatal("unbound zero-byte git target unexpectedly executed")
	}

	wrapperMarker := filepath.Join(repoRoot, ".coding-ethos", "cache", "git-wrapper")
	if _, statErr := os.Stat(wrapperMarker); !os.IsNotExist(statErr) {
		t.Fatalf("unbound target emitted wrapper marker: %v", statErr)
	}
}

func TestPrepareNativeProbeRepoCreatesPrivateWriteRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := prepareNativeProbeRepo(repoRoot); err != nil {
		t.Fatalf("prepareNativeProbeRepo() error = %v", err)
	}

	writeRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")
	info, err := os.Stat(writeRoot)
	if err != nil {
		t.Fatalf("stat native probe write root: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != nativeProbeDirMode {
		t.Fatalf(
			"native probe write root = %s, want private directory %o",
			info.Mode(),
			nativeProbeDirMode,
		)
	}
}

func TestValidateNativeProbeSideEffectsAcceptsAllowedWriteOnly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cacheRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")
	if err := os.MkdirAll(cacheRoot, nativeProbeDirMode); err != nil {
		t.Fatalf("create probe cache: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(cacheRoot, "probe"),
		[]byte("ok\n"),
		0o600,
	); err != nil {
		t.Fatalf("write allowed probe marker: %v", err)
	}

	evidence, err := validateNativeProbeSideEffects(repoRoot, Evidence{})
	if err != nil {
		t.Fatalf("validateNativeProbeSideEffects() error = %v", err)
	}
	if evidence.Denied {
		t.Fatalf("successful probe side effects denied execution: %#v", evidence)
	}
}

func TestValidateNativeProbeSideEffectsRejectsBlockedWrite(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cacheRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")
	if err := os.MkdirAll(cacheRoot, nativeProbeDirMode); err != nil {
		t.Fatalf("create probe cache: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(cacheRoot, "probe"),
		[]byte("ok\n"),
		0o600,
	); err != nil {
		t.Fatalf("write allowed probe marker: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repoRoot, "blocked"),
		[]byte("bad"),
		0o600,
	); err != nil {
		t.Fatalf("write blocked probe marker: %v", err)
	}

	evidence, err := validateNativeProbeSideEffects(repoRoot, Evidence{})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("validateNativeProbeSideEffects() error = %v, want unavailable", err)
	}
	if !evidence.Denied || evidence.Reason == "" {
		t.Fatalf("blocked write did not deny execution: %#v", evidence)
	}
}

func TestValidateNativeGitBindSideEffectsRejectsUnwrappedTarget(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	cacheRoot := filepath.Join(repoRoot, ".coding-ethos", "cache")
	if err := os.MkdirAll(cacheRoot, nativeProbeDirMode); err != nil {
		t.Fatalf("create git bind probe cache: %v", err)
	}
	for path, content := range map[string]string{
		filepath.Join(cacheRoot, "git-wrapper"): "wrapper\n",
		filepath.Join(cacheRoot, "git-target"):  "target\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write git bind marker %s: %v", path, err)
		}
	}

	evidence, err := validateNativeGitBindSideEffects(repoRoot, Evidence{})
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("validateNativeGitBindSideEffects() error = %v, want unavailable", err)
	}
	if !evidence.Denied ||
		evidence.Reason != "native sandbox executed unwrapped git target" {
		t.Fatalf("unwrapped git target evidence mismatch: %#v", evidence)
	}
}
