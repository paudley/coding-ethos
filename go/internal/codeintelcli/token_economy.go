// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:wsl_v5 // CLI validation and evidence stages stay visibly separated.
package codeintelcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/tokeneconomy"
)

const (
	reportKindKey             = "kind"
	tokenEconomyCommand       = "token-economy"
	tokenEconomyReportCommand = "report"
)

var (
	errTokenEconomySubcommand = errors.New("token-economy subcommand is required")
	errUnknownTokenEconomy    = errors.New("unknown token-economy subcommand")
	errBenchmarkSubcommand    = errors.New(
		"token-economy benchmark subcommand is required",
	)
	errUnknownBenchmark   = errors.New("unknown token-economy benchmark subcommand")
	errReportOutputPrefix = errors.New(
		"token-economy report --output-prefix is required",
	)
	errTokenEconomyCohort = errors.New(
		"choose exactly one token-economy cohort: --historical or --experiment-id",
	)
	errHistoricalReportInputs = errors.New(
		"historical token-economy reports require repeatable --db and explicit --from/--to",
	)
	errControlledReportInputs = errors.New(
		"--db, --from, and --to are valid only with --historical",
	)
)

type tokenEconomyDatabaseFlags []string

type tokenEconomyReportOptions struct {
	root                string
	stateRoot           string
	tokenDB             string
	experimentID        string
	outputPrefix        string
	fromUTC             string
	toUTC               string
	historicalDatabases []string
	historical          bool
}

func (values *tokenEconomyDatabaseFlags) String() string {
	return strings.Join(*values, ",")
}

func (values *tokenEconomyDatabaseFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: --db value is empty", errHistoricalReportInputs)
	}

	*values = append(*values, value)

	return nil
}

func tokenEconomy(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errTokenEconomySubcommand
	}

	switch args[0] {
	case "benchmark":
		return tokenEconomyBenchmark(ctx, args[1:])
	case "ledger":
		return inspectTokenEconomyLedger(args[1:])
	case tokenEconomyReportCommand:
		return writeTokenEconomyReport(ctx, args[1:])
	default:
		return fmt.Errorf("%w: %q", errUnknownTokenEconomy, args[0])
	}
}

func tokenEconomyBenchmark(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errBenchmarkSubcommand
	}

	switch args[0] {
	case "freeze":
		return freezeTokenEconomyBenchmark(ctx, args[1:])
	case "run":
		return runTokenEconomyBenchmark(ctx, args[1:])
	case "validate":
		return validateTokenEconomyBenchmark(ctx, args[1:])
	default:
		return fmt.Errorf("%w: %q", errUnknownBenchmark, args[0])
	}
}

func freezeTokenEconomyBenchmark(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("token-economy benchmark freeze", flag.ExitOnError)
	draftPath := flags.String("draft", "", "Absolute benchmark draft YAML path")
	outputPath := flags.String("output", "", "Create-new frozen benchmark YAML path")

	err := parseCommandFlags(flags, args, "token-economy benchmark freeze")
	if err != nil {
		return err
	}

	prepared, err := tokeneconomy.FreezeBenchmarkManifest(ctx, *draftPath, *outputPath)
	if err != nil {
		return fmt.Errorf("freeze token-economy benchmark: %w", err)
	}

	return encodeJSON(os.Stdout, prepared)
}

func validateTokenEconomyBenchmark(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("token-economy benchmark validate", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "Absolute frozen benchmark manifest path")

	err := parseCommandFlags(flags, args, "token-economy benchmark validate")
	if err != nil {
		return err
	}

	prepared, err := tokeneconomy.LoadBenchmarkManifest(ctx, *manifestPath)
	if err != nil {
		return fmt.Errorf("validate token-economy benchmark: %w", err)
	}

	return encodeJSON(os.Stdout, prepared)
}

func runTokenEconomyBenchmark(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("token-economy benchmark run", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "Absolute frozen benchmark manifest path")
	stateRoot := flags.String("state-root", "", "Absolute private benchmark state root")
	approvedRuns := flags.Int(
		"approved-max-runs",
		0,
		"Explicit maximum provider runs for this invocation (multiple of three)",
	)

	err := parseCommandFlags(flags, args, "token-economy benchmark run")
	if err != nil {
		return err
	}

	prepared, err := tokeneconomy.LoadBenchmarkManifest(ctx, *manifestPath)
	if err != nil {
		return fmt.Errorf("validate token-economy benchmark: %w", err)
	}

	options := tokeneconomy.BenchmarkRunOptions{
		StateRoot:       strings.TrimSpace(*stateRoot),
		ApprovedMaxRuns: *approvedRuns,
	}
	summary, err := tokeneconomy.RunBenchmark(ctx, prepared, options)
	if err != nil {
		return fmt.Errorf("run token-economy benchmark: %w", err)
	}

	return encodeJSON(os.Stdout, summary)
}

func inspectTokenEconomyLedger(args []string) error {
	flags := flag.NewFlagSet("token-economy ledger", flag.ExitOnError)
	provider := flags.String("provider", "", "Provider ledger format: codex or claude")
	path := flags.String("path", "", "Absolute provider-native JSONL ledger path")

	err := parseCommandFlags(flags, args, "token-economy ledger")
	if err != nil {
		return err
	}

	ledger, err := tokeneconomy.ParseLedger(
		tokeneconomy.Provider(strings.TrimSpace(*provider)),
		*path,
	)
	if err != nil {
		return fmt.Errorf("inspect provider token ledger: %w", err)
	}

	return encodeJSON(os.Stdout, ledger)
}

