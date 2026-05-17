// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolchaincli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestSHA256File(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "asset")

	inlineErr0 := os.WriteFile(path, []byte("payload\n"), privateFileMode)
	if inlineErr0 != nil {
		t.Fatalf("write asset: %v", inlineErr0)
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

	client := &http.Client{
		Transport: fakeGitHubTransport(func(request *http.Request) *http.Response {
			authHeader = request.Header.Get("Authorization")

			return jsonResponse(`{
				"assets": [
					{
						"name": "tool-darwin-amd64.tar.gz",
						"browser_download_url": "https://example.invalid/darwin"
					},
					{
						"name": "tool-linux-amd64.tar.gz",
						"browser_download_url": "https://example.invalid/linux"
					}
				]
			}`)
		}),
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

	client := &http.Client{
		Transport: fakeGitHubTransport(func(_ *http.Request) *http.Response {
			return jsonResponse(`{"assets": []}`)
		}),
	}

	_, err := releaseAssetURL(client, "owner/repo", "v1.2.3", "linux-amd64", "")
	if err == nil || !strings.Contains(err.Error(), "release asset not found") {
		t.Fatalf("release asset error = %v", err)
	}
}

func TestInstallGitHubBinaryDownloadsAndInstallsDirectAsset(t *testing.T) {
	t.Parallel()

	const assetPayload = "binary payload\n"

	client := &http.Client{
		Transport: fakeGitHubTransport(func(request *http.Request) *http.Response {
			if request.URL.Path == "/repos/owner/repo/releases/tags/v1.2.3" {
				return jsonResponse(`{
					"assets": [
						{
							"name": "tool-linux-amd64",
							"browser_download_url": "https://api.github.com/tool-linux-amd64"
						}
					]
				}`)
			}

			if request.URL.Path == "/tool-linux-amd64" {
				return textResponse(assetPayload)
			}

			return textStatusResponse(http.StatusNotFound, "not found")
		}),
	}
	sumBytes := sha256.Sum256([]byte(assetPayload))
	sum := hex.EncodeToString(sumBytes[:])

	destDir := t.TempDir()

	inlineErr1 := installGitHubBinary(
		client,
		"owner/repo",
		"v1.2.3",
		"linux-amd64",
		"tool",
		destDir,
		sum,
		"",
	)
	if inlineErr1 != nil {
		t.Fatalf("install github binary: %v", inlineErr1)
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
	writeTarGzipFixture(t, archivePath, "nested/tool", "payload\n", executableFileMode)

	destDir := filepath.Join(root, "bin")

	inlineErr2 := installDownloadedAsset(
		archivePath,
		"tool",
		destDir,
		filepath.Join(root, "extract"),
	)
	if inlineErr2 != nil {
		t.Fatalf("install downloaded asset: %v", inlineErr2)
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

	inlineErr3 := installGitShim(
		destDir,
		"/opt/git's/bin/git",
		"/repo/run go hook.sh",
	)
	if inlineErr3 != nil {
		t.Fatalf("install git shim: %v", inlineErr3)
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

func TestInstallGitShimCommandValidatesAndInstalls(t *testing.T) {
	t.Parallel()

	inlineErrAutoA := installGitShimCommand(nil)
	if !errors.Is(inlineErrAutoA, errDestRequired) {
		t.Fatalf(
			"installGitShimCommand(nil) = %v, want %v",
			inlineErrAutoA,
			errDestRequired,
		)
	}

	inlineErrAutoB := installGitShimCommand(
		[]string{"--dest-dir", t.TempDir()},
	)
	if !errors.Is(inlineErrAutoB, errGitRequired) {
		t.Fatalf(
			"installGitShimCommand missing git = %v, want %v",
			inlineErrAutoB,
			errGitRequired,
		)
	}

	destDir := t.TempDir()

	err := installGitShimCommand([]string{
		"--dest-dir", destDir,
		"--real-git", "/usr/bin/git",
		"--runner", "/repo/bin/coding-ethos-run",
	})
	if err != nil {
		t.Fatalf("installGitShimCommand: %v", err)
	}

	_, inlineErrA := os.Stat(filepath.Join(destDir, "git"))
	if inlineErrA != nil {
		t.Fatalf("git shim not installed: %v", inlineErrA)
	}
}

func TestManagedAndGitHubInstallCommandValidation(t *testing.T) {
	t.Parallel()

	inlineErrAutoC := installManagedToolchainCommand(nil)
	if !errors.Is(
		inlineErrAutoC,
		errManifestRequired,
	) {
		t.Fatalf(
			"installManagedToolchainCommand(nil) = %v, want %v",
			inlineErrAutoC,
			errManifestRequired,
		)
	}

	manifest := filepath.Join(t.TempDir(), "manifest.tsv")

	inlineErr4 := os.WriteFile(manifest, []byte(""), privateFileMode)
	if inlineErr4 != nil {
		t.Fatalf("write manifest: %v", inlineErr4)
	}

	err := installManagedToolchainCommand([]string{"--manifest-source", manifest})
	if err == nil || !strings.Contains(err.Error(), "--go-bin-dir") {
		t.Fatalf("missing go bin error = %v", err)
	}

	inlineErrAutoD := installGitHubBinaryCommand(nil)
	if !errors.Is(inlineErrAutoD, errRepoRequired) {
		t.Fatalf(
			"installGitHubBinaryCommand(nil) = %v, want %v",
			inlineErrAutoD,
			errRepoRequired,
		)
	}
}

func TestInstallAndVerifyGitHookShims(t *testing.T) {
	t.Parallel()

	hooksDir := t.TempDir()
	runner := filepath.Join(t.TempDir(), "coding-ethos-run")
	writeExecutableFixture(t, runner, "runner\n")

	err := installGitHooks([]string{"--hooks-dir", hooksDir, "--runner", runner})
	if err != nil {
		t.Fatalf("install git hooks: %v", err)
	}

	err = verifyGitHooks([]string{"--hooks-dir", hooksDir, "--runner", runner})
	if err != nil {
		t.Fatalf("verify git hooks: %v", err)
	}

	for _, hook := range append(gitHookNames(), lfsHookNames()...) {
		hookPath := filepath.Join(hooksDir, hook)

		info, err := os.Stat(hookPath)
		if err != nil {
			t.Fatalf("stat installed hook %s: %v", hook, err)
		}

		if info.Mode()&0o111 == 0 {
			t.Fatalf("installed hook %s is not executable: %v", hook, info.Mode())
		}

		payload, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatalf("read hook entrypoint %s: %v", hook, err)
		}

		command := "git-hook"
		if slices.Contains(lfsHookNames(), hook) {
			command = "lfs-hook"
		}

		want := "exec " + shellQuote(
			runner,
		) + " " + command + " " + shellQuote(
			hook,
		) + ` "$@"`
		if !strings.Contains(string(payload), want) {
			t.Fatalf("hook %s payload missing %q:\n%s", hook, want, payload)
		}
	}
}

func TestGitHookFixItemsReportsMissingAndStaleHooks(t *testing.T) {
	t.Parallel()

	hooksDir := t.TempDir()
	runner := filepath.Join(t.TempDir(), "coding-ethos-run")
	writeExecutableFixture(t, runner, "runner\n")
	writeExecutableFixture(t, filepath.Join(hooksDir, "pre-commit"), "stale\n")

	items, err := gitHookShimFixItems(hooksDir, runner)
	if err != nil {
		t.Fatalf("git hook fix items: %v", err)
	}

	joined := strings.Join(items, "\n")
	for _, want := range []string{
		"pre-commit does not route to coding-ethos-run",
		"pre-push missing or not executable",
		"post-commit missing or not executable",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("fix items missing %q:\n%s", want, joined)
		}
	}
}

func TestGitHookFixItemsCommandPrintsItems(t *testing.T) {
	t.Parallel()

	hooksDir := t.TempDir()
	runner := filepath.Join(t.TempDir(), "coding-ethos-run")
	writeExecutableFixture(t, runner, "runner\n")

	stdout := captureToolchainStdout(t, func() {
		err := gitHookFixItems([]string{"--hooks-dir", hooksDir, "--runner", runner})
		if err != nil {
			t.Fatalf("gitHookFixItems: %v", err)
		}
	})
	if !strings.Contains(stdout, "pre-commit missing or not executable") {
		t.Fatalf("git hook fix stdout = %q", stdout)
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
		".codex/config.toml missing hooks=true",
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

	inlineErr5 := os.WriteFile(
		fixItemsPath,
		[]byte(
			"  git-hooks,/repo/.git/hooks/pre-commit is stale,run cutover install\n\n",
		),
		privateFileMode,
	)
	if inlineErr5 != nil {
		t.Fatalf("write fix items: %v", inlineErr5)
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

func TestCutoverReportCommandValidatesAndPrintsReport(t *testing.T) {
	t.Parallel()

	fixItemsPath := filepath.Join(t.TempDir(), "fix-items.toon")

	err := os.WriteFile(
		fixItemsPath,
		[]byte("  git-hooks,stale hook,run make build\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write fix items: %v", err)
	}

	err = cutoverReport(nil)
	if !errors.Is(err, errActionRequired) {
		t.Fatalf("missing action error = %v, want %v", err, errActionRequired)
	}

	_, inlineErrAutoE := newCutoverReport(
		"verify",
		"blocked",
		"/repo",
		map[string]string{
			"git-hooks":      "FAIL",
			"agent-hooks":    "PASS",
			"repo-ignores":   "PASS",
			"policy-runtime": "PASS",
		},
		filepath.Join(t.TempDir(), "missing"),
	)
	if inlineErrAutoE == nil ||
		!strings.Contains(inlineErrAutoE.Error(), errFixItemsOpen.Error()) {
		t.Fatalf("missing fix item error = %v", inlineErrAutoE)
	}

	stdout := captureToolchainStdout(t, func() {
		err := cutoverReport([]string{
			"--action", "install",
			"--status", "blocked",
			"--repo", "/repo",
			"--git-hooks", "FAIL",
			"--agent-hooks", "PASS",
			"--repo-ignores", "PASS",
			"--runtime", "PASS",
			"--fix-items", fixItemsPath,
		})
		if err != nil {
			t.Fatalf("cutoverReport: %v", err)
		}
	})
	for _, want := range []string{
		"action: install",
		"status: blocked",
		"  git-hooks,FAIL",
		"fix_first[1]{surface,problem,action}:",
		"  git-hooks,stale hook,run make build",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cutover report stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestCutoverVerifyPassesAllSurfaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init")

	err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte(".coding-ethos/\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	hooksDir := filepath.Join(root, ".git", "hooks")
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

	err = installGitHooks([]string{"--hooks-dir", hooksDir, "--runner", runner})
	if err != nil {
		t.Fatalf("install git hooks: %v", err)
	}

	err = cutoverVerify([]string{
		"--action", "verify",
		"--root", root,
		"--runner", runner,
		"--hooks-dir", hooksDir,
		"--real-git", "git",
		"--bundle-root", filepath.Join(root, "pre-commit"),
	})
	if err != nil {
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

	inlineErr6 := os.WriteFile(
		manifestSource,
		[]byte(managedToolchainManifestFixture()),
		privateFileMode,
	)
	if inlineErr6 != nil {
		t.Fatalf("write manifest source: %v", inlineErr6)
	}

	var installed []string

	installer := recordingManagedToolInstaller(t, &installed)

	inlineErr7 := installManagedToolchain(
		manifestSource,
		goBinDir,
		githubBinDir,
		installedManifest,
		installer,
	)
	if inlineErr7 != nil {
		t.Fatalf("install managed toolchain: %v", inlineErr7)
	}

	wantInstalled := "go:mvdan.cc/sh/v3/cmd/shfmt@v3.13.1," +
		"github:koalaman/shellcheck@v0.10.0,npm:eslint@10.3.0"
	if strings.Join(installed, ",") != wantInstalled {
		t.Fatalf("installed = %#v", installed)
	}

	payload, err := os.ReadFile(installedManifest)
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}

	manifest := string(payload)
	for _, want := range []string{
		"Generated by coding-ethos-toolchain install-managed-toolchain",
		"shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\t" +
			filepath.Join(goBinDir, "shfmt"),
		"shellcheck\tgithub\tkoalaman/shellcheck\tv0.10.0\tlinux.x86_64" +
			"\tshellcheck\tabc\t" + filepath.Join(githubBinDir, "shellcheck"),
		"eslint\tnpm\teslint\t10.3.0\tnpm-locks/eslint-10.3.0" +
			"\teslint\tsha512-fixture\t" +
			filepath.Join(githubBinDir, "eslint"),
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("installed manifest missing %q:\n%s", want, manifest)
		}
	}
}

func managedToolchainManifestFixture() string {
	return strings.Join([]string{
		strings.Join(
			[]string{
				"# tool",
				"installer",
				"source",
				"version",
				"asset_substring",
				"binary",
				"checksum",
				"dest",
			},
			"\t",
		),
		"shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\tgo-bin",
		"shellcheck\tgithub\tkoalaman/shellcheck\tv0.10.0\tlinux.x86_64" +
			"\tshellcheck\tabc\tgithub-bin",
		"eslint\tnpm\teslint\t10.3.0\tnpm-locks/eslint-10.3.0" +
			"\teslint\tsha512-fixture\tgithub-bin",
		"",
	}, "\n")
}

func recordingManagedToolInstaller(
	t *testing.T,
	installed *[]string,
) managedToolInstaller {
	t.Helper()

	return managedToolInstaller{
		InstallGo: func(module, version, destDir string) error {
			*installed = append(*installed, "go:"+module+"@"+version)

			writeExecutableFixture(t, filepath.Join(destDir, "shfmt"), "shfmt\n")

			return nil
		},
		InstallNPM: func(
			packageName,
			version,
			binary,
			integrity,
			lockDir,
			destDir string,
		) error {
			*installed = append(*installed, "npm:"+packageName+"@"+version)

			if !strings.HasSuffix(lockDir, "npm-locks/eslint-10.3.0") {
				t.Fatalf("npm lock dir = %q", lockDir)
			}

			writeExecutableFixture(t, filepath.Join(destDir, binary), binary+"\n")

			return nil
		},
		InstallRust: func(crate, version, binary, destDir string) error {
			t.Fatalf("unexpected rust install %s@%s %s %s", crate, version, binary, destDir)

			return nil
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			*installed = append(*installed, "github:"+tool.Source+"@"+tool.Version)
			writeExecutableFixture(t, filepath.Join(destDir, tool.Binary), tool.Binary+"\n")

			return nil
		},
	}
}

func TestInstallManagedToolchainSkipsAlreadyInstalledTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestSource := filepath.Join(root, "managed.tsv")
	goBinDir := filepath.Join(root, "go-bin")
	installedManifest := filepath.Join(root, "manifest.tsv")

	err := os.MkdirAll(goBinDir, directoryMode)
	if err != nil {
		t.Fatalf("create go bin: %v", err)
	}

	writeExecutableFixture(t, filepath.Join(goBinDir, "shfmt"), "shfmt\n")

	err = os.WriteFile(
		manifestSource,
		[]byte("shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\tgo-bin\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write manifest source: %v", err)
	}

	record := "shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\tv3.13.1\t-\tshfmt\t-\t" +
		filepath.Join(goBinDir, "shfmt") + "\n"

	err = os.WriteFile(installedManifest, []byte(record), privateFileMode)
	if err != nil {
		t.Fatalf("write installed manifest: %v", err)
	}

	err = installManagedToolchain(
		manifestSource,
		goBinDir,
		filepath.Join(root, "github-bin"),
		installedManifest,
		unexpectedManagedToolInstaller(t),
	)
	if err != nil {
		t.Fatalf("install managed toolchain: %v", err)
	}
}

func unexpectedManagedToolInstaller(t *testing.T) managedToolInstaller {
	t.Helper()

	return managedToolInstaller{
		InstallGo: func(module, version, destDir string) error {
			t.Fatalf("unexpected go install %s@%s %s", module, version, destDir)

			return nil
		},
		InstallNPM: func(
			packageName,
			version,
			binary,
			integrity,
			lockDir,
			destDir string,
		) error {
			t.Fatalf(
				"unexpected npm install %s@%s %s %s %s %s",
				packageName,
				version,
				binary,
				integrity,
				lockDir,
				destDir,
			)

			return nil
		},
		InstallRust: func(crate, version, binary, destDir string) error {
			t.Fatalf("unexpected rust install %s@%s %s %s", crate, version, binary, destDir)

			return nil
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			t.Fatalf("unexpected github install %#v %s", tool, destDir)

			return nil
		},
	}
}

func TestWriteNPMBinaryWrapperExecutesManagedPackageScript(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	packageRoot := filepath.Join(root, "pkg with space")
	script := filepath.Join(packageRoot, "node_modules", ".bin", "eslint")

	err := os.MkdirAll(filepath.Dir(script), directoryMode)
	if err != nil {
		t.Fatalf("create npm fixture bin: %v", err)
	}

	writeExecutableFixture(t, script, "#!/usr/bin/env node\n")

	target := filepath.Join(root, "bin", "eslint")

	err = writeNPMBinaryWrapper(packageRoot, "eslint", target)
	if err != nil {
		t.Fatalf("write npm binary wrapper: %v", err)
	}

	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read npm wrapper: %v", err)
	}

	want := "exec '" + script + "' \"$@\""
	if !strings.Contains(string(payload), want) {
		t.Fatalf("wrapper missing managed script exec %q:\n%s", want, payload)
	}
}

func TestInstallManagedToolchainReportsManifestDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestSource := filepath.Join(root, "managed.tsv")

	err := os.WriteFile(
		manifestSource,
		[]byte("shfmt\tgo\tmvdan.cc/sh/v3/cmd/shfmt\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write manifest source: %v", err)
	}

	err = installManagedToolchain(
		manifestSource,
		filepath.Join(root, "go-bin"),
		filepath.Join(root, "github-bin"),
		filepath.Join(root, "manifest.tsv"),
		recordingManagedToolInstaller(t, &[]string{}),
	)

	var diagnosticErr managedToolchainDiagnosticError
	if !errors.As(err, &diagnosticErr) {
		t.Fatalf("install error = %T %v, want diagnostic error", err, err)
	}

	diagnostics := diagnosticErr.Diagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one", diagnostics)
	}

	diagnostic := diagnostics[0]
	if diagnostic.Tool != "managed-toolchain" ||
		diagnostic.File != manifestSource ||
		diagnostic.Line != 1 ||
		diagnostic.Code != "invalid-field-count" ||
		diagnostic.SkillID != "managed-toolchain" ||
		diagnostic.Metadata["repair_command"] != "make build" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostic)
	}
}

func TestRunCLIEmitsManagedToolchainDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestSource := filepath.Join(root, "managed.tsv")

	err := os.WriteFile(
		manifestSource,
		[]byte("broken\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write manifest source: %v", err)
	}

	stderr := captureToolchainStderr(t, func() {
		code := runCLI([]string{
			"install-managed-toolchain",
			"--manifest-source", manifestSource,
			"--go-bin-dir", filepath.Join(root, "go-bin"),
			"--github-bin-dir", filepath.Join(root, "github-bin"),
			"--installed-manifest", filepath.Join(root, "manifest.tsv"),
		})
		if code != 1 {
			t.Fatalf("runCLI exit = %d, want 1", code)
		}
	})

	for _, want := range []string{
		"tool: policy-lint",
		"managed-toolchain,",
		"policy_id",
		"toolchain.managed_manifest",
		"invalid-field-count",
		"make build",
		manifestSource,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
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

	inlineErr8 := os.WriteFile(
		filepath.Join(repo, ".gitignore"),
		[]byte(".coding-ethos/\n"),
		privateFileMode,
	)
	if inlineErr8 != nil {
		t.Fatalf("write gitignore: %v", inlineErr8)
	}

	items, err = repoIgnoreFixItemLines("git", repo)
	if err != nil {
		t.Fatalf("repo ignore fix items after ignore: %v", err)
	}

	if len(items) != 0 {
		t.Fatalf("items after ignore = %#v", items)
	}
}

func TestRepoIgnoreFixItemsCommandPrintsItems(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init")

	stdout := captureToolchainStdout(t, func() {
		err := repoIgnoreFixItems([]string{"--repo-root", repo, "--real-git", "git"})
		if err != nil {
			t.Fatalf("repoIgnoreFixItems: %v", err)
		}
	})
	if !strings.Contains(stdout, ".coding-ethos/ is not ignored") {
		t.Fatalf("repo ignore fix stdout = %q", stdout)
	}
}

func TestExtractZipExtractsFilesAndRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	archivePath := filepath.Join(root, "asset.zip")
	writeZipFixture(t, archivePath, map[string]string{
		"nested/tool": "payload\n",
	})

	destDir := filepath.Join(root, "extract")

	inlineErr9 := extractZip(archivePath, destDir)
	if inlineErr9 != nil {
		t.Fatalf("extract zip: %v", inlineErr9)
	}

	payload, err := os.ReadFile(filepath.Join(destDir, "nested", "tool"))
	if err != nil {
		t.Fatalf("read extracted zip member: %v", err)
	}

	if string(payload) != "payload\n" {
		t.Fatalf("zip payload = %q", payload)
	}

	traversalPath := filepath.Join(root, "traversal.zip")
	writeZipFixture(t, traversalPath, map[string]string{
		"../escape": "no\n",
	})

	inlineErrAutoF := extractZip(traversalPath, filepath.Join(root, "bad"))
	if inlineErrAutoF == nil {
		t.Fatal("extractZip should reject traversal member")
	}
}

