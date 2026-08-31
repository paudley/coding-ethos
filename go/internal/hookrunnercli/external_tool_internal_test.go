// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestPrepareHookProcessCacheEnvironmentScopesUniversalCaches(t *testing.T) {
	testlock.ProcessState(t, "hook-process-cache-environment")

	root := t.TempDir()
	t.Setenv("GOPATH", "/previous/go-path")
	t.Setenv("GOMODCACHE", "/previous/go-mod-cache")
	t.Setenv("UV_CACHE_DIR", "/previous/uv-cache")
	t.Setenv("UV_PROJECT_ENVIRONMENT", "/previous/uv-project-environment")
	t.Setenv("UV_FROZEN", "0")
	restore, err := prepareHookProcessCacheEnvironment(root)
	if err != nil {
		t.Fatalf("prepareHookProcessCacheEnvironment: %v", err)
	}

	wantCache := filepath.Join(root, ".coding-ethos", "cache", "uv")
	if got := os.Getenv("UV_CACHE_DIR"); got != wantCache {
		t.Fatalf("UV_CACHE_DIR = %q, want %q", got, wantCache)
	}
	if info, statErr := os.Stat(wantCache); statErr != nil || !info.IsDir() {
		t.Fatalf("UV cache directory is not usable: info=%v error=%v", info, statErr)
	}
	wantGoPath := filepath.Join(root, ".coding-ethos", "cache", "go-path")
	wantGoModCache := filepath.Join(wantGoPath, "pkg", "mod")
	for name, want := range map[string]string{
		"GOPATH":     wantGoPath,
		"GOMODCACHE": wantGoModCache,
	} {
		if got := os.Getenv(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
		if info, statErr := os.Stat(want); statErr != nil || !info.IsDir() {
			t.Fatalf("%s directory is not usable: info=%v error=%v", name, info, statErr)
		}
	}

	if got := os.Getenv(
		"UV_PROJECT_ENVIRONMENT",
	); got != "/previous/uv-project-environment" {
		t.Fatalf("UV_PROJECT_ENVIRONMENT changed process-wide: %q", got)
	}
	if got := os.Getenv("UV_FROZEN"); got != "0" {
		t.Fatalf("UV_FROZEN changed process-wide: %q", got)
	}
	projectEnvRoot := filepath.Join(root, ".coding-ethos", "cache", "uv-project-env")
	if _, statErr := os.Stat(projectEnvRoot); !os.IsNotExist(statErr) {
		t.Fatalf("process preparation created uv project environment: %v", statErr)
	}

	restore()
	if got := os.Getenv("GOPATH"); got != "/previous/go-path" {
		t.Fatalf("restored GOPATH = %q", got)
	}
	if got := os.Getenv("GOMODCACHE"); got != "/previous/go-mod-cache" {
		t.Fatalf("restored GOMODCACHE = %q", got)
	}
	if got := os.Getenv("UV_CACHE_DIR"); got != "/previous/uv-cache" {
		t.Fatalf("restored UV_CACHE_DIR = %q", got)
	}
	if got := os.Getenv(
		"UV_PROJECT_ENVIRONMENT",
	); got != "/previous/uv-project-environment" {
		t.Fatalf("restored UV_PROJECT_ENVIRONMENT = %q", got)
	}
	if got := os.Getenv("UV_FROZEN"); got != "0" {
		t.Fatalf("restored UV_FROZEN = %q", got)
	}
}

