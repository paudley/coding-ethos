// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	commandArgIndex   = 1
	commandArgsOffset = 2
	githubAPIVersion  = "2022-11-28"
)

var (
	errAssetNotFound    = errors.New("release asset not found")
	errActionRequired   = errors.New("cutover-report requires --action")
	errFileRequired     = errors.New("sha256 requires --file")
	errFixItemsOpen     = errors.New("cutover-report fix item file must be readable")
	errRepoRequired     = errors.New("github-asset-url requires --repo")
	errTagRequired      = errors.New("github-asset-url requires --tag")
	errAssetRequired    = errors.New("github-asset-url requires --asset-substring")
	errBinaryRequired   = errors.New("install-github-binary requires --binary")
	errDestRequired     = errors.New("install-git-shim requires --dest-dir")
	errGitRequired      = errors.New("install-git-shim requires --real-git")
	errHashRequired     = errors.New("install-github-binary requires --sha256")
	errHookRequired     = errors.New("install-git-shim requires --runner")
	errHooksRequired    = errors.New("git hook shim command requires --hooks-dir")
	errInputRequired    = errors.New("fix item command requires --input")
	errManifestRequired = errors.New(
		"install-managed-toolchain requires --manifest-source",
	)
	errRealGitRequired = errors.New(
		"repo-ignore-fix-items requires --real-git",
	)
	errRepoRootRequired = errors.New(
		"repo-ignore-fix-items requires --repo-root",
	)
)

var (
	gitHookNames = []string{"pre-commit", "pre-push", "commit-msg"}
	lfsHookNames = []string{
		"post-commit",
		"post-merge",
		"post-checkout",
	}
)

type release struct {
	Assets []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func main() {
	if len(os.Args) < commandArgsOffset {
		usage()
		os.Exit(commandArgsOffset)
	}

	var err error
	switch os.Args[commandArgIndex] {
	case "agent-hook-fix-items":
		err = agentHookFixItems(os.Args[commandArgsOffset:])
	case "cutover-report":
		err = cutoverReport(os.Args[commandArgsOffset:])
	case "cutover-verify":
		err = cutoverVerify(os.Args[commandArgsOffset:])
	case "github-asset-url":
		err = githubAssetURL(os.Args[commandArgsOffset:])
	case "install-github-binary":
		err = installGitHubBinaryCommand(os.Args[commandArgsOffset:])
	case "install-managed-toolchain":
		err = installManagedToolchainCommand(os.Args[commandArgsOffset:])
	case "install-git-hooks":
		err = installGitHooks(os.Args[commandArgsOffset:])
	case "install-git-shim":
		err = installGitShimCommand(os.Args[commandArgsOffset:])
	case "git-hook-fix-items":
		err = gitHookFixItems(os.Args[commandArgsOffset:])
	case "repo-ignore-fix-items":
		err = repoIgnoreFixItems(os.Args[commandArgsOffset:])
	case "runtime-fix-items":
		err = runtimeFixItems(os.Args[commandArgsOffset:])
	case "sha256":
		err = printSHA256(os.Args[commandArgsOffset:])
	case "verify-git-hooks":
		err = verifyGitHooks(os.Args[commandArgsOffset:])
	default:
		usage()
		os.Exit(commandArgsOffset)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func cutoverVerify(args []string) error {
	flags := flag.NewFlagSet("cutover-verify", flag.ExitOnError)
	action := flags.String("action", "verify", "Cutover action")
	root := flags.String("root", "", "Repository root")
	runner := flags.String("runner", "", "runner path")
	hooksDir := flags.String("hooks-dir", "", "Git hooks directory")
	flags.String("source-dir", "", "Deprecated; hook files are generated from --runner")
	realGit := flags.String("real-git", "", "Real git executable")
	bundleRoot := flags.String("bundle-root", "", "Policy bundle root")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse cutover-verify flags: %w", err)
	}
	for name, value := range map[string]string{
		"root":        *root,
		"runner":      *runner,
		"hooks-dir":   *hooksDir,
		"real-git":    *realGit,
		"bundle-root": *bundleRoot,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("cutover-verify requires --%s", name)
		}
	}

	status := "ready"
	surfaces := map[string]string{
		"git-hooks":      "PASS",
		"agent-hooks":    "PASS",
		"repo-ignores":   "PASS",
		"policy-runtime": "PASS",
	}
	fixItems := make([]string, 0)

	gitItems, err := gitHookShimFixItems(*hooksDir, *runner)
	if err != nil {
		return err
	}
	if len(gitItems) > 0 {
		status = "blocked"
		surfaces["git-hooks"] = "FAIL"
		fixItems = append(fixItems, gitItems...)
	}

	agentOutput, agentErr := runCutoverCommand(
		[]string{*runner, "agent-hooks", "verify", "--root", *root},
		map[string]string{"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1"},
	)
	if agentErr != nil {
		status = "blocked"
		surfaces["agent-hooks"] = "FAIL"
		fixItems = append(fixItems, agentHookFixItemLines(agentOutput)...)
	}

	repoIgnoreOutput, repoIgnoreErr := runCutoverCommand(
		[]string{*runner, "policy-lint", "--scope", "cutover", "--cwd", *root, "--json"},
		map[string]string{"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1"},
	)
	if repoIgnoreErr != nil {
		status = "blocked"
		surfaces["repo-ignores"] = "FAIL"
		items, err := repoIgnoreFixItemLines(*realGit, *root)
		if err != nil {
			return err
		}
		fixItems = append(fixItems, items...)
	}

	runtimeOutput, runtimeErr := runCutoverCommand(
		[]string{*runner, "git-hook", "validate"},
		map[string]string{
			"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1",
			"CODE_ETHOS_PRECOMMIT_ROOT":      *bundleRoot,
		},
	)
	if runtimeErr != nil {
		status = "blocked"
		surfaces["policy-runtime"] = "FAIL"
		fixItems = append(fixItems, runtimeFixItemLines(runtimeOutput)...)
	}

	report := cutoverStatusReport{
		Action:   *action,
		Status:   status,
		Repo:     *root,
		Surfaces: surfaces,
		FixItems: fixItems,
	}
	for _, line := range cutoverReportLines(report) {
		fmt.Fprintln(os.Stdout, line)
	}
	if status == "ready" {
		return nil
	}
	if agentErr != nil {
		fmt.Fprintln(os.Stderr, "agent hook verify output:")
		fmt.Fprint(os.Stderr, agentOutput)
	}
	if runtimeErr != nil {
		fmt.Fprintln(os.Stderr, "policy runtime verify output:")
		fmt.Fprint(os.Stderr, runtimeOutput)
	}
	if repoIgnoreErr != nil {
		fmt.Fprintln(os.Stderr, "repo ignore verify output:")
		fmt.Fprint(os.Stderr, repoIgnoreOutput)
	}

	return errors.New("cutover verification blocked")
}

func runCutoverCommand(args []string, env map[string]string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("cutover command args are required")
	}
	command := exec.Command(args[0], args[1:]...)
	command.Env = os.Environ()
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()

	return string(output), err
}

