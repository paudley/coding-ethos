// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolchaincli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

type managedTool struct {
	Tool           string
	Installer      string
	Source         string
	Version        string
	AssetSubstring string
	Binary         string
	Checksum       string
	Dest           string
}

type managedToolInstaller struct {
	InstallGo     func(module, version, destDir string) error
	InstallNPM    npmInstallFunc
	InstallRust   func(crate, version, binary, destDir string) error
	InstallGitHub func(tool managedTool, destDir string) error
}

type npmInstallFunc func(
	packageName,
	version,
	binary,
	integrity,
	lockDir,
	destDir string,
) error

type managedToolchainDiagnosticError struct {
	cause      error
	diagnostic diagnostics.Diagnostic
}

func (err managedToolchainDiagnosticError) Error() string {
	return err.cause.Error()
}

func (err managedToolchainDiagnosticError) Unwrap() error {
	return err.cause
}

func (err managedToolchainDiagnosticError) Diagnostics() []diagnostics.Diagnostic {
	return []diagnostics.Diagnostic{err.diagnostic}
}

func installManagedToolchainCommand(args []string) error {
	flags := flag.NewFlagSet("install-managed-toolchain", flag.ExitOnError)
	manifestSource := flags.String(
		"manifest-source",
		"",
		"Managed toolchain source manifest",
	)
	goBinDir := flags.String("go-bin-dir", "", "Managed Go binary directory")
	githubBinDir := flags.String(
		"github-bin-dir",
		"",
		"Managed GitHub binary directory",
	)

	installedManifest := flags.String(
		"installed-manifest",
		"",
		"Installed manifest path",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse install-managed-toolchain flags: %w", err)
	}

	if strings.TrimSpace(*manifestSource) == "" {
		return errManifestRequired
	}

	if strings.TrimSpace(*goBinDir) == "" {
		return apperror.StaticError("install-managed-toolchain requires --go-bin-dir")
	}

	if strings.TrimSpace(*githubBinDir) == "" {
		return apperror.StaticError(
			"install-managed-toolchain requires --github-bin-dir",
		)
	}

	if strings.TrimSpace(*installedManifest) == "" {
		return apperror.StaticError(
			"install-managed-toolchain requires --installed-manifest",
		)
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

	inlineErr1 := os.MkdirAll(filepath.Dir(installedManifest), directoryMode)
	if inlineErr1 != nil {
		return fmt.Errorf(
			"create managed manifest dir %s: %w",
			filepath.Dir(installedManifest),
			inlineErr1,
		)
	}

	records := make([]string, 0, len(tools))
	for _, tool := range tools {
		destDir, err := managedToolDestDir(tool.Dest, goBinDir, githubBinDir)
		if err != nil {
			return managedToolchainManifestDiagnostic(
				err,
				manifestSource,
				tool,
				"invalid-destination",
				"Managed tool destination is invalid.",
			)
		}

		installedPath := filepath.Join(destDir, tool.Binary)

		record := managedToolManifestRecord(tool, installedPath)
		if !managedToolAlreadyInstalled(installedManifest, installedPath, record) {
			err := installManagedTool(
				tool,
				filepath.Dir(manifestSource),
				destDir,
				installer,
			)
			if err != nil {
				return managedToolchainManifestDiagnostic(
					err,
					manifestSource,
					tool,
					"install-failed",
					"Managed tool installation failed.",
				)
			}
		}

		inlineErr2 := requireExecutableManagedTool(tool.Tool, installedPath)
		if inlineErr2 != nil {
			return managedToolchainExecutableDiagnostic(
				inlineErr2,
				manifestSource,
				installedManifest,
				tool,
				installedPath,
			)
		}

		records = append(records, record)
	}

	return writeManagedToolInstalledManifest(installedManifest, records)
}

func readManagedToolManifest(path string) ([]managedTool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, managedToolchainFileDiagnostic(
			fmt.Errorf("managed toolchain manifest not found %s: %w", path, err),
			path,
			"manifest-not-found",
			"Managed toolchain source manifest is missing.",
		)
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
		if len(fields) != toolManifestFields {
			return nil, managedToolchainLineDiagnostic(
				apperror.Wrapf(
					apperror.StaticError(
						"invalid managed toolchain manifest line %d: expected 8 tab-separated fields",
					),
					"invalid managed toolchain manifest line %d: expected 8 tab-separated fields",
					lineNumber,
				),
				path,
				lineNumber,
				"invalid-field-count",
				"Managed toolchain manifest row has the wrong field count.",
			)
		}

		tool := managedTool{
			Tool:           fields[0],
			Installer:      fields[1],
			Source:         fields[2],
			Version:        fields[3],
			AssetSubstring: fields[4],
			Binary:         fields[5],
			Checksum:       fields[6],
			Dest:           fields[7],
		}

		err := validateManagedTool(tool, lineNumber)
		if err != nil {
			return nil, managedToolchainManifestDiagnostic(
				err,
				path,
				tool,
				"missing-required-field",
				"Managed toolchain manifest row is missing a required field.",
			)
		}

		tools = append(tools, tool)
	}

	inlineErr3 := scanner.Err()
	if inlineErr3 != nil {
		return nil, managedToolchainFileDiagnostic(
			fmt.Errorf(
				"read managed toolchain manifest %s: %w",
				path,
				inlineErr3,
			),
			path,
			"manifest-read-failed",
			"Managed toolchain source manifest could not be read.",
		)
	}

	return tools, nil
}

