// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

const outputFormatTOON = "toon"

var (
	errUnknownOutputCommand    = apperror.StaticError("unknown output command")
	errUnsupportedReportFormat = apperror.StaticError("unsupported output report format")
	errUnsupportedPruneFormat  = apperror.StaticError("unsupported output prune format")
)

type outputCommand func(context.Context, []string) error

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()

		return nil
	}

	handler, ok := commandHandlers()[args[0]]
	if !ok {
		return fmt.Errorf("%w: %s", errUnknownOutputCommand, args[0])
	}

	return handler(ctx, args[1:])
}

func commandHandlers() map[string]outputCommand {
	return map[string]outputCommand{
		"prune":  prune,
		"report": report,
	}
}

func report(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("report", flag.ExitOnError)
	root := flags.String(
		"root",
		".",
		"Repository root containing coding-ethos output surfaces",
	)
	format := flags.String("format", "", "Output format: toon, human, or json")
	includeTemp := flags.Bool(
		"include-temp",
		false,
		"Include OS temp-directory output surfaces",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse output report flags: %w", err)
	}

	settings, err := outputsurface.LoadSettings(*root)
	if err != nil {
		return fmt.Errorf("load output report settings: %w", err)
	}

	*format = outputFormatOrDefault(*format, settings)

	if settings.Report.IncludeTemp {
		*includeTemp = true
	}

	report, err := outputsurface.BuildReport(ctx, outputsurface.Options{
		Root:        *root,
		IncludeTemp: *includeTemp,
		Settings:    &settings,
	})
	if err != nil {
		return fmt.Errorf("build output report: %w", err)
	}

	switch *format {
	case "json":
		return encodeJSON(report)
	case outputFormatTOON:
		_, err = os.Stdout.WriteString(outputsurface.FormatTOON(report))
	case "human":
		_, err = os.Stdout.WriteString(outputsurface.FormatHuman(report))
	default:
		return fmt.Errorf("%w: %q", errUnsupportedReportFormat, *format)
	}

	if err != nil {
		return fmt.Errorf("write output report: %w", err)
	}

	return nil
}

func prune(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("prune", flag.ExitOnError)
	root := flags.String(
		"root",
		".",
		"Repository root containing coding-ethos output surfaces",
	)
	format := flags.String(
		"format",
		"",
		"Output format: toon, human, or json",
	)
	scope := flags.String(
		"scope",
		"",
		"Comma-separated output surface IDs to prune",
	)
	olderThan := flags.String(
		"older-than",
		"",
		"Override max age, for example 24h or 30d",
	)
	includeTemp := flags.Bool(
		"include-temp",
		false,
		"Include OS temp-directory output surfaces",
	)
	dryRun := flags.Bool("dry-run", false, "Preview prune candidates without deleting")
	apply := flags.Bool("apply", false, "Delete selected candidates")
	all := flags.Bool("all", false, "Consider every command-prunable surface")
	vacuum := flags.Bool(
		"vacuum",
		false,
		"Run code-intel VACUUM when code_intel_db is selected",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse output prune flags: %w", err)
	}

	settings, err := outputsurface.LoadSettings(*root)
	if err != nil {
		return fmt.Errorf("load output prune settings: %w", err)
	}

	*format = outputFormatOrDefault(*format, settings)

	maxAge, err := outputsurface.ParseDuration(*olderThan)
	if err != nil {
		return fmt.Errorf("parse --older-than: %w", err)
	}

	report, err := outputsurface.Prune(ctx, outputsurface.PruneOptions{
		Root:        *root,
		Settings:    settings,
		Scopes:      splitScopes(*scope),
		OlderThan:   maxAge,
		IncludeTemp: *includeTemp,
		Apply:       *apply && !*dryRun,
		All:         *all,
		Vacuum:      *vacuum,
	})
	if err != nil {
		return fmt.Errorf("prune output surfaces: %w", err)
	}

	switch *format {
	case "json":
		return encodeJSON(report)
	case outputFormatTOON:
		_, err = os.Stdout.WriteString(outputsurface.FormatPruneTOON(report))
	case "human":
		_, err = os.Stdout.WriteString(outputsurface.FormatPruneHuman(report))
	default:
		return fmt.Errorf("%w: %q", errUnsupportedPruneFormat, *format)
	}

	if err != nil {
		return fmt.Errorf("write output prune report: %w", err)
	}

	return nil
}

func splitScopes(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return strings.Split(value, ",")
}

func outputFormatOrDefault(format string, settings outputsurface.Settings) string {
	if format != "" {
		return format
	}

	if settings.Report.DefaultFormat != "" {
		return settings.Report.DefaultFormat
	}

	return outputFormatTOON
}

func encodeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(value)
	if err != nil {
		return fmt.Errorf("encode output report JSON: %w", err)
	}

	return nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: coding-ethos-run output <command> [options]")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  prune     preview or apply output surface pruning")
	fmt.Fprintln(os.Stderr, "  report    inventory coding-ethos disk output surfaces")
}