type managedTool struct {
	Tool           string
	Installer      string
	Source         string
	Version        string
	AssetSubstring string
	Binary         string
	SHA256         string
	Dest           string
}

type managedToolInstaller struct {
	InstallGo     func(module string, version string, destDir string) error
	InstallRust   func(crate string, version string, binary string, destDir string) error
	InstallGitHub func(tool managedTool, destDir string) error
}

func installManagedToolchainCommand(args []string) error {
	flags := flag.NewFlagSet("install-managed-toolchain", flag.ExitOnError)
	manifestSource := flags.String("manifest-source", "", "Managed toolchain source manifest")
	goBinDir := flags.String("go-bin-dir", "", "Managed Go binary directory")
	githubBinDir := flags.String("github-bin-dir", "", "Managed GitHub binary directory")
	installedManifest := flags.String("installed-manifest", "", "Installed manifest path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse install-managed-toolchain flags: %w", err)
	}
	if strings.TrimSpace(*manifestSource) == "" {
		return errManifestRequired
	}
	if strings.TrimSpace(*goBinDir) == "" {
		return errors.New("install-managed-toolchain requires --go-bin-dir")
	}
	if strings.TrimSpace(*githubBinDir) == "" {
		return errors.New("install-managed-toolchain requires --github-bin-dir")
	}
	if strings.TrimSpace(*installedManifest) == "" {
		return errors.New("install-managed-toolchain requires --installed-manifest")
	}

	return installManagedToolchain(
		*manifestSource,
		*goBinDir,
		*githubBinDir,
		*installedManifest,
		realManagedToolInstaller(),
	)
}

func installManagedToolchain(
	manifestSource string,
	goBinDir string,
	githubBinDir string,
	installedManifest string,
	installer managedToolInstaller,
) error {
	tools, err := readManagedToolManifest(manifestSource)
	if err != nil {
		return err
	}
	goBinDir, err = ensureAbsoluteDir(goBinDir)
	if err != nil {
		return err
	}
	githubBinDir, err = ensureAbsoluteDir(githubBinDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(installedManifest), 0o755); err != nil {
		return fmt.Errorf("create managed manifest dir %s: %w", filepath.Dir(installedManifest), err)
	}

	records := make([]string, 0, len(tools))
	for _, tool := range tools {
		destDir, err := managedToolDestDir(tool.Dest, goBinDir, githubBinDir)
		if err != nil {
			return err
		}
		installedPath := filepath.Join(destDir, tool.Binary)
		record := managedToolManifestRecord(tool, installedPath)
		if !managedToolAlreadyInstalled(installedManifest, installedPath, record) {
			if err := installManagedTool(tool, destDir, installer); err != nil {
				return err
			}
		}
		if err := requireExecutableManagedTool(tool.Tool, installedPath); err != nil {
			return err
		}
		records = append(records, record)
	}

	return writeManagedToolInstalledManifest(installedManifest, records)
}