func TestExternalToolEnvRemovesGitHookLocalEnvironment(t *testing.T) {
	shimDir := t.TempDir()
	shimPath := filepath.Join(shimDir, "git")
	repo := t.TempDir()
	t.Chdir(repo)

	writeErr := os.WriteFile(
		shimPath,
		[]byte("#!/bin/sh\nexec coding-ethos-run policy-git \"$@\"\n"),
		0o600,
	)
	if writeErr != nil {
		t.Fatalf("write git shim fixture: %v", writeErr)
	}

	t.Setenv("GIT_DIR", "/tmp/wrong-git-dir")
	t.Setenv("GIT_INDEX_FILE", "/tmp/wrong-index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "test@example.com")
	t.Setenv("CODING_ETHOS_REAL_GIT", "/tmp/hook-real-git")
	t.Setenv("GOCACHE", "/tmp/host-go-cache")
	t.Setenv("GOPATH", "/tmp/host-go-path")
	t.Setenv("GOMODCACHE", "/tmp/host-go-mod-cache")
	t.Setenv("UV_PROJECT_ENVIRONMENT", "/tmp/host-uv-project")
	t.Setenv("UV_FROZEN", "0")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv(consumerRootEnv, repo)
	t.Setenv(hookGroupChildEnv, hookPlanBoolTrue)
	t.Setenv(hookGroupResultPathEnv, "/tmp/result.json")
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ROOT", repo)

	env, err := externalToolEnv(externalToolRequest{
		Dir:     repo,
		Command: []string{"go", "test", "./..."},
		Env:     []string{"KEEP_EXTRA=1"},
	})
	if err != nil {
		t.Fatalf("externalToolEnv() returned error: %v", err)
	}

	for _, item := range env {
		name, _, found := strings.Cut(item, "=")
		if found && externalToolEnvBlocked(name+"=value") {
			t.Fatalf("externalToolEnv leaked %s in %#v", name, env)
		}
	}

	if !slices.Contains(env, "KEEP_EXTRA=1") {
		t.Fatalf("externalToolEnv dropped explicit extra env: %#v", env)
	}

	if !slices.Contains(env, "GIT_OPTIONAL_LOCKS=0") {
		t.Fatalf("externalToolEnv did not disable optional git locks: %#v", env)
	}

	if !slices.Contains(env, "CODING_ETHOS_REAL_GIT=/tmp/hook-real-git") {
		t.Fatalf("externalToolEnv dropped approved real git binding: %#v", env)
	}

	for _, want := range []string{
		"CODING_ETHOS_AGENT_SHELL_SANDBOX=1",
		"CODING_ETHOS_SANDBOX_ACTIVE=1",
		"CODING_ETHOS_SANDBOX_ROOT=" + repo,
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("externalToolEnv dropped active sandbox marker %q: %#v", want, env)
		}
	}

	if !slices.Contains(
		env,
		"GOCACHE="+filepath.Join(repo, ".coding-ethos/cache/go-build"),
	) ||
		slices.Contains(env, "GOCACHE=/tmp/host-go-cache") {
		t.Fatalf("externalToolEnv did not replace host Go cache: %#v", env)
	}

	wantGoPath := filepath.Join(repo, ".coding-ethos", "cache", "go-path")
	for name, want := range map[string]string{
		"GOPATH":     wantGoPath,
		"GOMODCACHE": filepath.Join(wantGoPath, "pkg", "mod"),
	} {
		if got := externalToolTestRequiredEnvValue(t, env, name); got != want {
			t.Fatalf("externalToolEnv %s = %q, want %q", name, got, want)
		}
	}
	for _, unwanted := range []string{
		"GOPATH=/tmp/host-go-path",
		"GOMODCACHE=/tmp/host-go-mod-cache",
	} {
		if slices.Contains(env, unwanted) {
			t.Fatalf("externalToolEnv kept host Go path %q: %#v", unwanted, env)
		}
	}

	if !slices.Contains(
		env,
		"GOTMPDIR="+filepath.Join(repo, ".coding-ethos/cache/go-tmp"),
	) {
		t.Fatalf("externalToolEnv did not set Go build temp dir: %#v", env)
	}

	if !slices.Contains(
		env,
		"TMPDIR="+filepath.Join(repo, ".coding-ethos/cache/go-tmp"),
	) {
		t.Fatalf("externalToolEnv did not set process temp dir: %#v", env)
	}

	if !slices.Contains(
		env,
		"UV_CACHE_DIR="+filepath.Join(repo, ".coding-ethos/cache/uv"),
	) {
		t.Fatalf("externalToolEnv did not set uv cache dir: %#v", env)
	}

	if _, found := externalToolTestEnvValue(env, "UV_PROJECT_ENVIRONMENT"); found {
		t.Fatalf("externalToolEnv leaked uv project scope into non-uv tool: %#v", env)
	}
	if _, found := externalToolTestEnvValue(env, "UV_FROZEN"); found {
		t.Fatalf("externalToolEnv leaked frozen mode into non-uv tool: %#v", env)
	}

	for _, item := range env {
		if !strings.HasPrefix(item, "PATH=") {
			continue
		}

		if strings.Contains(item, shimDir) {
			t.Fatalf("externalToolEnv leaked coding-ethos git shim PATH: %#v", env)
		}

		if !strings.Contains(item, "/usr/bin") {
			t.Fatalf("externalToolEnv dropped non-shim PATH entries: %#v", env)
		}

		return
	}

	t.Fatalf("externalToolEnv omitted PATH: %#v", env)
}

