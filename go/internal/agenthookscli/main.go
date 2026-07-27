// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthookscli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/syncstate"
)

const (
	commandArgsOffset = 2
)

var (
	errProviderMatrixDrift = apperror.StaticError(
		"provider capability matrix out of sync",
	)
	errRuntimeVersionUnavailable = apperror.StaticError(
		"runtime version is unavailable from pyproject.toml",
	)
	errUnknownCommand = apperror.StaticError("unknown agent-hooks command")
)

type settingsCommandFlags struct {
	root               *string
	repoRoot           *string
	stateRoot          *string
	hookCommand        *string
	hookTimeoutSeconds *int
	mcpCommand         *string
}

func bindSettingsCommandFlags(flags *flag.FlagSet) settingsCommandFlags {
	return settingsCommandFlags{
		root: flags.String("root", ".", "Repository root for agent settings"),
		repoRoot: flags.String(
			"repo-root",
			"",
			"Actual repository root when --root is a private settings overlay",
		),
		stateRoot: flags.String(
			"state-root",
			"",
			"Private Coding Ethos state root; defaults to --root",
		),
		hookCommand: flags.String("hook-command", "", "Agent hook command"),
		hookTimeoutSeconds: flags.Int(
			"hook-timeout-seconds",
			agenthooks.DefaultHookTimeoutSeconds,
			"Provider hook timeout in seconds",
		),
		mcpCommand: flags.String(
			"mcp-command",
			"",
			"Coding Ethos MCP command; derived from --hook-command when omitted",
		),
	}
}

func runCLI(args []string) int {
	if len(args) == 0 {
		usage()

		return commandArgsOffset
	}

	var err error

	switch args[0] {
	case "print":
		err = printSettings(args[1:])
	case "sync":
		err = syncSettings(args[1:])
	case "doctor":
		err = doctorSettings(args[1:])
	case "verify":
		err = verifySettings(args[1:])
	case "capabilities":
		err = capabilities(args[1:])
	case "sync-provider-matrix":
		err = syncProviderMatrix(args[1:])
	case "check-provider-matrix":
		err = checkProviderMatrix(args[1:])
	default:
		usage()

		err = fmt.Errorf("%w: %s", errUnknownCommand, args[0])
	}

	if err != nil {
		feedback.Emit(
			os.Stderr,
			feedback.Error{Message: err.Error()},
			feedback.FormatTOON,
		)

		return 1
	}

	return 0
}

func capabilities(args []string) error {
	flags := flag.NewFlagSet("capabilities", flag.ContinueOnError)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse capabilities flags: %w", err)
	}

	runtimeVersion := syncstate.RuntimeVersion(*ethosRoot)
	if runtimeVersion == "" {
		return fmt.Errorf("%w: %s", errRuntimeVersionUnavailable, *ethosRoot)
	}

	err = feedback.WriteJSON(
		os.Stdout,
		agenthooks.Capabilities(runtimeVersion),
	)
	if err != nil {
		return fmt.Errorf("encode agent hook capabilities: %w", err)
	}

	return nil
}

func printSettings(args []string) error {
	flags := flag.NewFlagSet("print", flag.ContinueOnError)
	hookCommand := flags.String("hook-command", "", "Agent hook command")
	hookTimeoutSeconds := flags.Int(
		"hook-timeout-seconds",
		agenthooks.DefaultHookTimeoutSeconds,
		"Provider hook timeout in seconds",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse print flags: %w", err)
	}

	var buffer bytes.Buffer

	err = agenthooks.WriteSettingsWithOptions(
		&buffer,
		defaultHookCommand(*hookCommand),
		settingsOptions(*hookTimeoutSeconds),
	)
	if err != nil {
		return fmt.Errorf("write agent hook settings: %w", err)
	}

	err = feedback.WriteRendered(
		os.Stdout,
		strings.TrimSuffix(buffer.String(), "\n"),
		feedback.FormatJSON,
	)
	if err != nil {
		return fmt.Errorf("emit agent hook settings: %w", err)
	}

	return nil
}