func readManagedToolManifest(path string) ([]managedTool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("managed toolchain manifest not found %s: %w", path, err)
	}
	defer file.Close()

	tools := make([]managedTool, 0)
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 8 {
			return nil, fmt.Errorf("invalid managed toolchain manifest line %d: expected 8 tab-separated fields", lineNumber)
		}
		tool := managedTool{
			Tool:           fields[0],
			Installer:      fields[1],
			Source:         fields[2],
			Version:        fields[3],
			AssetSubstring: fields[4],
			Binary:         fields[5],
			SHA256:         fields[6],
			Dest:           fields[7],
		}
		if err := validateManagedTool(tool, lineNumber); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read managed toolchain manifest %s: %w", path, err)
	}

	return tools, nil
}

func validateManagedTool(tool managedTool, lineNumber int) error {
	for name, value := range map[string]string{
		"tool":            tool.Tool,
		"installer":       tool.Installer,
		"source":          tool.Source,
		"version":         tool.Version,
		"asset_substring": tool.AssetSubstring,
		"binary":          tool.Binary,
		"sha256":          tool.SHA256,
		"dest":            tool.Dest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("invalid managed toolchain manifest line %d: %s is required", lineNumber, name)
		}
	}

	return nil
}

func ensureAbsoluteDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create managed tool dir %s: %w", path, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve managed tool dir %s: %w", path, err)
	}

	return absolute, nil
}

func managedToolDestDir(dest string, goBinDir string, githubBinDir string) (string, error) {
	switch dest {
	case "go-bin":
		return goBinDir, nil
	case "github-bin":
		return githubBinDir, nil
	default:
		return "", fmt.Errorf("unknown managed tool destination: %s", dest)
	}
}

func managedToolManifestRecord(tool managedTool, installedPath string) string {
	return strings.Join(
		[]string{
			tool.Tool,
			tool.Installer,
			tool.Source,
			tool.Version,
			tool.AssetSubstring,
			tool.Binary,
			tool.SHA256,
			installedPath,
		},
		"\t",
	)
}

func managedToolAlreadyInstalled(installedManifest string, installedPath string, record string) bool {
	info, err := os.Stat(installedPath)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return false
	}
	payload, err := os.ReadFile(installedManifest)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(payload), "\n") {
		if line == record {
			return true
		}
	}

	return false
}

func installManagedTool(tool managedTool, destDir string, installer managedToolInstaller) error {
	switch tool.Installer {
	case "go":
		return installer.InstallGo(tool.Source, tool.Version, destDir)
	case "rust":
		return installer.InstallRust(tool.Source, tool.Version, tool.Binary, destDir)
	case "github":
		return installer.InstallGitHub(tool, destDir)
	default:
		return fmt.Errorf("unknown installer %q for managed tool %q", tool.Installer, tool.Tool)
	}
}

func realManagedToolInstaller() managedToolInstaller {
	return managedToolInstaller{
		InstallGo: func(module string, version string, destDir string) error {
			command := exec.Command("go", "install", module+"@"+version)
			command.Env = append(os.Environ(), "GOBIN="+destDir)
			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf("install Go tool %s@%s: %w: %s", module, version, err, strings.TrimSpace(string(output)))
			}

			return nil
		},
		InstallRust: func(crate string, version string, binary string, destDir string) error {
			workDir, err := os.MkdirTemp(destDir, ".coding-ethos-cargo.")
			if err != nil {
				return fmt.Errorf("create cargo install workspace: %w", err)
			}
			defer os.RemoveAll(workDir)
			command := exec.Command("cargo", "install", crate, "--version", version, "--locked", "--root", workDir)
			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf("install Rust tool %s@%s: %w: %s", crate, version, err, strings.TrimSpace(string(output)))
			}
			return installBinaryFile(filepath.Join(workDir, "bin", binary), filepath.Join(destDir, binary))
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			return installGitHubBinary(
				http.DefaultClient,
				tool.Source,
				tool.Version,
				tool.AssetSubstring,
				tool.Binary,
				destDir,
				tool.SHA256,
				os.Getenv("GITHUB_TOKEN"),
			)
		},
	}
}

