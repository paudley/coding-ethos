// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSHA256File(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "asset")
	if err := os.WriteFile(path, []byte("payload\n"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	sum, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256 file: %v", err)
	}
	if sum != "d4e4877bac978b7952f0d544fc52ebff5411d351d129f1f056fa43f11da9af2b" {
		t.Fatalf("sha256 = %q", sum)
	}
}

func TestReleaseAssetURLSelectsMatchingAsset(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"assets": [
				{"name": "tool-darwin-amd64.tar.gz", "browser_download_url": "https://example.invalid/darwin"},
				{"name": "tool-linux-amd64.tar.gz", "browser_download_url": "https://example.invalid/linux"}
			]
		}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Transport = rewriteGitHubTransport{
		base:      server.URL,
		transport: client.Transport,
	}

	url, err := releaseAssetURL(client, "owner/repo", "v1.2.3", "linux-amd64", "token")
	if err != nil {
		t.Fatalf("release asset url: %v", err)
	}
	if url != "https://example.invalid/linux" {
		t.Fatalf("url = %q", url)
	}
	if authHeader != "Bearer token" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
}

func TestReleaseAssetURLReportsMissingAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"assets": []}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Transport = rewriteGitHubTransport{
		base:      server.URL,
		transport: client.Transport,
	}

	_, err := releaseAssetURL(client, "owner/repo", "v1.2.3", "linux-amd64", "")
	if err == nil || !strings.Contains(err.Error(), "release asset not found") {
		t.Fatalf("release asset error = %v", err)
	}
}

func TestInstallGitHubBinaryDownloadsAndInstallsDirectAsset(t *testing.T) {
	t.Parallel()

	const assetPayload = "binary payload\n"
	var assetURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/owner/repo/releases/tags/v1.2.3" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
				"assets": [
					{"name": "tool-linux-amd64", "browser_download_url": "` + assetURL + `"}
				]
			}`))
			return
		}
		if request.URL.Path == "/tool-linux-amd64" {
			_, _ = writer.Write([]byte(assetPayload))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	assetURL = server.URL + "/tool-linux-amd64"

	client := server.Client()
	client.Transport = rewriteGitHubTransport{
		base:      server.URL,
		transport: client.Transport,
	}
	sumBytes := sha256.Sum256([]byte(assetPayload))
	sum := hex.EncodeToString(sumBytes[:])
	destDir := t.TempDir()
	if err := installGitHubBinary(
		client,
		"owner/repo",
		"v1.2.3",
		"linux-amd64",
		"tool",
		destDir,
		sum,
		"",
	); err != nil {
		t.Fatalf("install github binary: %v", err)
	}

	installed := filepath.Join(destDir, "tool")
	payload, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(payload) != assetPayload {
		t.Fatalf("installed payload = %q", payload)
	}
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %v", info.Mode())
	}
}

func TestInstallDownloadedAssetExtractsTarGzip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.tar.gz")
	writeTarGzipFixture(t, archivePath, "nested/tool", "payload\n", 0o755)

	destDir := filepath.Join(root, "bin")
	if err := installDownloadedAsset(archivePath, "tool", destDir, filepath.Join(root, "extract")); err != nil {
		t.Fatalf("install downloaded asset: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(destDir, "tool"))
	if err != nil {
		t.Fatalf("read installed tool: %v", err)
	}
	if string(payload) != "payload\n" {
		t.Fatalf("installed payload = %q", payload)
	}
}

func TestInstallGitShimWritesQuotedWrapper(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	if err := installGitShim(destDir, "/opt/git's/bin/git", "/repo/run go hook.sh"); err != nil {
		t.Fatalf("install git shim: %v", err)
	}

	shimPath := filepath.Join(destDir, "git")
	payload, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("read git shim: %v", err)
	}
	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("stat git shim: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("git shim is not executable: %v", info.Mode())
	}
	for _, want := range []string{
		`export CODING_ETHOS_REAL_GIT='/opt/git'\''s/bin/git'`,
		`exec '/repo/run go hook.sh' policy-git "$@"`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("git shim missing %q:\n%s", want, payload)
		}
	}
}

func TestInstallAndVerifyGitHookShims(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	hooksDir := t.TempDir()
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-git-hook.sh"), "git hook\n")
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-lfs-hook.sh"), "lfs hook\n")

	if err := installGitHooks([]string{"--hooks-dir", hooksDir, "--source-dir", sourceDir}); err != nil {
		t.Fatalf("install git hooks: %v", err)
	}
	if err := verifyGitHooks([]string{"--hooks-dir", hooksDir, "--source-dir", sourceDir}); err != nil {
		t.Fatalf("verify git hooks: %v", err)
	}

	for _, hook := range append(gitHookNames, lfsHookNames...) {
		info, err := os.Stat(filepath.Join(hooksDir, hook))
		if err != nil {
			t.Fatalf("stat installed hook %s: %v", hook, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("installed hook %s is not executable: %v", hook, info.Mode())
		}
	}
}

func TestGitHookFixItemsReportsMissingAndStaleHooks(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	hooksDir := t.TempDir()
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-git-hook.sh"), "git hook\n")
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-lfs-hook.sh"), "lfs hook\n")
	writeExecutableFixture(t, filepath.Join(hooksDir, "pre-commit"), "stale\n")

	items, err := gitHookShimFixItems(hooksDir, sourceDir)
	if err != nil {
		t.Fatalf("git hook fix items: %v", err)
	}
	joined := strings.Join(items, "\n")
	for _, want := range []string{
		"pre-commit is stale",
		"pre-push missing or not executable",
		"post-commit missing or not executable",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("fix items missing %q:\n%s", want, joined)
		}
	}
}