func TestExternalToolEnvIsolatesActiveUVProjects(t *testing.T) {
	repo := t.TempDir()
	firstProject := filepath.Join(repo, "first")
	secondProject := filepath.Join(repo, "second")
	for _, project := range []string{firstProject, secondProject} {
		mustWriteTestFile(
			t,
			filepath.Join(project, "pyproject.toml"),
			"[project]\nname = 'fixture'\n",
		)
	}

	t.Chdir(repo)
	t.Setenv(consumerRootEnv, repo)

	firstEnv, err := externalToolEnv(externalToolRequest{
		Dir:     firstProject,
		Command: []string{"uv", "run", "python", "-V"},
	})
	if err != nil {
		t.Fatalf("first externalToolEnv: %v", err)
	}
	secondEnv, err := externalToolEnv(externalToolRequest{
		Dir: repo,
		Command: []string{
			"uv", "run", "--project", "second", "python", "-V",
		},
	})
	if err != nil {
		t.Fatalf("second externalToolEnv: %v", err)
	}

	firstPath := externalToolTestRequiredEnvValue(
		t,
		firstEnv,
		"UV_PROJECT_ENVIRONMENT",
	)
	secondPath := externalToolTestRequiredEnvValue(
		t,
		secondEnv,
		"UV_PROJECT_ENVIRONMENT",
	)
	if firstPath == secondPath {
		t.Fatalf("distinct uv projects shared environment %q", firstPath)
	}
	wantRoot := filepath.Join(repo, ".coding-ethos", "cache", "uv-project-env")
	for _, path := range []string{firstPath, secondPath} {
		if !strings.HasPrefix(path, wantRoot+string(os.PathSeparator)) {
			t.Fatalf("uv project environment %q is outside %q", path, wantRoot)
		}
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Fatalf("uv project environment is not usable: info=%v error=%v", info, statErr)
		}
	}

	firstCache := externalToolTestRequiredEnvValue(t, firstEnv, "UV_CACHE_DIR")
	secondCache := externalToolTestRequiredEnvValue(t, secondEnv, "UV_CACHE_DIR")
	if firstCache != secondCache {
		t.Fatalf("uv projects did not share cache: %q != %q", firstCache, secondCache)
	}
}