func requireExecutableManagedTool(tool string, installedPath string) error {
	info, err := os.Stat(installedPath)
	if err != nil {
		return fmt.Errorf("managed tool %s was not installed as executable: %s: %w", tool, installedPath, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("managed tool %s was not installed as executable: %s", tool, installedPath)
	}

	return nil
}

func writeManagedToolInstalledManifest(path string, records []string) error {
	payload := strings.Builder{}
	payload.WriteString("# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>\n")
	payload.WriteString("# SPDX-License-Identifier: MIT\n")
	payload.WriteString("# Generated by coding-ethos-toolchain install-managed-toolchain. Do not edit.\n")
	payload.WriteString("# tool\tinstaller\tsource\tversion\tasset_substring\tbinary\tsha256\tpath\n")
	for _, record := range records {
		payload.WriteString(record)
		payload.WriteByte('\n')
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("create temporary managed manifest %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(payload.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary managed manifest %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary managed manifest %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install managed manifest %s: %w", path, err)
	}

	return nil
}

func installGitHubBinaryCommand(args []string) error {
	flags := flag.NewFlagSet("install-github-binary", flag.ExitOnError)
	repo := flags.String("repo", "", "GitHub repository in owner/name form")
	tag := flags.String("tag", "", "Release tag")
	assetSubstring := flags.String("asset-substring", "", "Release asset name substring")
	binary := flags.String("binary", "", "Binary name to install")
	destDir := flags.String("dest-dir", "", "Destination directory")
	expectedSHA256 := flags.String("sha256", "", "Expected release asset SHA-256")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse install-github-binary flags: %w", err)
	}
	switch {
	case strings.TrimSpace(*repo) == "":
		return errRepoRequired
	case strings.TrimSpace(*tag) == "":
		return errTagRequired
	case strings.TrimSpace(*assetSubstring) == "":
		return errAssetRequired
	case strings.TrimSpace(*binary) == "":
		return errBinaryRequired
	case strings.TrimSpace(*destDir) == "":
		return errDestRequired
	case strings.TrimSpace(*expectedSHA256) == "":
		return errHashRequired
	}

	return installGitHubBinary(
		http.DefaultClient,
		*repo,
		*tag,
		*assetSubstring,
		*binary,
		*destDir,
		*expectedSHA256,
		os.Getenv("GITHUB_TOKEN"),
	)
}

func installGitHubBinary(
	client *http.Client,
	repo string,
	tag string,
	assetSubstring string,
	binary string,
	destDir string,
	expectedSHA256 string,
	token string,
) error {
	url, err := releaseAssetURL(client, repo, tag, assetSubstring, token)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create managed binary destination %s: %w", destDir, err)
	}

	workDir, err := os.MkdirTemp(destDir, ".coding-ethos-download.")
	if err != nil {
		return fmt.Errorf("create download workspace in %s: %w", destDir, err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, downloadedFileName(url))
	if err := downloadGitHubAsset(client, url, archivePath, token); err != nil {
		return err
	}
	actualSHA256, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if actualSHA256 != expectedSHA256 {
		return fmt.Errorf(
			"SHA-256 mismatch for %s: expected %s, actual %s",
			archivePath,
			expectedSHA256,
			actualSHA256,
		)
	}

	return installDownloadedAsset(archivePath, binary, destDir, filepath.Join(workDir, "extract"))
}

func downloadedFileName(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "asset"
	}
	name := filepath.Base(parsedURL.Path)
	if name == "." || name == "/" || name == "" {
		return "asset"
	}

	return name
}

func downloadGitHubAsset(client *http.Client, rawURL string, outputPath string, token string) error {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create GitHub asset request: %w", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	}

	httpClient := clientWithTimeout(client)
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download GitHub asset %s: %w", rawURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf(
			"download GitHub asset %s: status %d: %s",
			rawURL,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create downloaded asset %s: %w", outputPath, err)
	}
	if _, err := io.Copy(output, response.Body); err != nil {
		_ = output.Close()
		return fmt.Errorf("write downloaded asset %s: %w", outputPath, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close downloaded asset %s: %w", outputPath, err)
	}

	return nil
}

func installDownloadedAsset(archivePath string, binary string, destDir string, extractDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"), strings.HasSuffix(archivePath, ".tgz"):
		if err := extractTarGzip(archivePath, extractDir); err != nil {
			return err
		}
	case strings.HasSuffix(archivePath, ".zip"):
		if err := extractZip(archivePath, extractDir); err != nil {
			return err
		}
	case strings.HasSuffix(archivePath, ".tar.xz"), strings.HasSuffix(archivePath, ".txz"):
		if err := extractTarXZ(archivePath, extractDir); err != nil {
			return err
		}
	default:
		return installBinaryFile(archivePath, filepath.Join(destDir, binary))
	}

	found, err := findExecutableNamed(extractDir, binary)
	if err != nil {
		return err
	}

	return installBinaryFile(found, filepath.Join(destDir, binary))
}