func managedToolchainFileDiagnostic(
	err error,
	path string,
	code string,
	message string,
) error {
	return managedToolchainDiagnosticError{
		cause: err,
		diagnostic: managedToolchainDiagnostic(
			path,
			0,
			"",
			code,
			message,
			map[string]any{"manifest_source": path},
		),
	}
}

func managedToolchainLineDiagnostic(
	err error,
	path string,
	line int,
	code string,
	message string,
) error {
	return managedToolchainDiagnosticError{
		cause: err,
		diagnostic: managedToolchainDiagnostic(
			path,
			line,
			"",
			code,
			message,
			map[string]any{"manifest_source": path},
		),
	}
}

func managedToolchainManifestDiagnostic(
	err error,
	manifestSource string,
	tool managedTool,
	code string,
	message string,
) error {
	metadata := managedToolchainToolMetadata(manifestSource, tool)

	return managedToolchainDiagnosticError{
		cause: err,
		diagnostic: managedToolchainDiagnostic(
			manifestSource,
			0,
			tool.Tool,
			code,
			message,
			metadata,
		),
	}
}

func managedToolchainExecutableDiagnostic(
	err error,
	manifestSource string,
	installedManifest string,
	tool managedTool,
	installedPath string,
) error {
	metadata := managedToolchainToolMetadata(manifestSource, tool)
	metadata["installed_manifest"] = installedManifest
	metadata["installed_path"] = installedPath

	return managedToolchainDiagnosticError{
		cause: err,
		diagnostic: managedToolchainDiagnostic(
			installedPath,
			0,
			tool.Tool,
			"managed-tool-not-executable",
			"Managed tool binary is missing or not executable.",
			metadata,
		),
	}
}

func managedToolchainToolMetadata(
	manifestSource string,
	tool managedTool,
) map[string]any {
	return map[string]any{
		"asset_substring": tool.AssetSubstring,
		"binary":          tool.Binary,
		"dest":            tool.Dest,
		"installer":       tool.Installer,
		"manifest_source": manifestSource,
		"source":          tool.Source,
		"tool":            tool.Tool,
		"version":         tool.Version,
	}
}

func managedToolchainDiagnostic(
	file string,
	line int,
	toolName string,
	code string,
	message string,
	metadata map[string]any,
) diagnostics.Diagnostic {
	if metadata == nil {
		metadata = map[string]any{}
	}

	metadata["repair_command"] = "make build"

	return diagnostics.Diagnostic{
		Tool:     "managed-toolchain",
		File:     file,
		Line:     line,
		Severity: "error",
		Code:     code,
		PolicyID: "toolchain.managed_manifest",
		SkillID:  "managed-toolchain",
		Message:  message,
		Advice: "Repair the managed toolchain through the canonical build path " +
			"instead of installing host-global tools.",
		AdviceSteps: []string{
			"Run make build from the coding-ethos checkout.",
			"Keep pre-commit/hooks/managed-toolchain.tsv tab-separated with 8 fields.",
			"Do not edit build/toolchain/manifest.tsv by hand.",
		},
		Rerun: []string{"make build"},
		PrincipleIDs: []string{
			"static-analysis-is-the-first-line-of-defense",
			"validation-at-the-gate",
			"one-path-for-critical-operations",
		},
		Metadata: metadata,
		Detail:   managedToolchainDiagnosticDetail(toolName),
	}
}

func managedToolchainDiagnosticDetail(toolName string) string {
	if strings.TrimSpace(toolName) == "" {
		return "Managed toolchain manifest validation failed before tool install."
	}

	return "Managed toolchain validation failed for tool " + toolName + "."
}

func validateManagedTool(tool managedTool, lineNumber int) error {
	for name, value := range map[string]string{
		"tool":            tool.Tool,
		"installer":       tool.Installer,
		"source":          tool.Source,
		"version":         tool.Version,
		"asset_substring": tool.AssetSubstring,
		"binary":          tool.Binary,
		"checksum":        tool.Checksum,
		"dest":            tool.Dest,
	} {
		if strings.TrimSpace(value) == "" {
			return apperror.Wrapf(
				apperror.StaticError(
					"invalid managed toolchain manifest line %d: %s is required",
				),
				"invalid managed toolchain manifest line %d: %s is required",
				lineNumber,
				name,
			)
		}
	}

	return nil
}

func ensureAbsoluteDir(path string) (string, error) {
	inlineErr4 := os.MkdirAll(path, directoryMode)
	if inlineErr4 != nil {
		return "", fmt.Errorf("create managed tool dir %s: %w", path, inlineErr4)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve managed tool dir %s: %w", path, err)
	}

	return absolute, nil
}

