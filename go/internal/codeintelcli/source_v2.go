// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

type rebuildDerivedReceipt struct {
	Contract          string                        `json:"contract"`
	Kind              string                        `json:"kind"`
	DeprecatedCommand string                        `json:"deprecated_command,omitempty"`
	Replacement       string                        `json:"replacement,omitempty"`
	Summary           codeintel.RebuildIndexSummary `json:"summary"`
}

func syncSourceIndex(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sync", flag.ExitOnError)
	root := flags.String("root", ".", "Repository worktree root")

	err := parseCommandFlags(flags, args, "sync")
	if err != nil {
		return err
	}

	var external codeintel.ExternalBatchExtractor

	extractor, extractorErr := codeintel.DefaultExternalBatchExtractor()
	if extractorErr == nil {
		external = extractor
	}

	receipt, err := codeintel.SyncSourceIndex(ctx, *root, external)
	if err != nil {
		if extractorErr != nil && errors.Is(err, codeintel.ErrExternalExtractorRequired) {
			return fmt.Errorf(
				"sync code-intel source generation: %w: %w",
				err,
				extractorErr,
			)
		}

		return fmt.Errorf("sync code-intel source generation: %w", err)
	}

	return encodeJSON(os.Stdout, receipt)
}

func printSourceIndexStatus(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("status", flag.ExitOnError)
	root := flags.String("root", ".", "Repository worktree root")

	err := parseCommandFlags(flags, args, "status")
	if err != nil {
		return err
	}

	receipt, err := codeintel.SourceIndexStatus(ctx, *root)
	if err != nil {
		return fmt.Errorf("read code-intel source status: %w", err)
	}

	return encodeJSON(os.Stdout, receipt)
}

func rebuildDerived(ctx context.Context, args []string) error {
	return runRebuildDerived(ctx, args, "rebuild-derived", false)
}

func rebuildIndex(ctx context.Context, args []string) error {
	return runRebuildDerived(ctx, args, "rebuild-index", true)
}

func runRebuildDerived(
	ctx context.Context,
	args []string,
	commandName string,
	deprecated bool,
) error {
	flags := flag.NewFlagSet(commandName, flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "Legacy DuckDB code intelligence database path")
	duckDBPath := flags.String(
		"duckdb",
		"",
		"Derived DuckDB code intelligence database path",
	)

	err := parseCommandFlags(flags, args, commandName)
	if err != nil {
		return err
	}

	summary, err := codeintel.RebuildDuckDBIndex(
		ctx,
		*root,
		resolvedDuckDBPath(*root, *duckDBPath),
		resolvedDBPath(*root, *dbPath),
	)
	if err != nil {
		return fmt.Errorf("rebuild derived DuckDB code intelligence index: %w", err)
	}

	receipt := rebuildDerivedReceipt{
		Contract: "coding-ethos.code-intel/v2",
		Kind:     "coding-ethos.code-intel.rebuild-derived/v2",
		Summary:  summary,
	}
	if deprecated {
		receipt.DeprecatedCommand = "rebuild-index"
		receipt.Replacement = "rebuild-derived"
	}

	return encodeJSON(os.Stdout, receipt)
}