func extractTarGzip(archivePath string, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open tar.gz asset %s: %w", archivePath, err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip asset %s: %w", archivePath, err)
	}
	defer gzipReader.Close()

	return extractTar(tar.NewReader(gzipReader), destDir)
}

func extractTar(tarReader *tar.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, err)
	}
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar asset: %w", err)
		}
		target, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create tar directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create tar parent directory %s: %w", filepath.Dir(target), err)
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, header.FileInfo().Mode())
			if err != nil {
				return fmt.Errorf("create tar file %s: %w", target, err)
			}
			if _, err := io.Copy(output, tarReader); err != nil {
				_ = output.Close()
				return fmt.Errorf("write tar file %s: %w", target, err)
			}
			if err := output.Close(); err != nil {
				return fmt.Errorf("close tar file %s: %w", target, err)
			}
		}
	}
}

func extractZip(archivePath string, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip asset %s: %w", archivePath, err)
	}
	defer reader.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, err)
	}
	for _, file := range reader.File {
		target, err := safeExtractPath(destDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create zip directory %s: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create zip parent directory %s: %w", filepath.Dir(target), err)
		}
		source, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip member %s: %w", file.Name, err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.FileInfo().Mode())
		if err != nil {
			_ = source.Close()
			return fmt.Errorf("create zip file %s: %w", target, err)
		}
		if _, err := io.Copy(output, source); err != nil {
			_ = source.Close()
			_ = output.Close()
			return fmt.Errorf("write zip file %s: %w", target, err)
		}
		if err := source.Close(); err != nil {
			_ = output.Close()
			return fmt.Errorf("close zip member %s: %w", file.Name, err)
		}
		if err := output.Close(); err != nil {
			return fmt.Errorf("close zip file %s: %w", target, err)
		}
	}

	return nil
}

func extractTarXZ(archivePath string, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, err)
	}
	command := exec.Command("tar", "-xJf", archivePath, "-C", destDir)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract tar.xz asset %s: %w: %s", archivePath, err, strings.TrimSpace(string(output)))
	}

	return nil
}

func safeExtractPath(destDir string, memberName string) (string, error) {
	cleanName := filepath.Clean(memberName)
	if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive member escapes extract dir: %s", memberName)
	}
	target := filepath.Join(destDir, cleanName)
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", fmt.Errorf("resolve archive member %s: %w", memberName, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive member escapes extract dir: %s", memberName)
	}

	return target, nil
}

func findExecutableNamed(root string, binary string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != binary || found != "" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 {
			found = path
			return filepath.SkipAll
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scan extracted asset %s: %w", root, err)
	}
	if found == "" {
		return "", fmt.Errorf("%s not found as executable in downloaded GitHub asset", binary)
	}

	return found, nil
}

func installBinaryFile(source string, target string) error {
	payload, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read binary %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create binary destination dir %s: %w", filepath.Dir(target), err)
	}

	return writeExecutableFile(target, payload)
}

func cutoverReport(args []string) error {
	flags := flag.NewFlagSet("cutover-report", flag.ExitOnError)
	action := flags.String("action", "", "Cutover action")
	status := flags.String("status", "", "Overall cutover status")
	repo := flags.String("repo", "", "Repository root")
	gitHooks := flags.String("git-hooks", "", "Git hook status")
	agentHooks := flags.String("agent-hooks", "", "Agent hook status")
	repoIgnores := flags.String("repo-ignores", "", "Repository ignore status")
	runtime := flags.String("runtime", "", "Policy runtime status")
	fixItems := flags.String("fix-items", "", "File containing TOON fix rows")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse cutover-report flags: %w", err)
	}

	report, err := newCutoverReport(
		*action,
		*status,
		*repo,
		map[string]string{
			"git-hooks":      *gitHooks,
			"agent-hooks":    *agentHooks,
			"repo-ignores":   *repoIgnores,
			"policy-runtime": *runtime,
		},
		*fixItems,
	)
	if err != nil {
		return err
	}
	for _, line := range cutoverReportLines(report) {
		fmt.Fprintln(os.Stdout, line)
	}

	return nil
}

type cutoverStatusReport struct {
	Action     string
	Status     string
	Repo       string
	Surfaces   map[string]string
	FixItems   []string
	HasFixFile bool
}

