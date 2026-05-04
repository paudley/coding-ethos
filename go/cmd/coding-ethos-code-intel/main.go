// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

var errCommandRequired = errors.New("code intelligence command is required")

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errCommandRequired
	}

	switch args[0] {
	case "ingest-traces":
		return ingestTraces(ctx, args[1:])
	case "stats":
		return printStats(ctx, args[1:])
	case "repeated-failures":
		return printRepeatedFailures(ctx, args[1:])
	case "search":
		return search(ctx, args[1:])
	default:
		return fmt.Errorf("unknown code intelligence command %q", args[0])
	}
}

func ingestTraces(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("ingest-traces", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos traces")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ingest-traces flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	summary, err := codeintel.NewTraceIngester(store).IngestTraceDirs(ctx, *root)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, summary)
}

func printStats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("stats", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse stats flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func printRepeatedFailures(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repeated-failures", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	path := flags.String("path", "", "Filter by normalized source path")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse repeated-failures flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.RepeatedFailures(ctx, codeintel.RepeatedFailureQuery{
		PolicyID: *policyID,
		SkillID:  *skillID,
		Path:     *path,
		Limit:    *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func search(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("search", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	text := flags.String("text", "", "FTS query text")
	limit := flags.Int("limit", 10, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse search flags: %w", err)
	}
	if *text == "" {
		return errors.New("--text is required")
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.Search(ctx, codeintel.SearchQuery{
		Text:  *text,
		Limit: *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func openStore(ctx context.Context, root string, dbPath string) (*codeintel.Store, error) {
	if dbPath == "" {
		dbPath = codeintel.DefaultDBPath(root)
	}

	return codeintel.Open(ctx, dbPath)
}

func encodeJSON(output *os.File, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	return nil
}