func TestAgentHookFixItemLines(t *testing.T) {
	t.Parallel()

	settingsItems := agentHookFixItemLines(
		"settings do not contain expected hooks for all providers",
	)
	if len(settingsItems) != 1 ||
		!strings.Contains(settingsItems[0], "native agent settings missing or stale") {
		t.Fatalf("settings fix items = %#v", settingsItems)
	}

	providerItems := agentHookFixItemLines(
		"Codex hooks feature missing\nGemini .gemini/settings.json mismatch",
	)
	joined := strings.Join(providerItems, "\n")
	for _, want := range []string{
		".codex/config.toml missing codex_hooks=true",
		".gemini/settings.json missing expected hook",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("provider fix items missing %q:\n%s", want, joined)
		}
	}
}

func TestCutoverReportLines(t *testing.T) {
	t.Parallel()

	fixItemsPath := filepath.Join(t.TempDir(), "fix-items.toon")
	if err := os.WriteFile(
		fixItemsPath,
		[]byte("  git-hooks,/repo/.git/hooks/pre-commit is stale,run cutover install\n\n"),
		0o600,
	); err != nil {
		t.Fatalf("write fix items: %v", err)
	}

	report, err := newCutoverReport(
		"verify",
		"blocked",
		"/repo",
		map[string]string{
			"git-hooks":      "FAIL",
			"agent-hooks":    "PASS",
			"repo-ignores":   "PASS",
			"policy-runtime": "PASS",
		},
		fixItemsPath,
	)
	if err != nil {
		t.Fatalf("new cutover report: %v", err)
	}

	got := strings.Join(cutoverReportLines(report), "\n")
	for _, want := range []string{
		"format: toon",
		"command: cutover",
		"action: verify",
		"status: blocked",
		"repo: /repo",
		"surfaces[4]{name,status}:",
		"  git-hooks,FAIL",
		"fix_first[1]{surface,problem,action}:",
		"  git-hooks,/repo/.git/hooks/pre-commit is stale,run cutover install",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cutover report missing %q:\n%s", want, got)
		}
	}
}

func TestCutoverVerifyPassesAllSurfaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".coding-ethos/\n"), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	sourceDir := t.TempDir()
	hooksDir := filepath.Join(root, ".git", "hooks")
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-git-hook.sh"), "git hook\n")
	writeExecutableFixture(t, filepath.Join(sourceDir, "run-lfs-hook.sh"), "lfs hook\n")
	if err := installGitHooks([]string{"--hooks-dir", hooksDir, "--source-dir", sourceDir}); err != nil {
		t.Fatalf("install git hooks: %v", err)
	}
	runner := filepath.Join(root, "runner")
	writeExecutableFixture(t, runner, strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"case \"${1:-}\" in",
		"  agent-hooks|policy-lint|git-hook) exit 0 ;;",
		"  *) exit 2 ;;",
		"esac",
		"",
	}, "\n"))

	if err := cutoverVerify([]string{
		"--action", "verify",
		"--root", root,
		"--runner", runner,
		"--hooks-dir", hooksDir,
		"--source-dir", sourceDir,
		"--real-git", "git",
		"--bundle-root", filepath.Join(root, "pre-commit"),
	}); err != nil {
		t.Fatalf("cutover verify: %v", err)
	}
}