func newCutoverReport(
	action string,
	status string,
	repo string,
	surfaces map[string]string,
	fixItemsPath string,
) (cutoverStatusReport, error) {
	report := cutoverStatusReport{
		Action:   strings.TrimSpace(action),
		Status:   strings.TrimSpace(status),
		Repo:     strings.TrimSpace(repo),
		Surfaces: surfaces,
	}
	if report.Action == "" {
		return report, errActionRequired
	}
	if report.Status == "" {
		return report, errors.New("cutover-report requires --status")
	}
	if report.Repo == "" {
		return report, errors.New("cutover-report requires --repo")
	}
	for _, surface := range cutoverSurfaceOrder() {
		if strings.TrimSpace(report.Surfaces[surface]) == "" {
			return report, fmt.Errorf("cutover-report requires --%s", surface)
		}
	}
	if strings.TrimSpace(fixItemsPath) == "" {
		return report, nil
	}

	payload, err := os.ReadFile(fixItemsPath)
	if err != nil {
		return report, fmt.Errorf("%w: %s: %w", errFixItemsOpen, fixItemsPath, err)
	}
	report.HasFixFile = true
	for _, line := range strings.Split(string(payload), "\n") {
		if strings.TrimSpace(line) != "" {
			report.FixItems = append(report.FixItems, line)
		}
	}

	return report, nil
}

func cutoverReportLines(report cutoverStatusReport) []string {
	lines := []string{
		"format: toon",
		"command: cutover",
		"action: " + report.Action,
		"status: " + report.Status,
		"repo: " + report.Repo,
		"surfaces[4]{name,status}:",
	}
	for _, surface := range cutoverSurfaceOrder() {
		lines = append(lines, fmt.Sprintf("  %s,%s", surface, report.Surfaces[surface]))
	}
	if len(report.FixItems) > 0 {
		lines = append(lines, fmt.Sprintf("fix_first[%d]{surface,problem,action}:", len(report.FixItems)))
		lines = append(lines, report.FixItems...)
	}

	return lines
}

func cutoverSurfaceOrder() []string {
	return []string{"git-hooks", "agent-hooks", "repo-ignores", "policy-runtime"}
}

func installGitShimCommand(args []string) error {
	flags := flag.NewFlagSet("install-git-shim", flag.ExitOnError)
	destDir := flags.String("dest-dir", "", "Directory where the git shim is installed")
	realGit := flags.String("real-git", "", "Real git executable")
	runner := flags.String("runner", "", "runner path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse install-git-shim flags: %w", err)
	}
	if strings.TrimSpace(*destDir) == "" {
		return errDestRequired
	}
	if strings.TrimSpace(*realGit) == "" {
		return errGitRequired
	}
	if strings.TrimSpace(*runner) == "" {
		return errHookRequired
	}

	return installGitShim(*destDir, *realGit, *runner)
}

func installGitShim(destDir string, realGit string, runner string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create git shim dir %s: %w", destDir, err)
	}

	shim := filepath.Join(destDir, "git")
	payload := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"export CODING_ETHOS_REAL_GIT=" + shellQuote(realGit),
		"exec " + shellQuote(runner) + ` policy-git "$@"`,
		"",
	}, "\n")

	return writeExecutableFile(shim, []byte(payload))
}

func installGitHooks(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("install-git-hooks", args)
	if err != nil {
		return err
	}

	for _, hook := range gitHookNames {
		if err := installHookEntrypoint(hooksDir, runner, hook); err != nil {
			return err
		}
	}
	for _, hook := range lfsHookNames {
		if err := installHookEntrypoint(hooksDir, runner, hook); err != nil {
			return err
		}
	}

	return nil
}

func verifyGitHooks(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("verify-git-hooks", args)
	if err != nil {
		return err
	}

	stale, err := gitHookShimFixItems(hooksDir, runner)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return errors.New("git hook entrypoints missing or stale")
	}

	return nil
}