func TestGithubAssetURLValidatesRequiredFlags(t *testing.T) {
	t.Parallel()

	inlineErrAutoG := githubAssetURL(nil)
	if !errors.Is(inlineErrAutoG, errRepoRequired) {
		t.Fatalf("githubAssetURL(nil) = %v, want %v", inlineErrAutoG, errRepoRequired)
	}

	inlineErrAutoH := githubAssetURL(
		[]string{"--repo", "owner/repo"},
	)
	if !errors.Is(
		inlineErrAutoH,
		errTagRequired,
	) {
		t.Fatalf(
			"githubAssetURL missing tag = %v, want %v",
			inlineErrAutoH,
			errTagRequired,
		)
	}

	err := githubAssetURL([]string{"--repo", "owner/repo", "--tag", "v1.0.0"})
	if !errors.Is(err, errAssetRequired) {
		t.Fatalf("githubAssetURL missing asset = %v, want %v", err, errAssetRequired)
	}
}

func TestRuntimeFixItemsReadsNonEmptyOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.out")

	inlineErr10 := os.WriteFile(path, []byte("failed\n"), privateFileMode)
	if inlineErr10 != nil {
		t.Fatalf("write runtime output: %v", inlineErr10)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat runtime output: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("runtime fixture should be non-empty")
	}
}

func TestRunCLIDispatchesToolchainCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	shaFile := filepath.Join(root, "payload.txt")

	err := os.WriteFile(shaFile, []byte("payload\n"), privateFileMode)
	if err != nil {
		t.Fatalf("write sha file: %v", err)
	}

	hooksDir := filepath.Join(root, "hooks")
	runner := filepath.Join(root, "coding-ethos-run")
	writeExecutableFixture(t, runner, "runner\n")

	repo := filepath.Join(root, "repo")

	err = os.Mkdir(repo, 0o700)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	runGit(t, repo, "init")

	agentOutput := filepath.Join(root, "agent.out")
	runtimeOutput := filepath.Join(root, "runtime.out")

	err = os.WriteFile(
		agentOutput,
		[]byte("Gemini .gemini/settings.json mismatch\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write agent output: %v", err)
	}

	err = os.WriteFile(runtimeOutput, []byte("runtime failed\n"), privateFileMode)
	if err != nil {
		t.Fatalf("write runtime output: %v", err)
	}

	for _, test := range toolchainCLICases(toolchainCLIPaths{
		shaFile:       shaFile,
		hooksDir:      hooksDir,
		runner:        runner,
		root:          root,
		repo:          repo,
		agentOutput:   agentOutput,
		runtimeOutput: runtimeOutput,
	}) {
		stdout := captureToolchainStdout(t, func() {
			if code := runCLI(test.args); code != 0 {
				t.Fatalf("%s exit = %d for args %#v", test.name, code, test.args)
			}
		})
		if test.want != "" && !strings.Contains(stdout, test.want) {
			t.Fatalf("%s stdout missing %q:\n%s", test.name, test.want, stdout)
		}
	}
}