func syncSettings(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	settings := bindSettingsCommandFlags(flags)
	ethosRoot := flags.String("ethos-root", ".", "Path to coding-ethos checkout")
	dryRun := flags.Bool("dry-run", false, "Report planned writes without mutating files")
	format := flags.String(
		"format",
		feedback.FormatTOON,
		"Output format for dry-run reports",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse sync flags: %w", err)
	}

	resolvedHookCommand := defaultHookCommand(*settings.hookCommand)
	resolvedRepoRoot := defaultRepoRoot(*settings.root, *settings.repoRoot)
	resolvedStateRoot := defaultStateRoot(*settings.root, *settings.stateRoot)
	options := settingsOptions(*settings.hookTimeoutSeconds)

	artifacts, err := agenthooks.StateArtifactsForRootsWithMCPCommandAndOptions(
		*settings.root,
		resolvedRepoRoot,
		resolvedStateRoot,
		resolvedHookCommand,
		*settings.mcpCommand,
		options,
	)
	if err != nil {
		return fmt.Errorf("plan agent hook settings: %w", err)
	}

	if *dryRun {
		return writeSyncStateReport(
			syncstate.Plan(*settings.root, "agent-hooks sync", artifacts),
			*format,
		)
	}

	err = applyAgentHookSettings(
		*settings.root,
		resolvedRepoRoot,
		resolvedStateRoot,
		resolvedHookCommand,
		*settings.mcpCommand,
		options,
	)
	if err != nil {
		return err
	}

	if privateSettingsOverlay(*settings.root, resolvedRepoRoot) {
		artifacts, err = agenthooks.StateArtifactsForRootsWithMCPCommandAndOptions(
			*settings.root,
			resolvedRepoRoot,
			resolvedStateRoot,
			resolvedHookCommand,
			*settings.mcpCommand,
			options,
		)
		if err != nil {
			return fmt.Errorf("refresh private overlay state artifacts: %w", err)
		}
	}

	return upsertAgentHookSyncState(*settings.root, *ethosRoot, artifacts)
}

func applyAgentHookSettings(
	root string,
	repoRoot string,
	stateRoot string,
	hookCommand string,
	mcpCommand string,
	options agenthooks.SettingsOptions,
) error {
	err := agenthooks.SyncSettingsForRootsWithMCPCommandAndOptions(
		root,
		repoRoot,
		stateRoot,
		hookCommand,
		mcpCommand,
		options,
	)
	if err != nil {
		return fmt.Errorf("sync agent hook settings: %w", err)
	}

	err = agenthooks.SyncCodexTrustStateWithOptions(
		root,
		hookCommand,
		codexTrustConfigForRoots(root, repoRoot),
		options,
	)
	if err != nil {
		return fmt.Errorf("sync Codex hook trust: %w", err)
	}

	return nil
}

func upsertAgentHookSyncState(
	root string,
	ethosRoot string,
	artifacts []syncstate.Artifact,
) error {
	_, err := syncstate.Upsert(syncstate.UpsertOptions{
		RepoRoot:        root,
		EthosRoot:       ethosRoot,
		RequestedAction: "agent-hooks sync",
		ProviderTargets: []syncstate.ProviderTarget{
			{Provider: "agent-hooks", Root: root},
		},
		Artifacts: artifacts,
	})
	if err != nil {
		return fmt.Errorf("write install sync state: %w", err)
	}

	return nil
}

func writeSyncStateReport(report syncstate.Report, format string) error {
	err := feedback.Write(os.Stdout, report, format)
	if err != nil {
		return fmt.Errorf("write install sync state report: %w", err)
	}

	return nil
}

func doctorSettings(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	settings := bindSettingsCommandFlags(flags)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse doctor flags: %w", err)
	}

	options := settingsOptions(*settings.hookTimeoutSeconds)

	err = agenthooks.DoctorSettingsForRootsWithMCPCommandAndOptions(
		*settings.root,
		defaultRepoRoot(*settings.root, *settings.repoRoot),
		defaultStateRoot(*settings.root, *settings.stateRoot),
		defaultHookCommand(*settings.hookCommand),
		*settings.mcpCommand,
		options,
	)
	if err != nil {
		return fmt.Errorf("doctor agent hook settings: %w", err)
	}

	resolvedRepoRoot := defaultRepoRoot(*settings.root, *settings.repoRoot)

	err = agenthooks.VerifyCodexTrustStateWithOptions(
		*settings.root,
		defaultHookCommand(*settings.hookCommand),
		codexTrustConfigForRoots(*settings.root, resolvedRepoRoot),
		options,
	)
	if err != nil {
		return fmt.Errorf("doctor Codex hook trust: %w", err)
	}

	err = writeDoctorReport(os.Stdout)
	if err != nil {
		return err
	}

	return nil
}