func gitHookFixItems(args []string) error {
	hooksDir, runner, err := gitHookShimFlags("git-hook-fix-items", args)
	if err != nil {
		return err
	}

	items, err := gitHookShimFixItems(hooksDir, runner)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func agentHookFixItems(args []string) error {
	input, err := inputFileFlag("agent-hook-fix-items", args)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read agent hook verify output %s: %w", input, err)
	}
	for _, item := range agentHookFixItemLines(string(payload)) {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func agentHookFixItemLines(output string) []string {
	if strings.Contains(output, "settings do not contain expected hooks for all providers") {
		return []string{
			"  agent-hooks,native agent settings missing or stale,run cutover install",
		}
	}

	items := make([]string, 0, 2)
	if strings.Contains(output, "Codex hooks feature") ||
		strings.Contains(output, "codex_hooks") {
		items = append(
			items,
			"  agent-hooks,.codex/config.toml missing codex_hooks=true,run cutover install",
		)
	}
	if strings.Contains(output, ".gemini/settings.json") ||
		strings.Contains(output, "Gemini") {
		items = append(
			items,
			"  agent-hooks,.gemini/settings.json missing expected hook,run cutover install",
		)
	}

	return items
}

func runtimeFixItems(args []string) error {
	input, err := inputFileFlag("runtime-fix-items", args)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read runtime verify output %s: %w", input, err)
	}
	for _, item := range runtimeFixItemLines(string(payload)) {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func runtimeFixItemLines(output string) []string {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	return []string{
		"  policy-runtime,git-hook validate failed,inspect policy runtime validation output",
	}
}

func repoIgnoreFixItems(args []string) error {
	flags := flag.NewFlagSet("repo-ignore-fix-items", flag.ExitOnError)
	repoRoot := flags.String("repo-root", "", "Repository root")
	realGit := flags.String("real-git", "", "Real git executable")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse repo-ignore-fix-items flags: %w", err)
	}
	if strings.TrimSpace(*repoRoot) == "" {
		return errRepoRootRequired
	}
	if strings.TrimSpace(*realGit) == "" {
		return errRealGitRequired
	}

	items, err := repoIgnoreFixItemLines(*realGit, *repoRoot)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintln(os.Stdout, item)
	}

	return nil
}

func repoIgnoreFixItemLines(realGit string, repoRoot string) ([]string, error) {
	requiredIgnores := []string{
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	}

	items := make([]string, 0, len(requiredIgnores))
	for _, requiredIgnore := range requiredIgnores {
		ignored, err := gitCheckIgnore(realGit, repoRoot, requiredIgnore)
		if err != nil {
			return nil, err
		}
		if !ignored {
			items = append(
				items,
				fmt.Sprintf(
					"  repo-ignores,%s is not ignored,add .coding-ethos/ to .gitignore",
					requiredIgnore,
				),
			)
		}
	}

	return items, nil
}

func gitCheckIgnore(realGit string, repoRoot string, path string) (bool, error) {
	command := exec.Command(realGit, "-C", repoRoot, "check-ignore", "--quiet", path)
	err := command.Run()
	if err == nil {
		return true, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("git check-ignore %s: %w", path, err)
}

func inputFileFlag(command string, args []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	input := flags.String("input", "", "Input file to parse")
	if err := flags.Parse(args); err != nil {
		return "", fmt.Errorf("parse %s flags: %w", command, err)
	}
	if strings.TrimSpace(*input) == "" {
		return "", errInputRequired
	}

	return *input, nil
}

func gitHookShimFlags(command string, args []string) (string, string, error) {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	hooksDir := flags.String("hooks-dir", "", "Git hooks directory")
	runner := flags.String("runner", "", "coding-ethos-run executable")
	flags.String("source-dir", "", "Deprecated; hook files are generated from --runner")
	if err := flags.Parse(args); err != nil {
		return "", "", fmt.Errorf("parse %s flags: %w", command, err)
	}
	if strings.TrimSpace(*hooksDir) == "" {
		return "", "", errHooksRequired
	}
	if strings.TrimSpace(*runner) == "" {
		return "", "", errHookRequired
	}

	return *hooksDir, *runner, nil
}

func installHookEntrypoint(hooksDir string, runner string, hookName string) error {
	target := filepath.Join(hooksDir, hookName)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir %s: %w", hooksDir, err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove existing hook %s: %w", target, err)
	}

	command := "git-hook"
	if slices.Contains(lfsHookNames, hookName) {
		command = "lfs-hook"
	}
	payload := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"exec " + shellQuote(runner) + " " + command + " " + shellQuote(hookName) + ` "$@"`,
		"",
	}, "\n")

	return writeExecutableFile(target, []byte(payload))
}

func gitHookShimFixItems(hooksDir string, runner string) ([]string, error) {
	items := make([]string, 0)
	for _, hook := range gitHookNames {
		item, err := hookShimFixItem(hooksDir, runner, hook)
		if err != nil {
			return nil, err
		}
		if item != "" {
			items = append(items, item)
		}
	}
	for _, hook := range lfsHookNames {
		item, err := hookShimFixItem(hooksDir, runner, hook)
		if err != nil {
			return nil, err
		}
		if item != "" {
			items = append(items, item)
		}
	}

	return items, nil
}

func hookShimFixItem(
	hooksDir string,
	runner string,
	hookName string,
) (string, error) {
	target := filepath.Join(hooksDir, hookName)
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf(
				"  git-hooks,%s missing or not executable,run cutover install",
				target,
			), nil
		}

		return "", fmt.Errorf("stat hook shim %s: %w", target, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Sprintf(
			"  git-hooks,%s missing or not executable,run cutover install",
			target,
		), nil
	}

	matches, err := hookEntrypointTargetsRunner(target, runner, hookName)
	if err != nil {
		return "", err
	}
	if !matches {
		return fmt.Sprintf(
			"  git-hooks,%s does not route to coding-ethos-run,run cutover install",
			target,
		), nil
	}

	return "", nil
}