func TestInstallManagedToolchainInstallsAndWritesManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestSource := filepath.Join(root, "managed.tsv")
	goBinDir := filepath.Join(root, "go-bin")
	githubBinDir := filepath.Join(root, "github-bin")
	installedManifest := filepath.Join(root, "manifest.tsv")
	if err := os.WriteFile(
		manifestSource,
		[]byte(strings.Join([]string{
			"# tool\tinstaller\tsource\tversion\tasset_substring\tbinary\tsha256\tdest",
			"shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\tgo-bin",
			"shellcheck\tgithub\tkoalaman/shellcheck\tv0.10.0\tlinux.x86_64\tshellcheck\tabc\tgithub-bin",
			"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write manifest source: %v", err)
	}

	var installed []string
	installer := managedToolInstaller{
		InstallGo: func(module string, version string, destDir string) error {
			installed = append(installed, "go:"+module+"@"+version)
			writeExecutableFixture(t, filepath.Join(destDir, "shfmt"), "shfmt\n")
			return nil
		},
		InstallRust: func(crate string, version string, binary string, destDir string) error {
			t.Fatalf("unexpected rust install %s@%s %s %s", crate, version, binary, destDir)
			return nil
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			installed = append(installed, "github:"+tool.Source+"@"+tool.Version)
			writeExecutableFixture(t, filepath.Join(destDir, tool.Binary), tool.Binary+"\n")
			return nil
		},
	}

	if err := installManagedToolchain(
		manifestSource,
		goBinDir,
		githubBinDir,
		installedManifest,
		installer,
	); err != nil {
		t.Fatalf("install managed toolchain: %v", err)
	}

	if strings.Join(installed, ",") != "go:mvdan.cc/sh/v3/cmd/shfmt@v3.13.1,github:koalaman/shellcheck@v0.10.0" {
		t.Fatalf("installed = %#v", installed)
	}
	payload, err := os.ReadFile(installedManifest)
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	manifest := string(payload)
	for _, want := range []string{
		"Generated by coding-ethos-toolchain install-managed-toolchain",
		"shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\t" + filepath.Join(goBinDir, "shfmt"),
		"shellcheck\tgithub\tkoalaman/shellcheck\tv0.10.0\tlinux.x86_64\tshellcheck\tabc\t" + filepath.Join(githubBinDir, "shellcheck"),
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("installed manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestInstallManagedToolchainSkipsAlreadyInstalledTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestSource := filepath.Join(root, "managed.tsv")
	goBinDir := filepath.Join(root, "go-bin")
	installedManifest := filepath.Join(root, "manifest.tsv")
	if err := os.MkdirAll(goBinDir, 0o755); err != nil {
		t.Fatalf("create go bin: %v", err)
	}
	writeExecutableFixture(t, filepath.Join(goBinDir, "shfmt"), "shfmt\n")
	if err := os.WriteFile(
		manifestSource,
		[]byte("shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\tgo-bin\n"),
		0o600,
	); err != nil {
		t.Fatalf("write manifest source: %v", err)
	}
	record := "shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\t" +
		filepath.Join(goBinDir, "shfmt") + "\n"
	if err := os.WriteFile(installedManifest, []byte(record), 0o600); err != nil {
		t.Fatalf("write installed manifest: %v", err)
	}

	installer := managedToolInstaller{
		InstallGo: func(module string, version string, destDir string) error {
			t.Fatalf("unexpected go install %s@%s %s", module, version, destDir)
			return nil
		},
		InstallRust: func(crate string, version string, binary string, destDir string) error {
			t.Fatalf("unexpected rust install %s@%s %s %s", crate, version, binary, destDir)
			return nil
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			t.Fatalf("unexpected github install %#v %s", tool, destDir)
			return nil
		},
	}

	if err := installManagedToolchain(
		manifestSource,
		goBinDir,
		filepath.Join(root, "github-bin"),
		installedManifest,
		installer,
	); err != nil {
		t.Fatalf("install managed toolchain: %v", err)
	}
}

func TestRepoIgnoreFixItemLines(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init")

	items, err := repoIgnoreFixItemLines("git", repo)
	if err != nil {
		t.Fatalf("repo ignore fix items before ignore: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items before ignore = %#v", items)
	}

	if err := os.WriteFile(
		filepath.Join(repo, ".gitignore"),
		[]byte(".coding-ethos/\n"),
		0o600,
	); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	items, err = repoIgnoreFixItemLines("git", repo)
	if err != nil {
		t.Fatalf("repo ignore fix items after ignore: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items after ignore = %#v", items)
	}
}

func TestRuntimeFixItemsReadsNonEmptyOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.out")
	if err := os.WriteFile(path, []byte("failed\n"), 0o600); err != nil {
		t.Fatalf("write runtime output: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat runtime output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("runtime fixture should be non-empty")
	}
}

func writeExecutableFixture(t *testing.T, path string, payload string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(payload), 0o755); err != nil {
		t.Fatalf("write executable fixture %s: %v", path, err)
	}
}

func writeTarGzipFixture(
	t *testing.T,
	path string,
	memberName string,
	payload string,
	mode int64,
) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz fixture: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{
		Name: memberName,
		Mode: mode,
		Size: int64(len(payload)),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write([]byte(payload)); err != nil {
		t.Fatalf("write tar payload: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tar.gz fixture: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

type rewriteGitHubTransport struct {
	base      string
	transport http.RoundTripper
}

func (transport rewriteGitHubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	baseRequest, err := http.NewRequest(http.MethodGet, transport.base, nil)
	if err != nil {
		return nil, err
	}
	rewritten.URL.Scheme = baseRequest.URL.Scheme
	rewritten.URL.Host = baseRequest.URL.Host

	return transport.transport.RoundTrip(rewritten)
}