func TestExternalToolEnvFreezesOnlySealedOrExplicitUVProjects(t *testing.T) {
	repo := t.TempDir()
	sealedProject := filepath.Join(repo, "sealed")
	unsealedProject := filepath.Join(repo, "unsealed")
	for _, project := range []string{sealedProject, unsealedProject} {
		mustWriteTestFile(
			t,
			filepath.Join(project, "pyproject.toml"),
			"[project]\nname = 'fixture'\n",
		)
	}
	mustWriteTestFile(t, filepath.Join(sealedProject, "uv.lock"), "version = 1\n")

	runGitTestCommandInDir(t, repo, "init", "--quiet")
	runGitTestCommandInDir(
		t,
		repo,
		"add",
		"sealed/pyproject.toml",
		"sealed/uv.lock",
		"unsealed/pyproject.toml",
	)
	runGitTestCommandInDir(
		t,
		repo,
		"-c",
		"user.name=Fixture",
		"-c",
		"user.email=fixture@example.test",
		"-c",
		"commit.gpgSign=false",
		"commit",
		"--quiet",
		"--no-gpg-sign",
		"-m",
		"fixture",
	)
	mustWriteTestFile(t, filepath.Join(unsealedProject, "uv.lock"), "version = 1\n")

	t.Chdir(repo)
	t.Setenv(consumerRootEnv, repo)

	sealedEnv, err := externalToolEnv(externalToolRequest{
		Dir:     sealedProject,
		Command: []string{"uv", "run", "python", "-V"},
	})
	if err != nil {
		t.Fatalf("sealed externalToolEnv: %v", err)
	}
	if got := externalToolTestRequiredEnvValue(t, sealedEnv, "UV_FROZEN"); got != "1" {
		t.Fatalf("sealed UV_FROZEN = %q, want 1", got)
	}

	unsealedEnv, err := externalToolEnv(externalToolRequest{
		Dir:     unsealedProject,
		Command: []string{"uv", "run", "python", "-V"},
	})
	if err != nil {
		t.Fatalf("unsealed externalToolEnv: %v", err)
	}
	if value, found := externalToolTestEnvValue(unsealedEnv, "UV_FROZEN"); found {
		t.Fatalf("unsealed UV_FROZEN = %q, want absent", value)
	}

	explicitEnv, err := externalToolEnv(externalToolRequest{
		Dir: unsealedProject,
		Command: []string{
			"uv", "run", "--frozen", "python", "-V",
		},
	})
	if err != nil {
		t.Fatalf("explicit frozen externalToolEnv: %v", err)
	}
	if got := externalToolTestRequiredEnvValue(t, explicitEnv, "UV_FROZEN"); got != "1" {
		t.Fatalf("explicit UV_FROZEN = %q, want 1", got)
	}
}

func TestExternalToolEnvAddsUsablePathWhenInheritedPathMissing(t *testing.T) {
	original, hadOriginal := os.LookupEnv("PATH")
	if err := os.Unsetenv("PATH"); err != nil {
		t.Fatalf("unset PATH: %v", err)
	}
	t.Cleanup(func() {
		if hadOriginal {
			_ = os.Setenv("PATH", original)
		} else {
			_ = os.Unsetenv("PATH")
		}
	})

	env, err := externalToolEnv(externalToolRequest{
		Command: []string{"go", "version"},
	})
	if err != nil {
		t.Fatalf("externalToolEnv() returned error: %v", err)
	}

	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name != "PATH" {
			continue
		}

		pathEntries := filepath.SplitList(value)
		if !slices.Contains(pathEntries, "/usr/bin") &&
			!slices.Contains(pathEntries, "/bin") {
			t.Fatalf("externalToolEnv PATH lacks system executable dirs: %#v", env)
		}

		return
	}

	t.Fatalf("externalToolEnv omitted PATH: %#v", env)
}

func externalToolTestRequiredEnvValue(t *testing.T, env []string, name string) string {
	t.Helper()

	value, found := externalToolTestEnvValue(env, name)
	if !found {
		t.Fatalf("external tool environment omitted %s: %#v", name, env)
	}

	return value
}

func externalToolTestEnvValue(env []string, name string) (string, bool) {
	for _, item := range env {
		itemName, value, found := strings.Cut(item, "=")
		if found && itemName == name {
			return value, true
		}
	}

	return "", false
}

func TestExternalToolCacheEnvFailsWhenCacheDirsCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, ".coding-ethos"),
		[]byte("file\n"),
		0o600,
	); err != nil {
		t.Fatalf("write blocking cache path: %v", err)
	}

	_, err := externalToolCacheEnv(root)
	if err == nil {
		t.Fatal("externalToolCacheEnv() error = nil, want cache creation failure")
	}
}

func TestRunExternalToolCapturesStdoutAndStderrSeparately(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")

	err := os.WriteFile(
		tool,
		[]byte("#!/usr/bin/env sh\necho stdout-json\necho stderr-text >&2\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err = os.Chmod(tool, 0o700)
	if err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	result := runExternalTool(externalToolRequest{
		Name:    "fixture",
		Dir:     dir,
		Command: []string{tool},
	})
	if result.Stdout != "stdout-json" || result.Stderr != "stderr-text" {
		t.Fatalf("streams = stdout %q stderr %q", result.Stdout, result.Stderr)
	}

	if result.Combined != "stdout-json\nstderr-text" {
		t.Fatalf("combined = %q", result.Combined)
	}
}