func hookEntrypointTargetsRunner(target string, runner string, hookName string) (bool, error) {
	payload, err := os.ReadFile(target)
	if err != nil {
		return false, fmt.Errorf("read hook entrypoint %s: %w", target, err)
	}
	command := "git-hook"
	if slices.Contains(lfsHookNames, hookName) {
		command = "lfs-hook"
	}

	want := "exec " + shellQuote(runner) + " " + command + " " + shellQuote(hookName) + ` "$@"`

	return strings.Contains(string(payload), want), nil
}

func filesEqual(left string, right string) (bool, error) {
	leftPayload, err := os.ReadFile(left)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", left, err)
	}
	rightPayload, err := os.ReadFile(right)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", right, err)
	}
	if len(leftPayload) != len(rightPayload) {
		return false, nil
	}

	return subtle.ConstantTimeCompare(leftPayload, rightPayload) == 1, nil
}

func writeExecutableFile(path string, payload []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}

	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func githubAssetURL(args []string) error {
	flags := flag.NewFlagSet("github-asset-url", flag.ExitOnError)
	repo := flags.String("repo", "", "GitHub repository in owner/name form")
	tag := flags.String("tag", "", "Release tag")
	assetSubstring := flags.String("asset-substring", "", "Release asset name substring")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse github-asset-url flags: %w", err)
	}
	if strings.TrimSpace(*repo) == "" {
		return errRepoRequired
	}
	if strings.TrimSpace(*tag) == "" {
		return errTagRequired
	}
	if strings.TrimSpace(*assetSubstring) == "" {
		return errAssetRequired
	}

	url, err := releaseAssetURL(http.DefaultClient, *repo, *tag, *assetSubstring, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, url)

	return nil
}

func releaseAssetURL(
	client *http.Client,
	repo string,
	tag string,
	assetSubstring string,
	token string,
) (string, error) {
	requestURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := clientWithTimeout(client).Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub release %s@%s: %w", repo, tag, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf(
			"fetch GitHub release %s@%s: status %d: %s",
			repo,
			tag,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var payload release
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode GitHub release %s@%s: %w", repo, tag, err)
	}

	for _, asset := range payload.Assets {
		if strings.Contains(asset.Name, assetSubstring) {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("%w: no release asset for %s@%s contains %q", errAssetNotFound, repo, tag, assetSubstring)
}

func clientWithTimeout(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	if client.Timeout != 0 {
		return client
	}

	copyClient := *client
	copyClient.Timeout = 30 * time.Second

	return &copyClient
}

func printSHA256(args []string) error {
	flags := flag.NewFlagSet("sha256", flag.ExitOnError)
	path := flags.String("file", "", "File to hash")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse sha256 flags: %w", err)
	}
	if strings.TrimSpace(*path) == "" {
		return errFileRequired
	}

	sum, err := sha256File(*path)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, sum)

	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  coding-ethos-toolchain github-asset-url --repo owner/name --tag v1.2.3 --asset-substring linux-amd64
  coding-ethos-toolchain agent-hook-fix-items --input agent-verify-output.txt
  coding-ethos-toolchain cutover-report --action verify --status ready --repo . --git-hooks PASS --agent-hooks PASS --repo-ignores PASS --runtime PASS
  coding-ethos-toolchain cutover-verify --root . --runner bin/coding-ethos-run --hooks-dir .git/hooks --real-git /usr/bin/git --bundle-root pre-commit
  coding-ethos-toolchain install-github-binary --repo owner/name --tag v1.2.3 --asset-substring linux-amd64 --binary tool --dest-dir bin --sha256 abc123
  coding-ethos-toolchain install-git-hooks --hooks-dir .git/hooks --runner bin/coding-ethos-run
  coding-ethos-toolchain install-git-shim --dest-dir bin --real-git /usr/bin/git --runner bin/coding-ethos-run
  coding-ethos-toolchain install-managed-toolchain --manifest-source managed-toolchain.tsv --go-bin-dir build/toolchain/go-bin --github-bin-dir build/toolchain/github-bin --installed-manifest build/toolchain/manifest.tsv
  coding-ethos-toolchain git-hook-fix-items --hooks-dir .git/hooks --runner bin/coding-ethos-run
  coding-ethos-toolchain repo-ignore-fix-items --repo-root . --real-git /usr/bin/git
  coding-ethos-toolchain runtime-fix-items --input runtime-output.txt
  coding-ethos-toolchain sha256 --file path
  coding-ethos-toolchain verify-git-hooks --hooks-dir .git/hooks --runner bin/coding-ethos-run
`)
}