func verifySettings(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	settings := bindSettingsCommandFlags(flags)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse verify flags: %w", err)
	}

	options := settingsOptions(*settings.hookTimeoutSeconds)

	report, err := agenthooks.VerifySettingsForRootsWithMCPCommandAndOptions(
		*settings.root,
		defaultRepoRoot(*settings.root, *settings.repoRoot),
		defaultStateRoot(*settings.root, *settings.stateRoot),
		defaultHookCommand(*settings.hookCommand),
		*settings.mcpCommand,
		options,
	)
	if err != nil {
		encodeErr := writeJSONReport(os.Stdout, report)
		if encodeErr != nil {
			return encodeErr
		}

		return fmt.Errorf("verify agent hook settings: %w", err)
	}

	resolvedRepoRoot := defaultRepoRoot(*settings.root, *settings.repoRoot)

	err = agenthooks.VerifyCodexTrustStateWithOptions(
		*settings.root,
		defaultHookCommand(*settings.hookCommand),
		codexTrustConfigForRoots(*settings.root, resolvedRepoRoot),
		options,
	)
	if err != nil {
		report.Status = "invalid"
		report.Checks = append(report.Checks, agenthooks.VerifyCheck{
			Provider: "codex",
			Event:    "hook-trust",
			Tool:     "config.toml",
			Status:   "fail",
			Detail:   err.Error(),
		})

		encodeErr := writeJSONReport(os.Stdout, report)
		if encodeErr != nil {
			return encodeErr
		}

		return fmt.Errorf("verify Codex hook trust: %w", err)
	}

	report.Checks = append(report.Checks, agenthooks.VerifyCheck{
		Provider: "codex",
		Event:    "hook-trust",
		Tool:     "config.toml",
		Status:   "pass",
	})

	err = writeJSONReport(os.Stdout, report)
	if err != nil {
		return err
	}

	return nil
}

func syncProviderMatrix(args []string) error {
	flags := flag.NewFlagSet("sync-provider-matrix", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for generated docs")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse sync-provider-matrix flags: %w", err)
	}

	_, err = agenthooks.SyncProviderCapabilityMatrix(*root)
	if err != nil {
		return fmt.Errorf("sync provider capability matrix: %w", err)
	}

	return nil
}

func checkProviderMatrix(args []string) error {
	flags := flag.NewFlagSet("check-provider-matrix", flag.ContinueOnError)
	root := flags.String("root", ".", "Repository root for generated docs")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse check-provider-matrix flags: %w", err)
	}

	mismatched, err := agenthooks.CheckProviderCapabilityMatrix(*root)
	if err != nil {
		return fmt.Errorf("check provider capability matrix: %w", err)
	}

	if len(mismatched) != 0 {
		return fmt.Errorf(
			"%w: %s",
			errProviderMatrixDrift,
			strings.Join(mismatched, ", "),
		)
	}

	return nil
}

func defaultHookCommand(hookCommand string) string {
	if strings.TrimSpace(hookCommand) != "" {
		return hookCommand
	}

	runner := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runner == "" {
		return ""
	}

	return runner + " agent-hook"
}

func settingsOptions(hookTimeoutSeconds int) agenthooks.SettingsOptions {
	return agenthooks.SettingsOptions{HookTimeoutSeconds: hookTimeoutSeconds}
}

func defaultRepoRoot(settingsRoot, repoRoot string) string {
	if strings.TrimSpace(repoRoot) != "" {
		return repoRoot
	}

	return settingsRoot
}

func defaultStateRoot(settingsRoot, stateRoot string) string {
	if strings.TrimSpace(stateRoot) != "" {
		return stateRoot
	}

	return settingsRoot
}

func privateSettingsOverlay(settingsRoot, repoRoot string) bool {
	var (
		settingsPath, settingsErr = filepath.Abs(settingsRoot)
		repoPath, repoErr         = filepath.Abs(repoRoot)
	)

	if settingsErr != nil || repoErr != nil {
		return filepath.Clean(settingsRoot) != filepath.Clean(repoRoot)
	}

	return filepath.Clean(settingsPath) != filepath.Clean(repoPath)
}

func codexTrustConfigForRoots(settingsRoot, repoRoot string) string {
	if !privateSettingsOverlay(settingsRoot, repoRoot) {
		return ""
	}

	return agenthooks.DefaultSettingsPaths(settingsRoot).CodexConfig
}

func writeDoctorReport(file *os.File) error {
	payload := map[string]any{
		"status":       "valid",
		"capabilities": agenthooks.ProviderCapabilities(),
	}

	return writeJSONReport(file, payload)
}

func writeJSONReport(file *os.File, payload any) error {
	err := feedback.WriteJSON(file, payload)
	if err != nil {
		return fmt.Errorf("encode doctor report: %w", err)
	}

	return nil
}

func usage() {
	usageTo(os.Stderr)
}

func usageTo(writer io.Writer) {
	const text = "Usage: coding-ethos-agent-hooks " +
		"<print|sync|doctor|verify|capabilities|" +
		"sync-provider-matrix|check-provider-matrix> " +
		"[flags]; sync supports --repo-root, --state-root, --dry-run, " +
		"and --format json|toon"

	feedback.Emit(
		writer,
		feedback.Text{
			Text: text,
		},
		feedback.FormatTOON,
	)
}