type toolchainCLIPaths struct {
	shaFile       string
	hooksDir      string
	runner        string
	root          string
	repo          string
	agentOutput   string
	runtimeOutput string
}

type toolchainCLICase struct {
	name string
	want string
	args []string
}

func toolchainCLICases(paths toolchainCLIPaths) []toolchainCLICase {
	return []toolchainCLICase{
		{
			name: "sha256",
			args: []string{"sha256", "--file", paths.shaFile},
			want: "d4e4877bac978b7952f0d544fc52ebff5411d351d129f1f056fa43f11da9af2b",
		},
		{
			name: "install hooks",
			args: []string{
				"install-git-hooks", "--hooks-dir", paths.hooksDir, "--runner", paths.runner,
			},
		},
		{
			name: "verify hooks",
			args: []string{
				"verify-git-hooks", "--hooks-dir", paths.hooksDir, "--runner", paths.runner,
			},
		},
		toolchainGitHookFixItemsCase(paths),
		toolchainRepoIgnoreFixItemsCase(paths),
		{
			name: "agent hook fix items",
			args: []string{"agent-hook-fix-items", "--input", paths.agentOutput},
			want: ".gemini/settings.json",
		},
		{
			name: "runtime fix items",
			args: []string{"runtime-fix-items", "--input", paths.runtimeOutput},
			want: "git-hook validate failed",
		},
	}
}

