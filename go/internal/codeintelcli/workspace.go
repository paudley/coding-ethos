// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

var (
	errWorkspaceCommandRequired = apperror.StaticError("workspace command is required")
	errWorkspaceCommandUnknown  = apperror.StaticError("unknown workspace command")
)

type workspaceCommand func(context.Context, []string) error

func workspace(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errWorkspaceCommandRequired
	}

	handler, ok := workspaceCommandHandlers()[args[0]]
	if !ok {
		return fmt.Errorf("%w: %q", errWorkspaceCommandUnknown, args[0])
	}

	return handler(ctx, args[1:])
}

func workspaceCommandHandlers() map[string]workspaceCommand {
	return map[string]workspaceCommand{
		"add":     workspaceAdd,
		"list":    workspaceList,
		"refresh": workspaceRefresh,
		"remove":  workspaceRemove,
		"scan":    workspaceScan,
		"status":  workspaceStatus,
	}
}

func workspaceAdd(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace add", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")
	alias := flags.String("alias", "", "Repository alias")
	repo := flags.String("repo", "", "Repository path")

	err := parseCommandFlags(flags, args, "workspace add")
	if err != nil {
		return err
	}

	return workspaceAddWithFlags(*root, *alias, *repo)
}

func workspaceAddWithFlags(root, alias, repo string) error {
	registry, err := codeintel.AddWorkspaceRepo(root, alias, repo)
	if err != nil {
		return fmt.Errorf("add workspace repo: %w", err)
	}

	return encodeJSON(os.Stdout, registry)
}

func workspaceRemove(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace remove", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")
	alias := flags.String("alias", "", "Repository alias")

	err := parseCommandFlags(flags, args, "workspace remove")
	if err != nil {
		return err
	}

	return workspaceRemoveWithFlags(*root, *alias)
}

func workspaceRemoveWithFlags(root, alias string) error {
	registry, err := codeintel.RemoveWorkspaceRepo(root, alias)
	if err != nil {
		return fmt.Errorf("remove workspace repo: %w", err)
	}

	return encodeJSON(os.Stdout, registry)
}

func workspaceScan(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace scan", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")

	err := parseCommandFlags(flags, args, "workspace scan")
	if err != nil {
		return err
	}

	return workspaceScanWithFlags(*root)
}

func workspaceScanWithFlags(root string) error {
	registry, warnings, err := codeintel.ScanWorkspaceRepos(root)
	if err != nil {
		return fmt.Errorf("scan workspace repos: %w", err)
	}

	return encodeJSON(os.Stdout, map[string]any{
		"registry": registry,
		"warnings": warnings,
	})
}

func workspaceList(_ context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace list", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")
	format := flags.String("format", outputFormatJSON, "Output format: json or toon")

	err := parseCommandFlags(flags, args, "workspace list")
	if err != nil {
		return err
	}

	return workspaceListWithFlags(*root, *format)
}

func workspaceListWithFlags(root, format string) error {
	registry, err := codeintel.LoadWorkspaceRegistry(root)
	if err != nil {
		return fmt.Errorf("load workspace registry: %w", err)
	}

	return writeWorkspaceOutput(registry, format)
}

func workspaceStatus(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace status", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")
	format := flags.String("format", outputFormatJSON, "Output format: json or toon")

	err := parseCommandFlags(flags, args, "workspace status")
	if err != nil {
		return err
	}

	registry, err := codeintel.LoadWorkspaceRegistry(*root)
	if err != nil {
		return fmt.Errorf("load workspace registry: %w", err)
	}

	status, err := codeintel.WorkspaceStatusForRegistry(ctx, registry)
	if err != nil {
		return fmt.Errorf("read workspace status: %w", err)
	}

	return writeWorkspaceOutput(status, *format)
}

func workspaceRefresh(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workspace refresh", flag.ExitOnError)
	root := flags.String("root", ".", "Workspace root")
	format := flags.String("format", outputFormatJSON, "Output format: json or toon")

	err := parseCommandFlags(flags, args, "workspace refresh")
	if err != nil {
		return err
	}

	status, err := codeintel.RefreshWorkspaceStatus(ctx, *root)
	if err != nil {
		return fmt.Errorf("refresh workspace status: %w", err)
	}

	return writeWorkspaceOutput(status, *format)
}

func writeWorkspaceOutput(value any, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", outputFormatJSON:
		return encodeJSON(os.Stdout, value)
	case outputFormatTOON:
		err := feedback.WriteRendered(
			os.Stdout,
			formatWorkspaceTOON(value),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write workspace TOON output: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnknownDownstreamAnalysisFormat, format)
	}
}

func formatWorkspaceTOON(value any) string {
	switch typed := value.(type) {
	case codeintel.WorkspaceRegistry:
		return formatWorkspaceRegistryTOON(typed)
	case codeintel.WorkspaceStatus:
		return formatWorkspaceStatusTOON(typed)
	default:
		return "unsupported workspace output"
	}
}

func formatWorkspaceRegistryTOON(registry codeintel.WorkspaceRegistry) string {
	var builder strings.Builder
	builder.WriteString("coding_ethos_workspace:\n")
	builder.WriteString("  root: " + strconv.Quote(registry.WorkspaceRoot) + "\n")
	builder.WriteString("  repos:\n")

	for _, repo := range registry.Repos {
		builder.WriteString("    - alias: " + strconv.Quote(repo.Alias) + "\n")
		builder.WriteString("      path: " + strconv.Quote(repo.Path) + "\n")
		builder.WriteString("      code_intel_db: " + strconv.Quote(repo.CodeIntelDB) + "\n")
	}

	return builder.String()
}

func formatWorkspaceStatusTOON(status codeintel.WorkspaceStatus) string {
	var builder strings.Builder
	builder.WriteString("coding_ethos_workspace_status:\n")
	builder.WriteString("  root: " + strconv.Quote(status.Root) + "\n")
	builder.WriteString("  repos: " + strconv.Itoa(status.Stats.Repos) + "\n")
	builder.WriteString("  stale: " + strconv.Itoa(status.Stats.Stale) + "\n")
	builder.WriteString("  cochanges: " + strconv.Itoa(status.Stats.CoChanges) + "\n")
	builder.WriteString("  registered:\n")

	for _, repo := range status.Repos {
		builder.WriteString("    - alias: " + strconv.Quote(repo.Alias) + "\n")
		builder.WriteString("      path: " + strconv.Quote(repo.Path) + "\n")
		builder.WriteString("      stale: " + strconv.FormatBool(repo.Stale) + "\n")

		if repo.StaleWarning != "" {
			builder.WriteString("      warning: " + strconv.Quote(repo.StaleWarning) + "\n")
		}
	}

	return builder.String()
}