func managedToolDestDir(dest, goBinDir, githubBinDir string) (string, error) {
	switch dest {
	case "go-bin":
		return goBinDir, nil
	case "github-bin":
		return githubBinDir, nil
	default:
		return "", apperror.Wrapf(
			apperror.StaticError("unknown managed tool destination: %s"),
			"unknown managed tool destination: %s",
			dest,
		)
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
			tool.Checksum,
			installedPath,
		},
		"\t",
	)
}

func managedToolAlreadyInstalled(installedManifest, installedPath, record string) bool {
	info, err := os.Stat(installedPath)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return false
	}

	payload, err := os.ReadFile(installedManifest)
	if err != nil {
		return false
	}

	return slices.Contains(strings.Split(string(payload), "\n"), record)
}

func installManagedTool(
	tool managedTool,
	manifestDir string,
	destDir string,
	installer managedToolInstaller,
) error {
	switch tool.Installer {
	case "go":
		return installer.InstallGo(tool.Source, tool.Version, destDir)
	case "npm":
		return installer.InstallNPM(
			tool.Source,
			tool.Version,
			tool.Binary,
			tool.Checksum,
			npmLockDir(manifestDir, tool.AssetSubstring),
			destDir,
		)
	case "rust":
		return installer.InstallRust(tool.Source, tool.Version, tool.Binary, destDir)
	case "github":
		return installer.InstallGitHub(tool, destDir)
	default:
		return apperror.Wrapf(
			apperror.StaticError("unknown installer %q for managed tool %q"),
			"unknown installer %q for managed tool %q",
			tool.Installer,
			tool.Tool,
		)
	}
}

func npmLockDir(manifestDir, lockPath string) string {
	if filepath.IsAbs(lockPath) {
		return lockPath
	}

	return filepath.Join(manifestDir, lockPath)
}

func realManagedToolInstaller() managedToolInstaller {
	return managedToolInstaller{
		InstallGo: func(module, version, destDir string) error {
			command := safeexec.Command("go", "install", module+"@"+version)

			command.Env = append(os.Environ(), "GOBIN="+destDir)

			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf(
					"install Go tool %s@%s: %w: %s",
					module,
					version,
					err,
					strings.TrimSpace(string(output)),
				)
			}

			return nil
		},
		InstallNPM: installNPMBinary,
		InstallRust: func(crate, version, binary, destDir string) error {
			workDir, err := os.MkdirTemp(destDir, ".coding-ethos-cargo.")
			if err != nil {
				return fmt.Errorf("create cargo install workspace: %w", err)
			}
			defer os.RemoveAll(workDir)

			command := safeexec.CommandContext(
				context.Background(),
				"cargo",
				"install",
				crate,
				"--version",
				version,
				"--locked",
				"--root",
				workDir,
			)

			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf(
					"install Rust tool %s@%s: %w: %s",
					crate,
					version,
					err,
					strings.TrimSpace(string(output)),
				)
			}

			return installBinaryFile(
				filepath.Join(workDir, "bin", binary),
				filepath.Join(destDir, binary),
			)
		},
		InstallGitHub: func(tool managedTool, destDir string) error {
			return installGitHubBinary(
				http.DefaultClient,
				tool.Source,
				tool.Version,
				tool.AssetSubstring,
				tool.Binary,
				destDir,
				tool.Checksum,
				os.Getenv("GITHUB_TOKEN"),
			)
		},
	}
}

func requireExecutableManagedTool(tool, installedPath string) error {
	info, err := os.Stat(installedPath)
	if err != nil {
		return fmt.Errorf(
			"managed tool %s was not installed as executable: %s: %w",
			tool,
			installedPath,
			err,
		)
	}

	if info.IsDir() || info.Mode()&0o111 == 0 {
		return apperror.Wrapf(
			apperror.StaticError("managed tool %s was not installed as executable: %s"),
			"managed tool %s was not installed as executable: %s",
			tool,
			installedPath,
		)
	}

	return nil
}

func writeManagedToolInstalledManifest(path string, records []string) error {
	payload := strings.Builder{}
	payload.WriteString(
		"# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>\n",
	)
	payload.WriteString("# SPDX-License-Identifier: MIT\n")
	payload.WriteString(
		"# Generated by coding-ethos-toolchain install-managed-toolchain. Do not edit.\n",
	)
	payload.WriteString(
		"# tool\tinstaller\tsource\tversion\tasset_substring\tbinary\tchecksum\tpath\n",
	)

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

	_, inlineErrA := tmp.WriteString(payload.String())
	if inlineErrA != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temporary managed manifest %s: %w", path, inlineErrA)
	}

	inlineErr5 := tmp.Close()
	if inlineErr5 != nil {
		return fmt.Errorf("close temporary managed manifest %s: %w", path, inlineErr5)
	}

	inlineErr6 := os.Rename(tmpPath, path)
	if inlineErr6 != nil {
		return fmt.Errorf("install managed manifest %s: %w", path, inlineErr6)
	}

	return nil
}