func toolchainGitHookFixItemsCase(paths toolchainCLIPaths) toolchainCLICase {
	return toolchainCLICase{
		name: "git hook fix items",
		args: []string{
			"git-hook-fix-items",
			"--hooks-dir",
			filepath.Join(paths.root, "missing-hooks"),
			"--runner",
			paths.runner,
		},
		want: "pre-commit missing or not executable",
	}
}

func toolchainRepoIgnoreFixItemsCase(paths toolchainCLIPaths) toolchainCLICase {
	return toolchainCLICase{
		name: "repo ignore fix items",
		args: []string{
			"repo-ignore-fix-items",
			"--repo-root",
			paths.repo,
			"--real-git",
			"git",
		},
		want: ".coding-ethos/ is not ignored",
	}
}

func TestRunCLIReturnsUsageAndCommandErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "missing command",
			args: nil,
			want: commandArgsOffset,
		},
		{
			name: "unknown command",
			args: []string{"unknown"},
			want: commandArgsOffset,
		},
		{
			name: "command error",
			args: []string{"sha256"},
			want: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if code := runCLI(test.args); code != test.want {
				t.Fatalf("exit = %d, want %d", code, test.want)
			}
		})
	}
}

func TestFixItemCommandsPrintStructuredRows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	agentOutput := filepath.Join(root, "agent.out")
	runtimeOutput := filepath.Join(root, "runtime.out")

	err := os.WriteFile(
		agentOutput,
		[]byte("Gemini .gemini/settings.json mismatch\n"),
		privateFileMode,
	)
	if err != nil {
		t.Fatalf("write agent output: %v", err)
	}

	err = os.WriteFile(runtimeOutput, []byte("validate failed\n"), privateFileMode)
	if err != nil {
		t.Fatalf("write runtime output: %v", err)
	}

	agentStdout := captureToolchainStdout(t, func() {
		err := agentHookFixItems([]string{"--input", agentOutput})
		if err != nil {
			t.Fatalf("agentHookFixItems() returned error: %v", err)
		}
	})
	if !strings.Contains(agentStdout, "agent-hooks") ||
		!strings.Contains(agentStdout, ".gemini/settings.json") {
		t.Fatalf("agent fix items output = %q", agentStdout)
	}

	runtimeStdout := captureToolchainStdout(t, func() {
		err := runtimeFixItems([]string{"--input", runtimeOutput})
		if err != nil {
			t.Fatalf("runtimeFixItems() returned error: %v", err)
		}
	})
	if !strings.Contains(runtimeStdout, "policy-runtime") ||
		!strings.Contains(runtimeStdout, "git-hook validate failed") {
		t.Fatalf("runtime fix items output = %q", runtimeStdout)
	}

	_, inlineErrAutoI := inputFileFlag("test", nil)
	if !errors.Is(inlineErrAutoI, errInputRequired) {
		t.Fatalf(
			"inputFileFlag() error = %v, want %v",
			inlineErrAutoI,
			errInputRequired,
		)
	}
}

func TestFilesEqualAndSHACommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	other := filepath.Join(root, "other")

	inlineErr11 := os.WriteFile(left, []byte("same\n"), privateFileMode)
	if inlineErr11 != nil {
		t.Fatalf("write left: %v", inlineErr11)
	}

	inlineErr12 := os.WriteFile(right, []byte("same\n"), privateFileMode)
	if inlineErr12 != nil {
		t.Fatalf("write right: %v", inlineErr12)
	}

	inlineErr13 := os.WriteFile(other, []byte("different\n"), privateFileMode)
	if inlineErr13 != nil {
		t.Fatalf("write other: %v", inlineErr13)
	}

	equal, err := filesEqual(left, right)
	if err != nil || !equal {
		t.Fatalf("filesEqual(left,right) = %v, %v", equal, err)
	}

	equal, err = filesEqual(left, other)
	if err != nil || equal {
		t.Fatalf("filesEqual(left,other) = %v, %v", equal, err)
	}

	stdout := captureToolchainStdout(t, func() {
		err := printSHA256([]string{"--file", left})
		if err != nil {
			t.Fatalf("printSHA256() returned error: %v", err)
		}
	})
	if len(strings.TrimSpace(stdout)) != sha256.Size*2 {
		t.Fatalf("sha stdout = %q", stdout)
	}

	inlineErrAutoJ := printSHA256(nil)
	if !errors.Is(inlineErrAutoJ, errFileRequired) {
		t.Fatalf("printSHA256(nil) = %v, want %v", inlineErrAutoJ, errFileRequired)
	}
}