func writeTokenEconomyReport(ctx context.Context, args []string) error {
	options, err := parseTokenEconomyReportOptions(args)
	if err != nil {
		return err
	}

	report, err := buildTokenEconomyReport(ctx, options)
	if err != nil {
		return fmt.Errorf("build token-economy report: %w", err)
	}

	artifacts, err := tokeneconomy.WriteReportArtifacts(report, options.outputPrefix)
	if err != nil {
		return fmt.Errorf("write token-economy report: %w", err)
	}

	return encodeJSON(os.Stdout, map[string]any{
		"artifacts":   artifacts,
		"causal":      report.Causal,
		"cohort":      report.Cohort,
		"conclusion":  report.Conclusion,
		reportKindKey: report.Kind,
	})
}

func parseTokenEconomyReportOptions(args []string) (tokenEconomyReportOptions, error) {
	flags := flag.NewFlagSet("token-economy "+tokenEconomyReportCommand, flag.ExitOnError)
	root := flags.String("root", ".", "Repository root owning Coding Ethos evidence")
	stateRoot := flags.String("state-root", "", "Private Coding Ethos state root")
	historicalDatabases := tokenEconomyDatabaseFlags{}
	flags.Var(
		&historicalDatabases,
		"db",
		"Historical code-intel DuckDB path; repeat for every source",
	)
	fromUTC := flags.String("from", "", "Historical window start, inclusive RFC3339")
	toUTC := flags.String("to", "", "Historical window end, exclusive RFC3339")
	tokenDB := flags.String("token-db", "", "Token-economy DuckDB path")
	historical := flags.Bool("historical", false, "Report observational proxy transforms")
	experimentID := flags.String("experiment-id", "", "Report one controlled experiment")
	outputPrefix := flags.String(
		"output-prefix",
		"",
		"Create-new report path without extension",
	)

	err := parseCommandFlags(flags, args, "token-economy "+tokenEconomyReportCommand)
	if err != nil {
		return tokenEconomyReportOptions{}, err
	}
	if *historical == (strings.TrimSpace(*experimentID) != "") {
		return tokenEconomyReportOptions{}, errTokenEconomyCohort
	}
	if strings.TrimSpace(*outputPrefix) == "" {
		return tokenEconomyReportOptions{}, errReportOutputPrefix
	}
	if *historical {
		if len(historicalDatabases) == 0 || strings.TrimSpace(*fromUTC) == "" ||
			strings.TrimSpace(*toUTC) == "" || strings.TrimSpace(*tokenDB) != "" {
			return tokenEconomyReportOptions{}, errHistoricalReportInputs
		}
	} else {
		if len(historicalDatabases) != 0 || strings.TrimSpace(*fromUTC) != "" ||
			strings.TrimSpace(*toUTC) != "" {
			return tokenEconomyReportOptions{}, errControlledReportInputs
		}
	}

	return tokenEconomyReportOptions{
		root:                *root,
		stateRoot:           *stateRoot,
		tokenDB:             *tokenDB,
		experimentID:        *experimentID,
		outputPrefix:        *outputPrefix,
		fromUTC:             *fromUTC,
		toUTC:               *toUTC,
		historicalDatabases: historicalDatabases,
		historical:          *historical,
	}, nil
}

func buildTokenEconomyReport(
	ctx context.Context,
	options tokenEconomyReportOptions,
) (tokeneconomy.Report, error) {
	if options.historical {
		report, err := tokeneconomy.HistoricalReport(
			ctx,
			tokeneconomy.HistoricalReportOptions{
				DatabasePaths: options.historicalDatabases,
				FromUTC:       options.fromUTC,
				ToUTC:         options.toUTC,
			},
			time.Now(),
		)
		if err != nil {
			return tokeneconomy.Report{}, fmt.Errorf(
				"query historical token-economy report: %w",
				err,
			)
		}

		return report, nil
	}

	evidenceRoot := resolvedTokenEconomyStateRoot(options.root, options.stateRoot)
	path := strings.TrimSpace(options.tokenDB)
	if path == "" {
		path = tokeneconomy.DefaultDBPath(evidenceRoot)
	}

	return controlledTokenEconomyReport(ctx, path, options.experimentID)
}

func controlledTokenEconomyReport(
	ctx context.Context,
	path string,
	experimentID string,
) (tokeneconomy.Report, error) {
	store, err := tokeneconomy.Open(ctx, filepath.Clean(path))
	if err != nil {
		return tokeneconomy.Report{}, fmt.Errorf("open token-economy store: %w", err)
	}
	defer store.Close()

	report, err := store.ExperimentReport(ctx, strings.TrimSpace(experimentID), time.Now())
	if err != nil {
		return tokeneconomy.Report{}, fmt.Errorf("query token-economy report: %w", err)
	}

	return report, nil
}

func resolvedTokenEconomyStateRoot(root, stateRoot string) string {
	if strings.TrimSpace(stateRoot) != "" {
		return filepath.Clean(stateRoot)
	}

	return codeintel.ResolveStateRoot(filepath.Clean(root))
}