func captureToolchainStdout(t *testing.T, run func()) string {
	t.Helper()

	release := testlock.ProcessStateScope(t, "coding-ethos-toolchain")
	defer release()

	original := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	os.Stdout = writer

	defer func() {
		os.Stdout = original
	}()

	run()

	inlineErr14 := writer.Close()
	if inlineErr14 != nil {
		t.Fatalf("close stdout writer: %v", inlineErr14)
	}

	var buffer strings.Builder

	_, inlineErrB := io.Copy(&buffer, reader)
	if inlineErrB != nil {
		t.Fatalf("read stdout: %v", inlineErrB)
	}

	inlineErr15 := reader.Close()
	if inlineErr15 != nil {
		t.Fatalf("close stdout reader: %v", inlineErr15)
	}

	return buffer.String()
}

func captureToolchainStderr(t *testing.T, run func()) string {
	t.Helper()

	release := testlock.ProcessStateScope(t, "coding-ethos-toolchain")
	defer release()

	original := os.Stderr

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}

	os.Stderr = writer

	defer func() {
		os.Stderr = original
	}()

	run()

	inlineErr14 := writer.Close()
	if inlineErr14 != nil {
		t.Fatalf("close stderr writer: %v", inlineErr14)
	}

	var buffer strings.Builder

	_, inlineErrB := io.Copy(&buffer, reader)
	if inlineErrB != nil {
		t.Fatalf("read stderr: %v", inlineErrB)
	}

	inlineErr15 := reader.Close()
	if inlineErr15 != nil {
		t.Fatalf("close stderr reader: %v", inlineErr15)
	}

	return buffer.String()
}

func writeExecutableFixture(t *testing.T, path, payload string) {
	t.Helper()

	err := os.WriteFile(path, []byte(payload), privateFileMode)
	if err != nil {
		t.Fatalf("write executable fixture %s: %v", path, err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod executable fixture %s: %v", path, err)
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

	inlineErr16 := tarWriter.WriteHeader(header)
	if inlineErr16 != nil {
		t.Fatalf("write tar header: %v", inlineErr16)
	}

	_, inlineErrC := tarWriter.Write([]byte(payload))
	if inlineErrC != nil {
		t.Fatalf("write tar payload: %v", inlineErrC)
	}

	inlineErr17 := tarWriter.Close()
	if inlineErr17 != nil {
		t.Fatalf("close tar writer: %v", inlineErr17)
	}

	inlineErr18 := gzipWriter.Close()
	if inlineErr18 != nil {
		t.Fatalf("close gzip writer: %v", inlineErr18)
	}

	inlineErr19 := file.Close()
	if inlineErr19 != nil {
		t.Fatalf("close tar.gz fixture: %v", inlineErr19)
	}
}

func writeZipFixture(t *testing.T, path string, members map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip fixture: %v", err)
	}

	writer := zip.NewWriter(file)
	for name, payload := range members {
		member, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip member %s: %v", name, err)
		}

		_, inlineErrD := member.Write([]byte(payload))
		if inlineErrD != nil {
			t.Fatalf("write zip member %s: %v", name, inlineErrD)
		}
	}

	inlineErr20 := writer.Close()
	if inlineErr20 != nil {
		t.Fatalf("close zip writer: %v", inlineErr20)
	}

	inlineErr21 := file.Close()
	if inlineErr21 != nil {
		t.Fatalf("close zip fixture: %v", inlineErr21)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	command := exec.CommandContext(context.Background(), gitPath, args...)
	command.Dir = dir

	command.Env = append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

type fakeGitHubTransport func(*http.Request) *http.Response

func (transport fakeGitHubTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return transport(request), nil
}

func jsonResponse(body string) *http.Response {
	response := textStatusResponse(http.StatusOK, body)
	response.Header.Set("Content-Type", "application/json")

	return response
}

func textResponse(body string) *http.Response {
	return textStatusResponse(http.StatusOK, body)
}

func textStatusResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
